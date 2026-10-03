package gitsync

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// The lookup a card calls for the URL to fetch code it does not have: `atrium_git_url`, and GET /_hub/git/url.
// See docs/rnd/hub-forge-design.md section 4.
//
// EVERY BRANCH ANSWERS ONE OR TWO SOURCES, and nothing is copied to say so:
//
//	hub   finished work, pushed: `<base>/git/hub/<host>/<owner>/<repo>.git`
//	room  work in progress on an attached room, passed through: `<base>/git/room/<room>/<host>/<owner>/<repo>.git`
//
// A branch that is both pushed and still on its room answers both, with the two shas, and `ahead` says whether the
// room has a commit the hub's store does not.
//
// WHAT A ROOM OFFERS is asked of the room itself: the ref advertisement its own git route gives a fetch
// (`/v1/git/<repo>.git/info/refs`, the same request the pass-through makes), which is the served set of ServedHide and
// nothing else. A branch the pass-through would refuse (stash, notes, an unserved branch, the clone's own main) is
// not in that advertisement, so it is never listed. The answer is held for a few seconds so a card asking in a loop
// does not ask the room in a loop.
//
// THE BRANCH THE CALLER NAMES IS ONLY EVER COMPARED, to the names the hub and the rooms listed. It is not given to
// git, not made into a path and not made into a ref, and a repository is looked up in the list of repositories the hub
// knows, so a name that is not on it asks nobody anything.

// The bounds. A room is asked for one repository at a time, in parallel, and each ask has a deadline.
const (
	// LookupRoomWait is how long one room has to say what it serves.
	LookupRoomWait = 5 * time.Second
	// lookupCacheFor is how long a room's answer for one repository is kept.
	lookupCacheFor = 10 * time.Second
	// lookupCacheMax bounds the kept answers.
	lookupCacheMax = 256
	// lookupAdvertMax bounds the advertisement read from a room.
	lookupAdvertMax = 4 << 20
	// lookupBranchMax bounds the branches taken from one room, and listed in one answer.
	lookupBranchMax = 500
	// LookupClosestMax bounds each list of closest names in a miss.
	LookupClosestMax = 5
	lookupInputMax   = 300
)

// The three answers' states.
const (
	URLFound    = "found"
	URLNotFound = "not found"
	URLOffline  = "offline"
)

// URLQuery is one lookup. Base is `scheme://host` the caller reached the hub by, which the URLs are built on and
// which the caller of this has already checked; it is never made up here.
type URLQuery struct {
	// Repo is `<host>/<owner>/<repo>`, `<owner>/<repo>`, a bare repository name, or a forge URL.
	Repo string
	// Branch is empty to list every branch.
	Branch string
	// Room, when set, asks that one room and no other.
	Room string
	Base string
}

// URLSource is where one branch can be fetched from.
type URLSource struct {
	Source string `json:"source"`
	URL    string `json:"url"`
	SHA    string `json:"sha"`
	// Room and Online are for a room source. Online is always true: a room that is not attached is listed in
	// URLAnswer.Offline instead, because nothing is known of what it has.
	Room   string `json:"room,omitempty"`
	Online bool   `json:"online,omitempty"`
	// Ahead is on a room source of a branch the hub has as well: the room's tip is a commit the hub's store lacks.
	Ahead *bool `json:"ahead,omitempty"`
	// Note is said of a source a card cannot fetch from, which has no URL (ForCard).
	Note string `json:"note,omitempty"`
}

// URLBranch is a branch and where it can be fetched from.
type URLBranch struct {
	Name    string      `json:"name"`
	Sources []URLSource `json:"sources"`
}

// URLClosest is what a miss offers instead, a few names and no more.
type URLClosest struct {
	Repos    []string `json:"repos,omitempty"`
	Branches []string `json:"branches,omitempty"`
}

// URLAnswer is the lookup's answer: `found`, `not found` or `offline`.
type URLAnswer struct {
	State string `json:"state"`
	// Repo is the canonical `<host>/<owner>/<repo>`, when the repository is one the hub knows.
	Repo   string `json:"repo,omitempty"`
	Branch string `json:"branch,omitempty"`
	// Branches is the branch asked for, or every branch when none was.
	Branches []URLBranch `json:"branches,omitempty"`
	// Offline is the rooms that have, or may have, work on this repository and are not attached.
	Offline []string    `json:"offline,omitempty"`
	Closest *URLClosest `json:"closest,omitempty"`
	Note    string      `json:"note,omitempty"`
}

