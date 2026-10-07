package link

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/forge"
	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/store"
)

func atoiOr0(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// Two fake rooms over a real hub and a real link: which room runs a PR, one row per PR across rooms, the warning for
// an owner that is offline, and the operator's move. The rooms are fakes that behave as api.postPR does: ask the hub
// for the claim first, make a row only when it is theirs.

type claimRoom struct {
	name string
	// running is how many running sessions its /v1/tasks lists.
	running int
	room    *Room
	stop    context.CancelFunc

	mu   sync.Mutex
	rows []string // one entry per row made, the key
	runs int
	// worktrees is how many pr-worktree calls reached this room.
	worktrees int
	// refuseWT makes the pr-worktree call fail, as a refused clone or fetch does.
	refuseWT bool
	// recognise makes a paste match the hub's recogniser rows first, as a room with a hub does, and keys the claim on
	// the captures. This room has no rows of its own.
	recognise bool

	// The review this room holds for the move: the archive it exports and the row id it says. Nil is no review.
	review   []byte
	reviewID string
	// importStatus, when set, is what an import answers, with no row made.
	importStatus int
	// What reached it: the archive imported, the rows archived, the walker bodies.
	imported []byte
	archived []string
	walkers  []string
	// opens is the urls a POST /v1/open brought here.
	opens []string
	// deaf is a room on a build older than the hub's recognisers: an open or a paste finds no recogniser. deafHits
	// counts what it refused.
	deaf     bool
	deafHits int
}

func (c *claimRoom) reviewCalls() (imported []byte, archived, walkers []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.imported, append([]string(nil), c.archived...), append([]string(nil), c.walkers...)
}

func (c *claimRoom) isDeaf() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.deaf
}

func (c *claimRoom) refused() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.deafHits
}

func (c *claimRoom) made() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.rows...)
}

