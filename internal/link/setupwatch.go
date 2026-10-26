package link

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/gitsync"
)

// Whether the rooms are set up alike, which the rooms pill says.
//
// Two rooms had atrium's claude hooks missing from settings.json for hours and
// nothing on the board said so: activity, subagent counts and the message
// channel were quietly inert on them. The hub now asks each attached room for
// its setup facts and puts them on the room's row of `rooms`, so the pill, its
// tooltip and the rooms page all draw from one answer.
//
// ── nothing new on the room ──────────────────────────────
//
// The facts are read from endpoints a room already has: `GET /v1/hooks` (the
// report the board's hooks dialog draws), `GET /v1/health` (the build id, and
// the go version it was built with) and `GET /v1/harnesses` (its runners). A
// second way to fetch the same facts would be a second answer to drift.
//
// ── when ─────────────────────────────────────────────────
//
// Off the hot path: a ticker looks at who is attached, checks a room it has no
// facts for or that re-attached since, and re-checks the rest every
// setupEvery. Nothing a request waits on ever asks a room.
//
// A room that does not answer is "not answering", which is a different thing
// from a setup problem and is never reported as one.

const (
	// setupEvery is how stale a room's facts may get before they are read again.
	setupEvery = 5 * time.Minute
	// setupTick is how often attachment is looked at, so a room that just attached is read in seconds, not minutes.
	setupTick = 15 * time.Second
	// setupBound is how long one room has to answer all three reads.
	setupBound = 10 * time.Second
)

type setupWatch struct {
	mu    sync.Mutex
	facts map[string]*RoomSetup
}

// setupFor is the facts to put on a room's row, or nil when none were read yet.
func (p *Proxy) setupFor(room string) *RoomSetup {
	p.setup.mu.Lock()
	defer p.setup.mu.Unlock()
	f := p.setup.facts[keyOf(room)]
	if f == nil {
		return nil
	}
	c := *f
	return &c
}