// lookupState is the held answers.
type lookupState struct {
	mu    sync.Mutex
	rooms map[string]roomHeld
}

type roomHeld struct {
	at  time.Time
	ans roomServed
}

// roomServed is what one room said it serves for one repository.
type roomServed struct {
	// State is `ok`, `none` (the room does not have it, or serves none of it) or `down` (no answer).
	State    string
	Branches map[string]string
}

type knownRepo struct {
	name    string
	inStore bool
	// rooms are the rooms the hub has seen sync it, by the name the room gave.
	rooms []string
}

// known is every repository the hub can say something of: its store, its git_repos and the repositories a room has
// synced. A room serves a repository only when the hub synced it, so this is the whole of what can be asked.
func (h *Hub) known(ctx context.Context) map[string]*knownRepo {
	out := map[string]*knownRepo{}
	get := func(name string) *knownRepo {
		k := out[name]
		if k == nil {
			k = &knownRepo{name: name}
			out[name] = k
		}
		return k
	}
	if ents, err := h.Store().List(); err == nil {
		for _, e := range ents {
			get(e.Ref.Name()).inStore = true
		}
	}
	if h.Repos != nil {
		if repos, err := h.Repos(); err == nil {
			for _, r := range repos {
				if ref, err := ParseName(r.Name); err == nil {
					get(ref.Name())
				}
			}
		}
	}
	h.mu.Lock()
	for _, rs := range h.rooms {
		for name, s := range rs.Sync {
			if s.State != "ok" && s.State != "behind" {
				continue
			}
			if ref, err := ParseName(name); err == nil {
				k := get(ref.Name())
				k.rooms = append(k.rooms, s.Room)
			}
		}
	}
	h.mu.Unlock()
	return out
}

// resolveRepo finds what the caller named among the known repositories. Names are compared without regard to case.
// A short name (`owner/repo` or `repo`) that more than one repository answers to is not guessed at: the candidates
// are returned and name is empty.
func resolveRepo(in string, known map[string]*knownRepo) (name string, candidates []string) {
	in = strings.TrimSpace(in)
	if in == "" || len(in) > lookupInputMax {
		return "", nil
	}
	if strings.Contains(in, "://") || scpRe.MatchString(in) {
		if ref, err := ParseURL(in); err == nil {
			in = ref.Name()
		} else {
			return "", nil
		}
	}
	in = strings.TrimSuffix(strings.TrimRight(in, "/"), ".git")
	lower := strings.ToLower(in)
	if ref, err := ParseName(in); err == nil {
		if _, ok := known[ref.Name()]; ok {
			return ref.Name(), nil
		}
	}
	var hits []string
	for n := range known {
		l := strings.ToLower(n)
		if l == lower || strings.HasSuffix(l, "/"+lower) {
			hits = append(hits, n)
		}
	}
	sort.Strings(hits)
	switch len(hits) {
	case 0:
		return "", nil
	case 1:
		return hits[0], nil
	}
	return "", hits
}