func (c *claimRoom) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch {
	case c.isDeaf() && r.Method == http.MethodPost && (r.URL.Path == "/v1/open" || r.URL.Path == "/v1/prs"):
		c.mu.Lock()
		c.deafHits++
		c.mu.Unlock()
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":"no recogniser matches this","code":"no_recogniser","step":"recognise"}`))
	case r.URL.Path == "/v1/tasks":
		tasks := []map[string]string{}
		for i := 0; i < c.running; i++ {
			tasks = append(tasks, map[string]string{"id": "t", "status": "running"})
		}
		tasks = append(tasks, map[string]string{"id": "d", "status": "done"})
		_ = json.NewEncoder(w).Encode(map[string]any{"tasks": tasks})
	case r.URL.Path == "/v1/prs" && r.Method == http.MethodPost:
		var in struct {
			URL string `json:"url"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		key := strings.ToLower(strings.TrimPrefix(in.URL, "https://"))
		if c.recognise {
			var got HubRecognisers
			if err := c.room.Forge(r.Context(), forge.HubRecognisersPath, struct{}{}, &got); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			_, vars, err := store.MatchRecogniserIn(got.Recognisers, in.URL)
			if err != nil {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = w.Write([]byte(`{"error":"no recogniser matches this"}`))
				return
			}
			key = store.PRKey(vars["host"], vars["org"], vars["repo"], atoiOr0(vars["num"]))
		}
		ans, err := c.room.ClaimPR(r.Context(), map[string]any{"key": key, "url": in.URL})
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if !strings.EqualFold(ans.Owner, c.name) {
			_ = json.NewEncoder(w).Encode(map[string]any{"created": false, "held_by": ans.Owner,
				"forwarded": ans.Forwarded})
			return
		}
		c.mu.Lock()
		for _, have := range c.rows {
			if have == key {
				c.mu.Unlock()
				_, _ = w.Write([]byte(`{"created":false}`))
				return
			}
		}
		c.rows = append(c.rows, key)
		c.runs++
		c.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"created":true}`))
	case strings.HasSuffix(r.URL.Path, "/pr-worktree") && r.Method == http.MethodPost:
		c.mu.Lock()
		c.worktrees++
		refuse := c.refuseWT
		c.mu.Unlock()
		if refuse {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":"the hub could not fetch the head"}`))
			return
		}
		_, _ = w.Write([]byte(`{"path":"/wt/` + c.name + `","existed":false}`))
	case r.URL.Path == "/v1/prs/export" && r.Method == http.MethodGet:
		if c.review == nil {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"no review of that pr here"}`))
			return
		}
		w.Header().Set("X-Atrium-PR-ID", c.reviewID)
		_, _ = w.Write(c.review)
	case r.URL.Path == "/v1/prs/import" && r.Method == http.MethodPost:
		b, _ := io.ReadAll(r.Body)
		if c.importStatus != 0 {
			w.WriteHeader(c.importStatus)
			_, _ = w.Write([]byte(`{"error":"the archive is over the cap"}`))
			return
		}
		c.mu.Lock()
		c.imported = b
		c.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"pr":{"id":"new-` + c.name + `"},"created":true}`))
	case strings.HasPrefix(r.URL.Path, "/v1/prs/") && strings.HasSuffix(r.URL.Path, "/archive") && r.Method == http.MethodPost:
		c.mu.Lock()
		c.archived = append(c.archived, strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/prs/"), "/archive"))
		c.mu.Unlock()
		_, _ = w.Write([]byte(`{}`))
	case strings.HasPrefix(r.URL.Path, "/v1/prs/") && strings.HasSuffix(r.URL.Path, "/walker") && r.Method == http.MethodPost:
		b, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		c.walkers = append(c.walkers, r.URL.Path+" "+string(b))
		c.mu.Unlock()
		_, _ = w.Write([]byte(`{}`))
	case r.URL.Path == "/v1/open" && r.Method == http.MethodPost:
		var in struct {
			URL string `json:"url"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		c.mu.Lock()
		c.opens = append(c.opens, in.URL)
		c.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"key": "k", "card": "c1", "pr": "pr_1", "created": true})
	default:
		http.NotFound(w, r)
	}
}

type claimHub struct {
	hub   *Hub
	proxy *Proxy
	front *httptest.Server
	st    *hubstore.Store
	rooms map[string]*claimRoom
	done  func()
}

func newClaimHub(t *testing.T, running map[string]int) *claimHub {
	t.Helper()
	return newClaimHubIdle(t, running, nil)
}

// newClaimHubIdle is newClaimHub with the idle CPU some rooms report on their beat. A room not in idle says none.
func newClaimHubIdle(t *testing.T, running map[string]int, idle map[string]float64) *claimHub {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	st, err := hubstore.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	hub := NewHub(Timings{Beat: 200 * time.Millisecond, Silence: 2 * time.Second, Warm: 2})
	hub.Git = http.NotFoundHandler()
	ctx, stop := context.WithCancel(context.Background())
	go func() { _ = hub.Serve(ctx, ln) }()
	x := &claimHub{hub: hub, st: st, rooms: map[string]*claimRoom{}}
	for name, n := range running {
		cr := &claimRoom{name: name, running: n}
		rctx, rstop := context.WithCancel(ctx)
		cr.stop = rstop
		cr.room = &Room{Name: name, Dial: plain{addr: ln.Addr().String()}, Handler: cr,
			T: Timings{Beat: 200 * time.Millisecond, Warm: 2, Backoff: 50 * time.Millisecond}}
		if v, ok := idle[name]; ok {
			cr.room.IdleCPU = func() (float64, bool) { return v, true }
		}
		go func() { _ = cr.room.Run(rctx) }()
		x.rooms[name] = cr
	}
	waitFor(t, 5*time.Second, func() bool {
		for name := range running {
			if !hub.Has(name) || !x.rooms[name].room.HubServesGit() {
				return false
			}
		}
		return true
	})
	x.proxy = NewProxy(hub, nil, "", nil)
	x.proxy.SetPRClaims(st)
	x.front = httptest.NewServer(x.proxy)
	x.done = func() { x.front.Close(); stop(); ln.Close(); st.Close() }
	return x
}

// pasteVia is the operator pasting through the hub with a room named, as the board does for a scoped room.
func (x *claimHub) pasteVia(t *testing.T, room, url string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, x.front.URL+"/v1/prs", strings.NewReader(`{"url":"`+url+`"}`))
	req.Header.Set("Content-Type", "application/json")
	if room != "" {
		req.Header.Set(RoomHeader, room)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

const prURL = "https://github.com/openziti/tlsuv/pull/378"

// THE SAME PR FROM BOTH ROOMS IS ONE ROW AND ONE RUN, on the least busy room, whichever room was asked first.
func TestSamePRFromBothRoomsIsOneRowOneRun(t *testing.T) {
	x := newClaimHub(t, map[string]int{"alpha": 3, "beta": 0})
	defer x.done()

	// Pasted at alpha, the busy one. The hub places it on beta and hands the paste over.
	if code, body := x.pasteVia(t, "alpha", prURL); code != 200 || !strings.Contains(body, `"held_by":"beta"`) {
		t.Fatalf("paste at alpha = %d %s", code, body)
	}
	// Pasted again at beta, then at alpha again: the same answer, nothing new.
	x.pasteVia(t, "beta", prURL)
	x.pasteVia(t, "alpha", prURL)

	if got := x.rooms["beta"].made(); len(got) != 1 || x.rooms["beta"].runs != 1 {
		t.Fatalf("beta made rows %v runs %d, want one of each", got, x.rooms["beta"].runs)
	}
	if got := x.rooms["alpha"].made(); len(got) != 0 {
		t.Fatalf("alpha made rows %v, want none", got)
	}
	c, err := x.st.PRClaimOf("github.com/openziti/tlsuv/pull/378")
	if err != nil || c.Room != "beta" {
		t.Fatalf("claim = %+v, %v", c, err)
	}
}

// PLACEMENT: fewest running sessions, a tie to the lower name, a room that did not answer passed over.
func TestPlacementPicksTheLeastBusyRoom(t *testing.T) {
	x := newClaimHub(t, map[string]int{"alpha": 2, "beta": 1, "gamma": 1})
	defer x.done()
	for i := 0; i < 10; i++ {
		if got := x.proxy.placePRRoom(context.Background(), ""); got != "beta" {
			t.Fatalf("placed on %q, want beta (least busy, ties to the lower name)", got)
		}
	}
	x.rooms["beta"].running = 5
	x.rooms["gamma"].running = 5
	if got := x.proxy.placePRRoom(context.Background(), ""); got != "alpha" {
		t.Fatalf("placed on %q, want alpha", got)
	}
}

// A PASTE WITH NO ROOM NAMED, two rooms attached, is placed by the hub rather than refused.
func TestUnscopedPasteGoesToTheLeastBusyRoom(t *testing.T) {
	x := newClaimHub(t, map[string]int{"alpha": 4, "beta": 0})
	defer x.done()
	if code, body := x.pasteVia(t, "", prURL); code != 201 {
		t.Fatalf("paste = %d %s", code, body)
	}
	if len(x.rooms["beta"].made()) != 1 || len(x.rooms["alpha"].made()) != 0 {
		t.Fatalf("rows: beta %v alpha %v", x.rooms["beta"].made(), x.rooms["alpha"].made())
	}
}

// fakeGrowls is a GrowlStore that records what the sweep raises and ends.
type fakeGrowls struct {
	GrowlStore
	mu     sync.Mutex
	raised []GrowlRow
	ended  []string
}

func (f *fakeGrowls) Raise(room string, g GrowlRow) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	g.Room = room
	f.raised = append(f.raised, g)
	return true, nil
}

func (f *fakeGrowls) End(id string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ended = append(f.ended, id)
	return true, nil
}

// AN OFFLINE OWNER IS A WARNING AND NOT A MOVE: the claim stays, the board is told once, and when the room is back
// the warning ends.
func TestOfflineOwnerWarnsAndDoesNotMove(t *testing.T) {
	old := prWarnGrace
	prWarnGrace = 0
	defer func() { prWarnGrace = old }()
	x := newClaimHub(t, map[string]int{"alpha": 3, "beta": 0})
	defer x.done()
	x.pasteVia(t, "alpha", prURL)
	key := "github.com/openziti/tlsuv/pull/378"

	fg := &fakeGrowls{}
	g := NewGrowler(fg)
	if x.proxy.prWarnSweep(g) || len(fg.raised) != 0 {
		t.Fatalf("a warning for an online owner: %+v", fg.raised)
	}
	x.rooms["beta"].stop()
	waitFor(t, 5*time.Second, func() bool { return !x.hub.Has("beta") })

	if !x.proxy.prWarnSweep(g) || len(fg.raised) != 1 {
		t.Fatalf("raised = %+v", fg.raised)
	}
	w := fg.raised[0]
	if !strings.HasPrefix(w.Title, "WARNING") || !strings.Contains(w.Title, key) || !strings.Contains(w.Title, "beta") || w.Room != "beta" {
		t.Fatalf("warning = %+v", w)
	}
	// Once, not every tick, and nothing re-placed it even though alpha is online and is asked again.
	x.proxy.prWarnSweep(g)
	x.pasteVia(t, "alpha", prURL)
	if len(fg.raised) != 1 {
		t.Fatalf("raised again: %+v", fg.raised)
	}
	if c, _ := x.st.PRClaimOf(key); c.Room != "beta" {
		t.Fatalf("claim moved to %q", c.Room)
	}
	if len(x.rooms["alpha"].made()) != 0 {
		t.Fatalf("alpha made a row for a PR beta holds")
	}
}

// A MANUAL MOVE UPDATES THE CLAIM, ends the warning, and the next ask is told the new owner.
func TestManualMoveUpdatesTheClaim(t *testing.T) {
	old := prWarnGrace
	prWarnGrace = 0
	defer func() { prWarnGrace = old }()
	x := newClaimHub(t, map[string]int{"alpha": 3, "beta": 0})
	defer x.done()
	x.pasteVia(t, "alpha", prURL)
	key := "github.com/openziti/tlsuv/pull/378"

	fg := &fakeGrowls{}
	g := NewGrowler(fg)
	x.proxy.SetGrowler(g)
	g.st = fg
	x.rooms["beta"].stop()
	waitFor(t, 5*time.Second, func() bool { return !x.hub.Has("beta") })
	x.proxy.prWarnSweep(g)
	if len(fg.raised) != 1 {
		t.Fatalf("raised = %+v", fg.raised)
	}

	// The review is on beta and beta is offline, so the move is refused with a sentence and nothing changes.
	res, err := http.Post(x.front.URL+"/_hub/pr-claims/move", "application/json",
		strings.NewReader(`{"key":"`+key+`","to":"alpha"}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusConflict || !strings.Contains(string(b), "which is offline. bring it back or move it later") {
		t.Fatalf("offline move = %d %s", res.StatusCode, b)
	}
	if c, _ := x.st.PRClaimOf(key); c.Room != "beta" || len(fg.ended) != 0 {
		t.Fatalf("claim = %+v, ended %v", c, fg.ended)
	}
	// Beta comes back and the move goes through.
	rctx, rstop := context.WithCancel(context.Background())
	defer rstop()
	go func() { _ = x.rooms["beta"].room.Run(rctx) }()
	waitFor(t, 5*time.Second, func() bool { return x.hub.Has("beta") })
	res, err = http.Post(x.front.URL+"/_hub/pr-claims/move", "application/json",
		strings.NewReader(`{"key":"`+key+`","to":"alpha"}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !strings.Contains(string(b), `"room":"alpha"`) {
		t.Fatalf("move = %d %s", res.StatusCode, b)
	}
	if c, _ := x.st.PRClaimOf(key); c.Room != "alpha" {
		t.Fatalf("claim = %+v", c)
	}
	if len(fg.ended) != 1 || fg.ended[0] != fg.raised[0].ID {
		t.Fatalf("ended = %v, raised %v", fg.ended, fg.raised[0].ID)
	}
	// Online owner, so no new warning, and an ask is told alpha.
	x.proxy.prWarnSweep(g)
	if len(fg.raised) != 1 {
		t.Fatalf("raised again: %+v", fg.raised)
	}
	if code, body := x.pasteVia(t, "alpha", prURL); code != 201 {
		t.Fatalf("paste at the new owner = %d %s", code, body)
	}
	if got := x.rooms["alpha"].made(); len(got) != 1 {
		t.Fatalf("alpha rows = %v", got)
	}
}

// THE PULLS VIEW FOLDS TWO ROWS WITH ONE KEY to the claim's owner, and says what it folded.
func TestPullsViewFoldsTwoRowsWithOneKeyToTheOwner(t *testing.T) {
	st, err := hubstore.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	p := &Proxy{prc: st}
	key := hubstore.PRKey("github.com", "openziti", "tlsuv", 378)
	if _, _, err := st.ClaimPR(key, "beta", ""); err != nil {
		t.Fatal(err)
	}
	row := func(room, id string) map[string]any {
		return map[string]any{"room": room, "id": id, "host": "github.com", "org": "openziti", "repo": "tlsuv",
			"number": float64(378), "state": "ready"}
	}
	counts := map[string]float64{"ready": 2}
	out := p.foldPRRows([]map[string]any{row("alpha", "alpha~1"), row("beta", "beta~2")}, counts)
	if len(out) != 1 || out[0]["room"] != "beta" || counts["ready"] != 1 {
		t.Fatalf("out = %+v counts = %v", out, counts)
	}
	f, _ := out[0]["folded"].([]map[string]any)
	if len(f) != 1 || f[0]["room"] != "alpha" {
		t.Fatalf("folded = %+v", out[0]["folded"])
	}
}

func (f *fakeGrowls) Live() ([]GrowlRow, error) { return nil, nil }
func (f *fakeGrowls) Rooms() ([]string, error)  { return nil, nil }
func (f *fakeGrowls) Wake() ([]string, error)   { return nil, nil }

func (f *fakeGrowls) Setting(string) (string, error) { return "", nil }

func f64(v float64) *float64 { return &v }

// THE ORDER: sessions first, then idle CPU (a reported figure before none), then the name.
func TestRoomLoadOrdering(t *testing.T) {
	cases := []struct {
		name string
		a, b roomLoad
		want bool
	}{
		{"fewer sessions beat more idle", roomLoad{room: "z", n: 1, idle: f64(5)}, roomLoad{room: "a", n: 2, idle: f64(90)}, true},
		{"more idle wins a tie", roomLoad{room: "z", n: 1, idle: f64(80)}, roomLoad{room: "a", n: 1, idle: f64(20)}, true},
		{"less idle loses a tie", roomLoad{room: "a", n: 1, idle: f64(20)}, roomLoad{room: "z", n: 1, idle: f64(80)}, false},
		{"a figure beats none", roomLoad{room: "z", n: 1, idle: f64(0)}, roomLoad{room: "a", n: 1}, true},
		{"none loses to a figure", roomLoad{room: "a", n: 1}, roomLoad{room: "z", n: 1, idle: f64(0)}, false},
		{"equal idle goes to the name", roomLoad{room: "a", n: 1, idle: f64(50)}, roomLoad{room: "b", n: 1, idle: f64(50)}, true},
		{"neither known goes to the name", roomLoad{room: "b", n: 1}, roomLoad{room: "a", n: 1}, false},
		{"neither known, lower name", roomLoad{room: "a", n: 1}, roomLoad{room: "b", n: 1}, true},
	}
	for _, c := range cases {
		if got := c.a.lessLoaded(c.b); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// A TIE ON SESSIONS goes to the room reporting more idle CPU, and a room that reports none sorts after one that does.
func TestPlacementBreaksATieOnIdleCPU(t *testing.T) {
	// alpha says nothing, beta is busier than gamma.
	x := newClaimHubIdle(t, map[string]int{"alpha": 1, "beta": 1, "gamma": 1}, map[string]float64{"beta": 20, "gamma": 70})
	defer x.done()
	waitFor(t, 5*time.Second, func() bool {
		n := 0
		for _, a := range x.hub.Rooms() {
			if a.IdleCPU != nil {
				n++
			}
		}
		return n == 2
	})
	if got := x.proxy.placePRRoom(context.Background(), ""); got != "gamma" {
		t.Fatalf("placed on %q, want gamma (most idle CPU)", got)
	}
	// Sessions still come first: gamma is idlest but busier by a session.
	x.rooms["gamma"].running = 2
	if got := x.proxy.placePRRoom(context.Background(), ""); got != "beta" {
		t.Fatalf("placed on %q, want beta (a figure before none, fewer sessions than gamma)", got)
	}
	// With no figure anywhere the name decides, as before.
	x.rooms["alpha"].running, x.rooms["beta"].running = 0, 1
	if got := x.proxy.placePRRoom(context.Background(), ""); got != "alpha" {
		t.Fatalf("placed on %q, want alpha (fewest sessions)", got)
	}
}
