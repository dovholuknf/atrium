package gitsync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// RoomInfo is an attached room, as the hub's git side needs to know it.
type RoomInfo struct {
	Name string
	// Git is whether the room said, in its hello, that it takes git syncs. A room that did
	// not is never asked, and is never probed by being refused.
	Git bool
}

// Rooms is what the hub's git side needs of the link.
type Rooms interface {
	Attached() []RoomInfo
	// Transport reaches an attached room's handler over the data pool the room dialled.
	Transport(room string) http.RoundTripper
}

// Hub mirrors each configured repository into a bare repository, tells rooms to sync from
// it, collects each room's claude/* branches back, and delivers them into the checkout.
// Every transfer is a fetch, and no receive-pack runs anywhere.
type Hub struct {
	// Dir is the hub's atrium directory. A bare repository lives at `<Dir>/git/<name>.git`.
	Dir string
	// Repos is the current `git_repos` setting, read on every pass so a change takes effect
	// without a restart.
	Repos func() ([]Repo, error)
	Rooms Rooms
	// Runner runs every git command. Nil takes Default.
	Runner *Runner
	// Audit records an operational line. Nil records nothing.
	Audit func(room, kind, detail string)
	// MirrorEvery and CollectEvery default to 30 seconds and 5 minutes.
	MirrorEvery, CollectEvery time.Duration

	mu     sync.Mutex
	locks  map[string]*sync.Mutex
	mirror map[string]MirrorState
	rooms  map[string]*RoomState
}

// MirrorState is the last look at one repository's mirror.
type MirrorState struct {
	Name   string    `json:"name"`
	Branch string    `json:"branch"`
	SHA    string    `json:"sha,omitempty"`
	At     time.Time `json:"at"`
	Error  string    `json:"error,omitempty"`
}

// RoomState is what the hub last did with one room.
type RoomState struct {
	Sync map[string]SyncResult `json:"-"`
	// State and Repos are the shape a room's own GET /v1/git/status answers: `state` is the
	// worst of the per-repo results (failed, behind, absent, unsupported, then ok), and
	// `none` before any sync.
	State   string         `json:"state"`
	Repos   []SyncResult   `json:"repos"`
	Collect *CollectResult `json:"collect,omitempty"`
}

var severity = map[string]int{"ok": 1, "unsupported": 2, "absent": 3, "behind": 4, "failed": 5}

func (rs *RoomState) shape() {
	rs.State, rs.Repos = "none", []SyncResult{}
	for _, s := range rs.Sync {
		rs.Repos = append(rs.Repos, s)
		if rs.State == "none" || severity[s.State] > severity[rs.State] {
			rs.State = s.State
		}
	}
	sort.Slice(rs.Repos, func(i, j int) bool { return rs.Repos[i].Name < rs.Repos[j].Name })
}