// Lookup answers one question. The caller has already decided that this reach may ask.
func (h *Hub) Lookup(ctx context.Context, q URLQuery) URLAnswer {
	q.Repo, q.Branch, q.Room = strings.TrimSpace(q.Repo), strings.TrimSpace(q.Branch), strings.TrimSpace(q.Room)
	attached := h.attachedByName()
	if q.Room != "" {
		if _, ok := attached[strings.ToLower(q.Room)]; !ok {
			names := make([]string, 0, len(attached))
			for _, a := range attached {
				names = append(names, a.Name)
			}
			sort.Strings(names)
			// What the caller typed is cut like every other echo of it, in the note and in the list.
			room := cutText(q.Room)
			note := shown(q.Room) + " is not connected, so what it has cannot be fetched now"
			if len(names) > 0 {
				note += ". connected: " + strings.Join(names, ", ")
			}
			return URLAnswer{State: URLOffline, Branch: cutText(q.Branch), Offline: []string{room}, Note: note}
		}
	}

	known := h.known(ctx)
	name, cands := resolveRepo(q.Repo, known)
	if name == "" {
		all := make([]string, 0, len(known))
		for n := range known {
			all = append(all, n)
		}
		closest := cands
		note := "the hub has no repository called " + shown(q.Repo)
		if len(cands) > 1 {
			note = shown(q.Repo) + " is more than one repository here, say which"
		} else {
			closest = closestNames(q.Repo, all, LookupClosestMax)
		}
		return URLAnswer{State: URLNotFound, Closest: &URLClosest{Repos: closest}, Note: note}
	}
	k := known[name]

	// THE HUB'S SIDE, from its own store: no room is asked for it.
	type onHub struct{ sha, room string }
	hub := map[string]onHub{}
	hubOffline := map[string]bool{}
	var hubDir string
	if k.inStore {
		if v, ok := h.Store().ViewOne(ctx, name); ok {
			hubDir, _ = h.Store().Path(name)
			if v.Main.SHA != "" {
				hub["main"] = onHub{sha: v.Main.SHA}
			}
			for _, b := range v.Branches {
				hub[b.Name] = onHub{sha: b.SHA, room: b.Room}
				if _, up := attached[strings.ToLower(b.Room)]; b.Room != "" && !up {
					hubOffline[b.Room] = true
				}
			}
		}
	}

	// THE ROOMS' SIDE, each room asked what it serves of this repository.
	served := h.askRooms(ctx, name, q.Room, attached)
	offline := map[string]bool{}
	if q.Room == "" {
		for _, r := range k.rooms {
			if _, up := attached[strings.ToLower(r)]; !up {
				offline[r] = true
			}
		}
		for r := range hubOffline {
			offline[r] = true
		}
	}

	names := map[string]bool{}
	for b := range hub {
		names[b] = true
	}
	for _, s := range served {
		for b := range s.ans.Branches {
			names[b] = true
		}
	}
	sorted := make([]string, 0, len(names))
	for b := range names {
		sorted = append(sorted, b)
	}
	sort.Slice(sorted, func(i, j int) bool {
		if (sorted[i] == "main") != (sorted[j] == "main") {
			return sorted[i] == "main"
		}
		return sorted[i] < sorted[j]
	})

	out := URLAnswer{Repo: name, Branch: q.Branch}
	for r := range offline {
		out.Offline = append(out.Offline, r)
	}
	sort.Strings(out.Offline)
	var down []string
	for _, s := range served {
		if s.ans.State == "down" {
			down = append(down, s.room.Name)
		}
	}
	sort.Strings(down)

	build := func(b string) URLBranch {
		ub := URLBranch{Name: b}
		on, onHub := hub[b]
		if onHub {
			ub.Sources = append(ub.Sources, URLSource{Source: "hub", URL: q.Base + StorePrefix + name + ".git", SHA: on.sha})
		}
		for _, s := range served {
			sha, ok := s.ans.Branches[b]
			if !ok {
				continue
			}
			src := URLSource{Source: "room", Room: s.room.Name, Online: true, SHA: sha,
				URL: q.Base + PassPrefix + s.room.Name + "/" + name + ".git"}
			if onHub {
				// AHEAD is a tip the hub's store does not hold. A room at the hub's tip, or behind it, has none.
				ahead := sha != on.sha && !h.Store().hasCommit(ctx, hubDir, sha)
				src.Ahead = &ahead
			}
			ub.Sources = append(ub.Sources, src)
		}
		return ub
	}

	if q.Branch != "" {
		if !names[q.Branch] {
			// A name that is not one the repository has is what the caller typed, so it is cut like the rest.
			out.Branch = cutText(q.Branch)
			out.Closest = &URLClosest{Branches: closestNames(q.Branch, sorted, LookupClosestMax)}
			switch {
			case len(out.Offline) > 0:
				out.State = URLOffline
				out.Note = strings.Join(out.Offline, ", ") + " is not connected, and may have " + shown(q.Branch)
			default:
				out.State = URLNotFound
				out.Note = name + " has no branch " + shown(q.Branch)
			}
			if len(down) > 0 {
				out.Note += ". " + strings.Join(down, ", ") + " did not answer"
			}
			return out
		}
		out.State = URLFound
		out.Branches = []URLBranch{build(q.Branch)}
		return out
	}

	if len(sorted) == 0 && len(out.Offline) > 0 {
		out.State = URLOffline
		out.Note = strings.Join(out.Offline, ", ") + " is not connected, and the hub has none of " + name + "'s branches"
		return out
	}
	out.State = URLFound
	if len(sorted) > lookupBranchMax {
		sorted = sorted[:lookupBranchMax]
		out.Note = fmt.Sprintf("the first %d branches", lookupBranchMax)
	}
	for _, b := range sorted {
		out.Branches = append(out.Branches, build(b))
	}
	if len(sorted) == 0 {
		out.Note = name + " has no branches yet"
	}
	if len(down) > 0 {
		out.Note = strings.TrimSpace(out.Note + " " + strings.Join(down, ", ") + " did not answer")
	}
	return out
}