// RunSetupWatch reads attached rooms' setup until ctx ends.
func (p *Proxy) RunSetupWatch(ctx context.Context) {
	t := time.NewTicker(setupTick)
	defer t.Stop()
	for {
		p.setupSweep(ctx, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// setupSweep checks every attached room that is due, and says `rooms` if anything a board draws changed.
func (p *Proxy) setupSweep(ctx context.Context, now time.Time) {
	var due []Attached
	live := map[string]bool{}
	p.setup.mu.Lock()
	for _, a := range p.hub.Rooms() {
		k := keyOf(a.Name)
		live[k] = true
		f := p.setup.facts[k]
		if f == nil || !f.Since.Equal(a.Since) || now.Sub(f.Checked) >= setupEvery {
			due = append(due, a)
		}
	}
	// A room that left takes its facts with it, so it is read fresh when it returns.
	for k := range p.setup.facts {
		if !live[k] {
			delete(p.setup.facts, k)
		}
	}
	p.setup.mu.Unlock()

	var wg sync.WaitGroup
	changed := make(chan bool, len(due))
	for _, a := range due {
		wg.Add(1)
		go func(a Attached) {
			defer wg.Done()
			changed <- p.checkSetup(ctx, a)
		}(a)
	}
	wg.Wait()
	close(changed)
	for c := range changed {
		if c {
			p.feeds.roomsChanged()
			return
		}
	}
}

// checkSetup reads one room and stores what it said. True when what a board draws changed.
func (p *Proxy) checkSetup(ctx context.Context, a Attached) bool {
	ctx, cancel := context.WithTimeout(ctx, setupBound)
	defer cancel()
	f := p.readSetup(ctx, a.Name)
	f.Since = a.Since
	f.Issues = p.setupIssues(ctx, f)
	k := keyOf(a.Name)
	p.setup.mu.Lock()
	defer p.setup.mu.Unlock()
	if p.setup.facts == nil {
		p.setup.facts = map[string]*RoomSetup{}
	}
	old := p.setup.facts[k]
	p.setup.facts[k] = f
	return old == nil || steadySetup(*old) != steadySetup(*f)
}

// steadySetup is the facts without the moment they were read, to compare by.
func steadySetup(s RoomSetup) string {
	s.Checked = time.Time{}
	b, _ := json.Marshal(s)
	return string(b)
}

// readSetup asks the room. A room that fails any of the three reads is not answering.
func (p *Proxy) readSetup(ctx context.Context, room string) *RoomSetup {
	f := &RoomSetup{Checked: time.Now().UTC()}
	var hooks struct {
		Missing    int    `json:"missing"`
		Unreadable string `json:"unreadable"`
		TwoGates   bool   `json:"two_gates"`
		Hooks      []struct {
			Stale bool `json:"stale"`
		} `json:"hooks"`
	}
	var health struct {
		Build  string `json:"build"`
		Code   string `json:"code"`
		Commit string `json:"commit"`
		Go     string `json:"go"`
	}
	var harnesses struct {
		Harnesses []struct {
			ID      string `json:"id"`
			Enabled bool   `json:"enabled"`
			Found   string `json:"found"`
		} `json:"harnesses"`
	}
	if p.setupGet(ctx, room, "/v1/hooks", &hooks) != nil ||
		p.setupGet(ctx, room, "/v1/health", &health) != nil {
		return f
	}
	// A room too old to list its runners still has a setup worth checking.
	_ = p.setupGet(ctx, room, "/v1/harnesses", &harnesses)
	f.Answering = true
	f.HooksMissing, f.HooksUnreadable = hooks.Missing, hooks.Unreadable
	f.HooksTwoGates = hooks.TwoGates
	for _, h := range hooks.Hooks {
		if h.Stale {
			f.HooksStale++
		}
	}
	f.Build, f.Go = health.Build, health.Go
	f.Code, f.Commit = health.Code, health.Commit
	for _, h := range harnesses.Harnesses {
		if h.Enabled && h.Found != "" {
			f.Runners = append(f.Runners, h.ID)
		}
	}
	sort.Strings(f.Runners)
	return f
}

func (p *Proxy) setupGet(ctx context.Context, room, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+hostFor(room)+path, nil)
	if err != nil {
		return err
	}
	res, err := p.roomClient(room).Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
		return errStatus(res.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(out)
}

// setupIssues names what is wrong with an answering room. A build is only compared when both sides say one.
func (p *Proxy) setupIssues(ctx context.Context, f *RoomSetup) []SetupIssue {
	issues := []SetupIssue{}
	if !f.Answering {
		return issues
	}
	switch {
	case f.HooksUnreadable != "":
		issues = append(issues, SetupIssue{"hooks", "claude settings.json cannot be read, so atrium cannot check its hooks"})
	default:
		// The two-gates row is counted once in Missing. It gets its own text, and the count of the rest excludes it.
		missing := f.HooksMissing
		if f.HooksTwoGates {
			issues = append(issues, SetupIssue{"hooks",
				"two permission gates: atrium-perm-hook.ps1 and atrium hook. every tool call asks twice"})
			missing--
		}
		if missing > 0 {
			text := fmt.Sprintf("atrium claude hooks: %d missing", missing)
			if f.HooksStale > 0 {
				text += fmt.Sprintf(", %d stale", f.HooksStale)
			}
			issues = append(issues, SetupIssue{"hooks", text})
		}
	}
	if text := p.buildIssue(ctx, f); text != "" {
		issues = append(issues, SetupIssue{"build", text})
	}
	return issues
}

// SetHubCode says the fingerprint of this hub's own Go, apart from its board. Empty says nothing, and a room's build is
// then compared by board hash alone.
func (p *Proxy) SetHubCode(code string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.hubCode = strings.TrimSpace(code)
}

// buildIssue is the text of the build issue, or "" when there is none. When both sides carry a code fingerprint it alone
// decides: a board-only difference is the same room code and says nothing. A room with no fingerprint (an old build)
// keeps the board-hash compare.
func (p *Proxy) buildIssue(ctx context.Context, f *RoomSetup) string {
	p.mu.Lock()
	hubCode, hubCommit, g := p.hubCode, p.hubBuild, p.landGit
	p.mu.Unlock()
	if hubCode != "" && f.Code != "" {
		if hubCode == f.Code {
			return ""
		}
		return fmt.Sprintf("runs different atrium code than the hub (%s, hub %s)%s",
			shortID(f.Code), shortID(hubCode), newerBuild(ctx, g, f.Commit, hubCommit))
	}
	if p.boardID != "" && f.Build != "" && f.Build != p.boardID {
		return fmt.Sprintf("runs a different atrium build than the hub (%s, hub %s)", shortID(f.Build), shortID(p.boardID))
	}
	return ""
}

// newerBuild words the two commits and which one is newer, when git can say. It is "" when either commit is missing, and
// names only the commits when neither holds the other.
func newerBuild(ctx context.Context, g LandGit, room, hub string) string {
	room, hub = strings.ToLower(strings.TrimSpace(room)), strings.ToLower(strings.TrimSpace(hub))
	if !gitsync.ValidSHA(room) || !gitsync.ValidSHA(hub) {
		return ""
	}
	out := fmt.Sprintf(": room commit %s, hub commit %s", shortID(room), shortID(hub))
	if g == nil {
		return out
	}
	if has, known, _ := g.Contains(ctx, hub, room); known && has {
		return out + ", the hub is newer"
	}
	if has, known, _ := g.Contains(ctx, room, hub); known && has {
		return out + ", the room is newer"
	}
	return out
}

func shortID(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

// serveSetupCheck is `POST /_hub/setup-check?room=<name>`: read one room now. The board calls it after a fix, so the pill
// clears when the hooks are written and not five minutes later.
func (p *Proxy) serveSetupCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "that has to be a POST"})
		return
	}
	name := r.URL.Query().Get("room")
	for _, a := range p.hub.Rooms() {
		if keyOf(a.Name) != keyOf(name) {
			continue
		}
		if p.checkSetup(r.Context(), a) {
			p.feeds.roomsChanged()
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"room": a.Name, "setup": p.setupFor(a.Name)})
		return
	}
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": "no room called " + url.QueryEscape(name) + " is attached"})
}