// SyncResult is one room's answer about one repository. States are the room's own: ok,
// absent, behind or failed. `unsupported` is the hub's, for a room that never said Git.
type SyncResult struct {
	Room   string `json:"room"`
	Name   string `json:"name"`
	State  string `json:"state"`
	SHA    string `json:"sha,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// CollectResult is one collect of one room, across the configured repositories.
type CollectResult struct {
	Room  string        `json:"room"`
	At    time.Time     `json:"at"`
	Repos []CollectRepo `json:"repos"`
}

// CollectRepo is one repository in a collect.
type CollectRepo struct {
	Name string `json:"name"`
	// Moved is whether refs/rooms/<room> changed in the bare repository.
	Moved bool `json:"moved"`
	// Delivered is whether the checkout was fetched into.
	Delivered bool   `json:"delivered"`
	Refs      int    `json:"refs"`
	Error     string `json:"error,omitempty"`
}

func (h *Hub) runner() *Runner {
	if h.Runner != nil {
		return h.Runner
	}
	return Default
}

func (h *Hub) audit(room, kind, detail string) {
	if h.Audit != nil {
		h.Audit(room, kind, detail)
	}
}

func (h *Hub) lock(key string) *sync.Mutex {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.locks == nil {
		h.locks = map[string]*sync.Mutex{}
	}
	if h.locks[key] == nil {
		h.locks[key] = &sync.Mutex{}
	}
	return h.locks[key]
}

// Bare is where a repository's bare copy lives.
func (h *Hub) Bare(name string) string {
	return filepath.Join(h.Dir, "git", filepath.FromSlash(name)+".git")
}

// Backend is the handler the hub serves on the `git` link kind, and nowhere else. Only
// names on the list resolve.
func (h *Hub) Backend() *Backend {
	return &Backend{
		// HEAD IS NOT UNDER refs/, so hiding refs/rooms leaves it advertised, and it could
		// point at a hidden ref. Hidden by name.
		Hide: []string{"HEAD", "refs/rooms"},
		Resolve: func(name string) (string, bool) {
			repos, err := h.Repos()
			if err != nil {
				return "", false
			}
			for _, r := range repos {
				if r.Name == name {
					return h.Bare(name), true
				}
			}
			return "", false
		},
	}
}

// RoomKey is a room's name as it appears in a ref: lowercased, and refused when git would
// refuse it.
func (h *Hub) RoomKey(ctx context.Context, room string) (string, error) {
	key := strings.ToLower(room)
	if key == "" || strings.ContainsAny(key, " \t\\:~^?*[") || strings.Contains(key, "..") {
		return "", fmt.Errorf("the room name %q cannot be part of a git ref", room)
	}
	if _, err := h.runner().Git(ctx, "", "check-ref-format", "refs/rooms/"+key+"/claude/x"); err != nil {
		return "", fmt.Errorf("the room name %q cannot be part of a git ref", room)
	}
	return key, nil
}

func (h *Hub) repoNamed(name string) (Repo, error) {
	repos, err := h.Repos()
	if err != nil {
		return Repo{}, err
	}
	for _, r := range repos {
		if r.Name == name {
			return r, nil
		}
	}
	return Repo{}, fmt.Errorf("%q is not in git_repos", name)
}

func (h *Hub) roomInfo(room string) (RoomInfo, bool) {
	for _, r := range h.Rooms.Attached() {
		if strings.EqualFold(r.Name, room) {
			return r, true
		}
	}
	return RoomInfo{}, false
}

// ── mirror in ───────────────────────────────────────────

func slash(p string) string { return filepath.ToSlash(p) }

// Mirror brings each bare repository up to its checkout's branch, and answers the names
// that moved. A local fetch, run by the hub process and not by an agent.
func (h *Hub) Mirror(ctx context.Context) (moved []string, err error) {
	repos, err := h.Repos()
	if err != nil {
		return nil, err
	}
	var errs []string
	for _, r := range repos {
		m, err := h.mirrorOne(ctx, r)
		h.mu.Lock()
		if h.mirror == nil {
			h.mirror = map[string]MirrorState{}
		}
		st := MirrorState{Name: r.Name, Branch: r.Branch, At: time.Now()}
		if err != nil {
			st.Error = err.Error()
		}
		st.SHA = m.sha
		h.mirror[r.Name] = st
		h.mu.Unlock()
		if err != nil {
			errs = append(errs, r.Name+": "+err.Error())
			continue
		}
		if m.moved {
			moved = append(moved, r.Name)
		}
	}
	if len(errs) > 0 {
		return moved, errors.New(strings.Join(errs, "; "))
	}
	return moved, nil
}

type mirrored struct {
	sha   string
	moved bool
}

func (h *Hub) mirrorOne(ctx context.Context, r Repo) (mirrored, error) {
	l := h.lock("mirror:" + r.Name)
	l.Lock()
	defer l.Unlock()
	run := h.runner()
	bare := h.Bare(r.Name)
	if _, err := os.Stat(filepath.Join(bare, "HEAD")); err != nil {
		if err := os.MkdirAll(bare, 0o755); err != nil {
			return mirrored{}, err
		}
		if _, err := run.Git(ctx, "", "init", "-q", "--bare", bare); err != nil {
			return mirrored{}, err
		}
		if _, err := run.Git(ctx, bare, "symbolic-ref", "HEAD", "refs/heads/"+r.Branch); err != nil {
			return mirrored{}, err
		}
	}
	h.cleanLocks(bare, "refs", "packed-refs")
	// HEAD ALWAYS NAMES THE INTEGRATION BRANCH. Upload-pack advertises `symref=HEAD:<target>`
	// even when HEAD is hidden, so a HEAD that pointed at a room's branch would say its name.
	if cur, _ := run.Git(ctx, bare, "symbolic-ref", "-q", "HEAD"); strings.TrimSpace(cur) != "refs/heads/"+r.Branch {
		if _, err := run.Git(ctx, bare, "symbolic-ref", "HEAD", "refs/heads/"+r.Branch); err != nil {
			return mirrored{}, err
		}
	}

	ck := slash(r.Checkout)
	want, err := run.Git(ctx, ck, "-c", "safe.directory="+ck, "rev-parse", "--verify", "-q", "refs/heads/"+r.Branch+"^{commit}")
	want = strings.TrimSpace(want)
	if err != nil || want == "" {
		return mirrored{}, fmt.Errorf("the checkout %s has no branch %s", r.Checkout, r.Branch)
	}
	have, _ := run.Git(ctx, bare, "rev-parse", "--verify", "-q", "refs/heads/"+r.Branch+"^{commit}")
	have = strings.TrimSpace(have)
	if have == want {
		return mirrored{sha: want}, nil
	}
	if _, err := run.Git(ctx, bare, "-c", "safe.directory="+ck, "fetch", "-q", "--no-tags", "--no-write-fetch-head",
		ck, "+refs/heads/"+r.Branch+":refs/heads/"+r.Branch); err != nil {
		return mirrored{sha: have}, err
	}
	h.audit("", "git-mirrored", r.Name+" "+r.Branch+" is now "+short(want))
	return mirrored{sha: want, moved: true}, nil
}

func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// cleanLocks removes stale locks under the refs this side owns in gitDir, and says so. A
// stale lock anywhere else is left, and git reports it in its own words when it trips on it.
func (h *Hub) cleanLocks(gitDir string, owned ...string) {
	removed, _ := CleanLocks(gitDir, owned, h.runner().Bound)
	for _, p := range removed {
		h.audit("", "git-lock-removed", p)
	}
}

// ── sync out ────────────────────────────────────────────

// Sync asks a room to sync one repository (or every one, when name is empty). `init` is only
// ever true when a person or a director asked for it.
func (h *Hub) Sync(ctx context.Context, room, name string, init bool) ([]SyncResult, error) {
	repos, err := h.Repos()
	if err != nil {
		return nil, err
	}
	if name != "" {
		r, err := h.repoNamed(name)
		if err != nil {
			return nil, err
		}
		repos = []Repo{r}
	}
	info, ok := h.roomInfo(room)
	if !ok {
		return nil, fmt.Errorf("the room %q is not attached", room)
	}
	var out []SyncResult
	for _, r := range repos {
		if !info.Git {
			out = append(out, h.remember(SyncResult{Room: info.Name, Name: r.Name, State: "unsupported",
				Detail: "the room's build predates git sync, so it never said Git in its hello"}))
			continue
		}
		// The mirror first, so a sync asked for now is a sync of what the checkout has now.
		if _, err := h.mirrorOne(ctx, r); err != nil {
			out = append(out, h.remember(SyncResult{Room: info.Name, Name: r.Name, State: "failed",
				Detail: "the hub could not mirror it: " + err.Error()}))
			continue
		}
		out = append(out, h.remember(h.askRoom(ctx, info.Name, r.Name, init)))
	}
	return out, nil
}

func (h *Hub) remember(s SyncResult) SyncResult {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.rooms == nil {
		h.rooms = map[string]*RoomState{}
	}
	k := strings.ToLower(s.Room)
	if h.rooms[k] == nil {
		h.rooms[k] = &RoomState{Sync: map[string]SyncResult{}}
	}
	if h.rooms[k].Sync == nil {
		h.rooms[k].Sync = map[string]SyncResult{}
	}
	h.rooms[k].Sync[s.Name] = s
	return s
}

func (h *Hub) askRoom(ctx context.Context, room, name string, init bool) SyncResult {
	l := h.lock("sync:" + strings.ToLower(room) + ":" + name)
	l.Lock()
	defer l.Unlock()
	res := SyncResult{Room: room, Name: name, State: "failed"}
	tr := h.Rooms.Transport(room)
	defer closeIdle(tr)
	body, _ := json.Marshal(map[string]any{"name": name, "init": init})
	ctx, cancel := context.WithTimeout(ctx, CommandBound+time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+strings.ToLower(room)+".room.atrium.internal/v1/git/sync", bytes.NewReader(body))
	if err != nil {
		res.Detail = err.Error()
		return res
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		res.Detail = "the room did not answer: " + err.Error()
		return res
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		res.Detail = fmt.Sprintf("the room answered %d: %s", resp.StatusCode, lineOf(string(raw)))
		return res
	}
	var ans struct{ State, SHA, Detail string }
	if err := json.Unmarshal(raw, &ans); err != nil {
		res.Detail = "the room's answer could not be read: " + err.Error()
		return res
	}
	res.State, res.SHA, res.Detail = ans.State, ans.SHA, ans.Detail
	h.audit(room, "git-sync", name+" "+res.State+" "+short(res.SHA))
	return res
}

func lineOf(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

func closeIdle(rt http.RoundTripper) {
	if c, ok := rt.(interface{ CloseIdleConnections() }); ok {
		c.CloseIdleConnections()
	}
}

// ── collect and deliver ─────────────────────────────────

// Collect fetches a room's claude/* branches, except claude/main, into
// refs/rooms/<room>/claude/* of each bare repository, pruning what the room deleted. When
// that moved a ref, and only then, the branches are delivered into the checkout as
// refs/remotes/<room>/claude/*. Nothing under refs/heads is ever written there.
func (h *Hub) Collect(ctx context.Context, room string) (CollectResult, error) {
	res := CollectResult{Room: room, At: time.Now()}
	info, ok := h.roomInfo(room)
	if !ok {
		return res, fmt.Errorf("the room %q is not attached", room)
	}
	res.Room = info.Name
	if !info.Git {
		return res, errors.New("the room's build predates git sync, so it never said Git in its hello")
	}
	key, err := h.RoomKey(ctx, info.Name)
	if err != nil {
		return res, err
	}
	repos, err := h.Repos()
	if err != nil {
		return res, err
	}
	for _, r := range repos {
		cr := h.collectOne(ctx, info.Name, key, r)
		res.Repos = append(res.Repos, cr)
	}
	h.mu.Lock()
	if h.rooms == nil {
		h.rooms = map[string]*RoomState{}
	}
	if h.rooms[key] == nil {
		h.rooms[key] = &RoomState{}
	}
	h.rooms[key].Collect = &res
	h.mu.Unlock()
	return res, nil
}

func (h *Hub) collectOne(ctx context.Context, room, key string, r Repo) CollectRepo {
	cr := CollectRepo{Name: r.Name}
	l := h.lock("collect:" + r.Name)
	l.Lock()
	defer l.Unlock()
	run := h.runner()
	bare := h.Bare(r.Name)
	if _, err := os.Stat(filepath.Join(bare, "HEAD")); err != nil {
		cr.Error = "the hub has not mirrored this repository yet"
		return cr
	}
	h.cleanLocks(bare, "refs/rooms/"+key, "packed-refs")

	before, _ := listRefs(ctx, run, bare, "refs/rooms/"+key+"/")

	fwd, err := NewForwarder(h.Rooms.Transport(room), strings.ToLower(room)+".room.atrium.internal", "/v1/git")
	if err != nil {
		cr.Error = err.Error()
		return cr
	}
	defer fwd.Close()
	_, err = run.Git(ctx, bare, "fetch", "-q", "--no-tags", "--no-write-fetch-head", "--prune",
		fwd.URL+"/"+r.Name+".git",
		"+refs/heads/claude/*:refs/rooms/"+key+"/claude/*", "^refs/heads/claude/main")
	if err != nil {
		cr.Error = err.Error()
		return cr
	}
	after, err := listRefs(ctx, run, bare, "refs/rooms/"+key+"/")
	if err != nil {
		cr.Error = err.Error()
		return cr
	}
	cr.Refs = len(after)
	cr.Moved = !sameRefs(before, after)

	// Delivered when the collect moved a ref, or when the checkout does not already hold what
	// the bare repository does, which is what a hub killed between the two leaves behind.
	ck := slash(r.Checkout)
	have, _ := listRefs(ctx, run, ck, "refs/remotes/"+key+"/", "-c", "safe.directory="+ck)
	if !cr.Moved && sameRefs(rename(after, "refs/rooms/"+key+"/", ""), rename(have, "refs/remotes/"+key+"/", "")) {
		return cr
	}
	if err := h.deliver(ctx, r, key); err != nil {
		cr.Error = "collected, but could not deliver into the checkout: " + err.Error()
		return cr
	}
	cr.Delivered = true
	h.audit(room, "git-collected", fmt.Sprintf("%s: %d branches", r.Name, cr.Refs))
	return cr
}

func (h *Hub) deliver(ctx context.Context, r Repo, key string) error {
	run := h.runner()
	ck := slash(r.Checkout)
	if common, err := run.Git(ctx, ck, "-c", "safe.directory="+ck, "rev-parse", "--path-format=absolute", "--git-common-dir"); err == nil {
		h.cleanLocks(strings.TrimSpace(common), "refs/remotes/"+key)
	}
	args := []string{"-c", "safe.directory=" + ck, "fetch", "-q", "--no-tags", "--no-write-fetch-head", "--prune",
		slash(h.Bare(r.Name)), "+refs/rooms/" + key + "/claude/*:refs/remotes/" + key + "/claude/*"}
	// The checkout is a working clone somebody else is using, so a ref lock held for a moment
	// is not a failure.
	var err error
	for i, wait := 0, 200*time.Millisecond; i < 5; i, wait = i+1, wait*2 {
		if _, err = run.Git(ctx, ck, args...); err == nil {
			return nil
		}
		if !strings.Contains(err.Error(), "lock") {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
	return err
}

// listRefs answers refname to sha for everything under a prefix. `pre` are git options to
// put before the command.
func listRefs(ctx context.Context, run *Runner, dir, prefix string, pre ...string) (map[string]string, error) {
	args := append(append([]string{}, pre...), "for-each-ref", "--format=%(objectname) %(refname)", prefix)
	out, err := run.Git(ctx, dir, args...)
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	for _, l := range strings.Split(out, "\n") {
		if sha, ref, ok := strings.Cut(strings.TrimSpace(l), " "); ok {
			m[ref] = sha
		}
	}
	return m, nil
}

func sameRefs(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func rename(m map[string]string, from, to string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[to+strings.TrimPrefix(k, from)] = v
	}
	return out
}

// ── when things happen ──────────────────────────────────

// Attached is called once when a room attaches: sync every repository with init false, then
// collect. Off the attach path.
func (h *Hub) Attached(ctx context.Context, room string) {
	go func() {
		info, ok := h.roomInfo(room)
		if !ok || !info.Git {
			return
		}
		if _, err := h.Sync(ctx, room, "", false); err != nil {
			h.audit(room, "git-sync", "failed: "+err.Error())
		}
		if _, err := h.Collect(ctx, room); err != nil {
			h.audit(room, "git-collected", "failed: "+err.Error())
		}
	}()
}

// Start runs the timers until ctx ends, and stops every git child within `wait` after.
func (h *Hub) Start(ctx context.Context, wait time.Duration) {
	mirrorEvery, collectEvery := h.MirrorEvery, h.CollectEvery
	if mirrorEvery <= 0 {
		mirrorEvery = 30 * time.Second
	}
	if collectEvery <= 0 {
		collectEvery = 5 * time.Minute
	}
	go func() {
		<-ctx.Done()
		h.runner().Stop(wait)
	}()
	go func() {
		t := time.NewTicker(mirrorEvery)
		defer t.Stop()
		for {
			moved, err := h.Mirror(ctx)
			if err != nil && ctx.Err() == nil {
				h.audit("", "git-mirror", "failed: "+err.Error())
			}
			for _, name := range moved {
				go h.tellRooms(ctx, name)
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
	go func() {
		t := time.NewTicker(collectEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			for _, r := range h.Rooms.Attached() {
				if r.Git {
					if _, err := h.Collect(ctx, r.Name); err != nil && ctx.Err() == nil {
						h.audit(r.Name, "git-collected", "failed: "+err.Error())
					}
				}
			}
		}
	}()
}

// tellRooms asks every attached room that takes git to sync a repository whose mirror moved.
func (h *Hub) tellRooms(ctx context.Context, name string) {
	for _, r := range h.Rooms.Attached() {
		if !r.Git {
			continue
		}
		res := h.remember(h.askRoom(ctx, r.Name, name, false))
		if res.State == "failed" {
			h.audit(r.Name, "git-sync", name+" failed: "+res.Detail)
		}
	}
}

// HubStatus is what the hub knows, for the board and the CLI.
type HubStatus struct {
	Repos  []Repo                `json:"repos"`
	Mirror []MirrorState         `json:"mirror"`
	Rooms  map[string]*RoomState `json:"rooms"`
}

func (h *Hub) Status() HubStatus {
	repos, _ := h.Repos()
	h.mu.Lock()
	defer h.mu.Unlock()
	st := HubStatus{Repos: repos, Rooms: map[string]*RoomState{}}
	for _, m := range h.mirror {
		st.Mirror = append(st.Mirror, m)
	}
	sort.Slice(st.Mirror, func(i, j int) bool { return st.Mirror[i].Name < st.Mirror[j].Name })
	for k, v := range h.rooms {
		cp := *v
		cp.shape()
		st.Rooms[k] = &cp
	}
	return st
}