// cutText is what the caller typed, cut and on one line, for an answer or a sentence.
func cutText(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 80 {
		s = string(r[:80]) + "..."
	}
	return s
}

// shown is cutText in quotes, for a sentence. It is never put anywhere else.
func shown(s string) string { return "`" + cutText(s) + "`" }

func (h *Hub) attachedByName() map[string]RoomInfo {
	out := map[string]RoomInfo{}
	if h.Rooms == nil {
		return out
	}
	for _, a := range h.Rooms.Attached() {
		out[strings.ToLower(a.Name)] = a
	}
	return out
}

type askedRoom struct {
	room RoomInfo
	ans  roomServed
}

// askRooms asks every attached room that serves git (or only `only`) what it serves of one repository, in parallel,
// and answers the rooms that have some of it, ordered by name. A room that does not answer is kept as `down`.
func (h *Hub) askRooms(ctx context.Context, name, only string, attached map[string]RoomInfo) []askedRoom {
	var rooms []RoomInfo
	for _, a := range attached {
		if !a.Git || (only != "" && !strings.EqualFold(a.Name, only)) {
			continue
		}
		rooms = append(rooms, a)
	}
	sort.Slice(rooms, func(i, j int) bool { return rooms[i].Name < rooms[j].Name })
	got := make([]roomServed, len(rooms))
	var wg sync.WaitGroup
	for i := range rooms {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got[i] = h.roomServes(ctx, rooms[i], name)
		}()
	}
	wg.Wait()
	var out []askedRoom
	for i, r := range rooms {
		if got[i].State == "none" {
			continue
		}
		out = append(out, askedRoom{room: r, ans: got[i]})
	}
	return out
}

// roomServes is what one room offers of one repository, from its own git route, held for lookupCacheFor.
func (h *Hub) roomServes(ctx context.Context, room RoomInfo, name string) roomServed {
	key := strings.ToLower(room.Name) + "\x00" + name
	ttl := lookupCacheFor
	if h.LookupTTL > 0 {
		ttl = h.LookupTTL
	}
	st := &h.look
	st.mu.Lock()
	if e, ok := st.rooms[key]; ok && time.Since(e.at) < ttl {
		st.mu.Unlock()
		return e.ans
	}
	st.mu.Unlock()

	ans := h.askRoom1(ctx, room, name)
	// An answer that is `down` is not kept: the next ask should find the room back.
	if ans.State != "down" {
		st.mu.Lock()
		if st.rooms == nil {
			st.rooms = map[string]roomHeld{}
		}
		for k, e := range st.rooms {
			if time.Since(e.at) >= ttl {
				delete(st.rooms, k)
			}
		}
		for len(st.rooms) >= lookupCacheMax {
			oldest, at := "", time.Time{}
			for k, e := range st.rooms {
				if oldest == "" || e.at.Before(at) {
					oldest, at = k, e.at
				}
			}
			delete(st.rooms, oldest)
		}
		st.rooms[key] = roomHeld{at: time.Now(), ans: ans}
		st.mu.Unlock()
	}
	return ans
}

// askRoom1 makes the one request: the ref advertisement the room gives a fetch, which is its served set.
func (h *Hub) askRoom1(ctx context.Context, room RoomInfo, name string) roomServed {
	wait := LookupRoomWait
	if h.LookupWait > 0 {
		wait = h.LookupWait
	}
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://"+room.Name+"/v1/git/"+name+".git/info/refs?service=git-upload-pack", nil)
	if err != nil {
		return roomServed{State: "down"}
	}
	resp, err := h.Rooms.Transport(room.Name).RoundTrip(req)
	if err != nil {
		return roomServed{State: "down"}
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return roomServed{State: "none"}
	case resp.StatusCode != http.StatusOK:
		return roomServed{State: "down"}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, lookupAdvertMax+1))
	if err != nil || len(raw) > lookupAdvertMax {
		return roomServed{State: "down"}
	}
	br, ok := parseAdvert(raw)
	if !ok {
		return roomServed{State: "down"}
	}
	if len(br) == 0 {
		return roomServed{State: "none"}
	}
	return roomServed{State: "ok", Branches: br}
}

// parseAdvert reads a protocol v0 upload-pack advertisement: the `# service=` line, a flush, then `<sha> <ref>` lines up
// to a flush. Only refs/heads are taken, and never the names ServedHide never serves, so a room that advertised more
// than it should (it does not) is still not listed.
func parseAdvert(body []byte) (map[string]string, bool) {
	rest := body
	data, flush, rest, err := readPkt(rest)
	if err != nil || flush || !strings.HasPrefix(string(data), "# service=git-upload-pack") {
		return nil, false
	}
	data, flush, rest, err = readPkt(rest)
	if err != nil || !flush {
		return nil, false
	}
	out := map[string]string{}
	for len(rest) > 0 {
		data, flush, rest, err = readPkt(rest)
		if err != nil {
			return nil, false
		}
		if flush {
			break
		}
		line := strings.TrimSuffix(string(data), "\n")
		if strings.HasPrefix(line, "ERR ") {
			return nil, false
		}
		if i := strings.IndexByte(line, 0); i >= 0 {
			line = line[:i]
		}
		sha, ref, ok := strings.Cut(line, " ")
		b, isHead := strings.CutPrefix(ref, "refs/heads/")
		if !ok || !isHead || !isHex40(sha) || b == "" || len(b) > 200 || neverServed[b] || strings.ContainsAny(b, "\x00\r\n") {
			continue
		}
		if len(out) >= lookupBranchMax {
			break
		}
		out[b] = sha
	}
	return out, true
}

// hasCommit says whether a bare repository holds a commit. sha is 40 hex digits, which the caller took from a
// parsed advertisement, and so is not an option.
func (s *Store) hasCommit(ctx context.Context, dir, sha string) bool {
	if dir == "" || !isHex40(sha) {
		return false
	}
	_, err := s.git(ctx, dir, "cat-file", "-e", sha+"^{commit}")
	return err == nil
}

// closestNames is up to n of have that look like want, nearest first: a name that starts with it, then one that has it
// inside, then a small edit distance. A name is also compared by its last one and two parts, so `zrok` finds
// `github/openziti/zrok`.
func closestNames(want string, have []string, n int) []string {
	want = strings.ToLower(strings.TrimSpace(want))
	if want == "" || len(want) > lookupInputMax {
		return nil
	}
	type scored struct {
		name  string
		score int
	}
	limit := max(2, len(want)/3)
	var out []scored
	for _, h := range have {
		l := strings.ToLower(h)
		forms := []string{l}
		parts := strings.Split(l, "/")
		for i := 1; i < len(parts) && i < 3; i++ {
			forms = append(forms, strings.Join(parts[len(parts)-i:], "/"))
		}
		best := -1
		for _, f := range forms {
			sc := -1
			switch {
			case strings.HasPrefix(f, want) || strings.HasPrefix(want, f) && len(f) >= 3:
				sc = 0
			case strings.Contains(f, want):
				sc = 1
			default:
				if d := editDistance(want, f, limit); d <= limit {
					sc = 2 + d
				}
			}
			if sc >= 0 && (best < 0 || sc < best) {
				best = sc
			}
		}
		if best >= 0 {
			out = append(out, scored{h, best})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score < out[j].score
		}
		return out[i].name < out[j].name
	})
	if len(out) > n {
		out = out[:n]
	}
	names := make([]string, len(out))
	for i, o := range out {
		names[i] = o.name
	}
	return names
}

// editDistance is the Levenshtein distance between a and b, or limit+1 when it is more than limit.
func editDistance(a, b string, limit int) int {
	ra, rb := []rune(a), []rune(b)
	if d := len(ra) - len(rb); d > limit || -d > limit {
		return limit + 1
	}
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		low := cur[0]
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			low = min(low, cur[j])
		}
		if low > limit {
			return limit + 1
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

// Text is the one line a model reads: what to run, or why there is nothing to run.
func (a URLAnswer) Text() string {
	switch a.State {
	case URLFound:
		if a.Branch != "" && len(a.Branches) == 1 {
			src, why := a.Branches[0].pick()
			if src.URL == "" {
				return a.Branch + " cannot be fetched by a card from here: " + why
			}
			return "fetch it with: git fetch " + src.URL + " " + a.Branch + " (" + why + ")"
		}
		names := make([]string, 0, len(a.Branches))
		for _, b := range a.Branches {
			names = append(names, b.Name)
		}
		if len(names) == 0 {
			return a.Repo + " has no branches yet"
		}
		more := ""
		if len(names) > 10 {
			names, more = names[:10], fmt.Sprintf(" and %d more", len(names)-10)
		}
		return a.Repo + " has: " + strings.Join(names, ", ") + more + ". ask again with branch=<name> for the URL to fetch it from"
	default:
		s := a.State + ": " + a.Note
		if a.Closest != nil {
			if len(a.Closest.Repos) > 0 {
				s += ". closest repositories: " + strings.Join(a.Closest.Repos, ", ")
			}
			if len(a.Closest.Branches) > 0 {
				s += ". closest branches: " + strings.Join(a.Closest.Branches, ", ")
			}
		}
		return s
	}
}

// pick is the source to fetch a branch from, and why. A room that is ahead of the hub's copy has the newer work, and
// a branch on the hub alone, or a room alone, has only the one. A source with no URL (ForCard took it out) is never
// the one to fetch from, and a branch with no other says so.
func (b URLBranch) pick() (URLSource, string) {
	var hub *URLSource
	var cut *URLSource
	for i := range b.Sources {
		s := b.Sources[i]
		switch {
		case s.URL == "":
			if cut == nil || s.Source == "hub" {
				cut = &b.Sources[i]
			}
		case s.Source == "hub":
			hub = &b.Sources[i]
		case s.Ahead != nil && *s.Ahead:
			return s, "in progress on " + s.Room + ", which has commits the hub does not"
		}
	}
	if hub != nil {
		why := "finished, on the hub"
		if cut != nil && cut.Source == "room" {
			why += ". " + cut.Room + " has work in progress that a card cannot fetch: ask the card on it to " +
				"atrium_git_push it, then ask again"
		}
		return *hub, why
	}
	if cut != nil {
		return *cut, cut.Note
	}
	s := b.Sources[0]
	return s, "in progress on " + s.Room
}

// NoRoomForCards is what a room's work in progress is answered with to a card: the fetch of it is passed through to
// the room on the hub's own board, and a card's room has no forwarder for it (only for the hub's store). The operator
// can fetch it, and so can a card once the room has pushed it.
const NoRoomForCards = "a card cannot fetch a room's work in progress: its room has no forwarder for it yet. " +
	"ask the card on that room to atrium_git_push it (then it is on the hub), or ask the operator"

// NoHubRemote is what the hub's own work is answered with to a card whose room did not say where its hub forwarder is.
const NoHubRemote = "this card's room did not say where its hub remote is (a room older than the forwarder, or one " +
	"that is not answering), so there is no URL a card here can fetch from. update the room, or ask the operator"

// ForCard is the answer for a card on a room. The URLs the lookup built are the hub's own, on the address the caller
// reached the hub by, which for the control tool is the hub's loopback and is no address at all to a card on another
// machine. A card fetches the hub's store through its own room's forwarder, whose base is `forwarder`
// (`http://127.0.0.1:<agent port>/git/`, the room's HubRemoteBase), and the path after /git/ is the same. A room
// source has no forwarder, so it has no URL and says so. An empty `forwarder` is a room that did not say, and then
// no source has a URL. The answer is a copy.
func (a URLAnswer) ForCard(forwarder string) URLAnswer {
	out := a
	out.Branches = make([]URLBranch, len(a.Branches))
	for i, b := range a.Branches {
		nb := URLBranch{Name: b.Name, Sources: make([]URLSource, len(b.Sources))}
		for j, s := range b.Sources {
			switch {
			case s.Source == "hub" && forwarder != "":
				_, path, _ := strings.Cut(s.URL, "/git/")
				s.URL = forwarder + strings.TrimPrefix(path, "/")
			case s.Source == "hub":
				s.URL, s.Note = "", NoHubRemote
			default:
				s.URL, s.Note = "", NoRoomForCards
			}
			nb.Sources[j] = s
		}
		out.Branches[i] = nb
	}
	return out
}
