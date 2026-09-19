package link

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeBoardTasks stands in for the hub's own board API. It records the room
// header each request carried, so a test can prove the control tools scope the
// way a scoped board does.
type fakeBoardTasks struct {
	sawRoom string
	tasks   []map[string]any
	health  map[string]any
}

func (f *fakeBoardTasks) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.sawRoom = r.Header.Get(RoomHeader)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/health":
			_ = json.NewEncoder(w).Encode(f.health)
		case "/v1/tasks":
			_ = json.NewEncoder(w).Encode(map[string]any{"tasks": f.tasks})
		default:
			http.NotFound(w, r)
		}
	})
}

// req builds a tool request carrying the identity headers a session would send.
func ctlReq(agent, room string) *mcp.CallToolRequest {
	h := http.Header{}
	if agent != "" {
		h.Set(AgentHeader, agent)
	}
	if room != "" {
		h.Set(RoomHeader, room)
	}
	return &mcp.CallToolRequest{Extra: &mcp.RequestExtra{Header: h}}
}

func TestPeersReadsIdentityFromHeadersAndScopesByRoom(t *testing.T) {
	board := &fakeBoardTasks{tasks: []map[string]any{
		{"id": "1", "wire_name": "alice", "status": "working"},
		{"id": "2", "wire_name": "bob", "status": "needs-input", "supervised": true},
		{"id": "3", "wire_name": "old", "status": "done"},
	}}
	srv := httptest.NewServer(board.handler())
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client()}

	_, out, err := c.peersHandler(context.Background(), ctlReq("alice", "beta"), peersInput{})
	if err != nil {
		t.Fatalf("peers: %v", err)
	}
	if out.Me != "alice" {
		t.Errorf("me = %q, want alice (read off %s)", out.Me, AgentHeader)
	}
	if board.sawRoom != "beta" {
		t.Errorf("board saw room %q, want the header forwarded as beta", board.sawRoom)
	}
	// alice is self (dropped), old is done (dropped without All), so only bob.
	if len(out.Peers) != 1 || out.Peers[0].Handle != "bob" {
		t.Fatalf("peers = %+v, want just bob", out.Peers)
	}
	if !out.Peers[0].Owned {
		t.Errorf("bob is supervised, so atrium_owns_terminal should be true")
	}
}

func TestPeersAllIncludesFinishedCards(t *testing.T) {
	board := &fakeBoardTasks{tasks: []map[string]any{
		{"id": "3", "wire_name": "old", "status": "done"},
	}}
	srv := httptest.NewServer(board.handler())
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client()}

	_, out, err := c.peersHandler(context.Background(), ctlReq("me", "r"), peersInput{All: true})
	if err != nil {
		t.Fatalf("peers: %v", err)
	}
	if len(out.Peers) != 1 {
		t.Fatalf("with All, the done card should show: %+v", out.Peers)
	}
}

func TestPeersWithNoAgentHeaderSaysSo(t *testing.T) {
	board := &fakeBoardTasks{tasks: []map[string]any{}}
	srv := httptest.NewServer(board.handler())
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client()}

	_, out, err := c.peersHandler(context.Background(), ctlReq("", ""), peersInput{})
	if err != nil {
		t.Fatalf("peers: %v", err)
	}
	if out.Me != "" || out.Note == "" {
		t.Errorf("a session with no handle should get Me empty and a note, got %+v", out)
	}
}

func TestStatusCountsWaiting(t *testing.T) {
	board := &fakeBoardTasks{
		health: map[string]any{"halted": false, "db": "/tmp/atrium.db"},
		tasks: []map[string]any{
			{"id": "1", "status": "working"},
			{"id": "2", "status": "needs-input"},
			{"id": "3", "status": "needs-permission"},
		},
	}
	srv := httptest.NewServer(board.handler())
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client()}

	_, out, err := c.statusHandler(context.Background(), ctlReq("a", "beta"), statusInput{})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !out.Running {
		t.Errorf("board answered health, so running should be true")
	}
	if out.Cards != 3 || out.Waiting != 2 {
		t.Errorf("cards=%d waiting=%d, want 3 and 2", out.Cards, out.Waiting)
	}
	if out.DB != "/tmp/atrium.db" {
		t.Errorf("db = %q, want it carried through from health", out.DB)
	}
}

func TestLaunchForwardsBriefToTheRoom(t *testing.T) {
	var gotBrief, gotRoom string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRoom = r.Header.Get(RoomHeader)
		var body struct {
			Brief string `json:"brief"`
			Cwd   string `json:"cwd"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotBrief = body.Brief
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "card1", "wire_name": "kid"})
	}))
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client()}

	_, out, err := c.launchHandler(context.Background(), ctlReq("a", "beta"),
		launchInput{Cwd: "/work/dir", Brief: "read this"})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	if gotBrief != "read this" {
		t.Errorf("the room got brief %q, want it forwarded", gotBrief)
	}
	if gotRoom != "beta" {
		t.Errorf("the room header was %q, want beta", gotRoom)
	}
	if out.Brief != "/work/dir/BRIEF.md" {
		t.Errorf("out.Brief = %q, want the room path named back", out.Brief)
	}
}

func TestLaunchForwardsThemeToTheRoom(t *testing.T) {
	var gotTheme string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Theme string `json:"theme"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotTheme = body.Theme
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "card1", "wire_name": "kid"})
	}))
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client()}

	_, _, err := c.launchHandler(context.Background(), ctlReq("a", "beta"),
		launchInput{Cwd: "/work/dir", Theme: "tangent"})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	if gotTheme != "tangent" {
		t.Errorf("the room got theme %q, want it forwarded", gotTheme)
	}
}

// capBoard stands in for the hub's own board when exercising the launch cap. It
// serves a fixed task list on GET /v1/tasks and records whether a launch was
// forwarded to POST /v1/launch, so a test can prove a refusal never reached the
// room and an allowed launch did.
type capBoard struct {
	tasks    []map[string]any
	launched bool
}

func (b *capBoard) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/tasks":
			_ = json.NewEncoder(w).Encode(map[string]any{"tasks": b.tasks})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/launch":
			b.launched = true
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "new", "wire_name": "kid"})
		default:
			http.NotFound(w, r)
		}
	})
}

func TestLaunchRefusesAtTheCap(t *testing.T) {
	// Five live supervised runners, plus cards that must NOT count: a done card, a
	// backlog card, and an unsupervised one. So the live total is exactly the cap.
	tasks := []map[string]any{
		{"id": "6", "status": "done", "supervised": true},
		{"id": "7", "status": "backlog", "supervised": true},
		{"id": "8", "status": "working", "supervised": false},
	}
	for i := 0; i < DefaultLaunchCap; i++ {
		tasks = append(tasks, map[string]any{
			"id": string(rune('a' + i)), "status": "working", "supervised": true,
		})
	}
	board := &capBoard{tasks: tasks}
	srv := httptest.NewServer(board.handler())
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client()}

	_, _, err := c.launchHandler(context.Background(), ctlReq("a", "beta"),
		launchInput{Cwd: "/work/dir"})
	if err == nil {
		t.Fatal("at the cap, launch should be refused")
	}
	if board.launched {
		t.Fatal("a refused launch must not be forwarded to the room")
	}
}

func TestLaunchProceedsUnderTheCap(t *testing.T) {
	// One under the cap: only running/supervised sessions count, so the done and
	// backlog cards below leave room for one more.
	tasks := []map[string]any{
		{"id": "x", "status": "done", "supervised": true},
		{"id": "y", "status": "backlog", "supervised": true},
	}
	for i := 0; i < DefaultLaunchCap-1; i++ {
		tasks = append(tasks, map[string]any{
			"id": string(rune('a' + i)), "status": "needs-input", "supervised": true,
		})
	}
	board := &capBoard{tasks: tasks}
	srv := httptest.NewServer(board.handler())
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client()}

	_, out, err := c.launchHandler(context.Background(), ctlReq("a", "beta"),
		launchInput{Cwd: "/work/dir"})
	if err != nil {
		t.Fatalf("under the cap, launch should proceed: %v", err)
	}
	if !board.launched {
		t.Fatal("an allowed launch should be forwarded to the room")
	}
	if out.Card != "new" {
		t.Errorf("out.Card = %q, want the room's card id", out.Card)
	}
}

func TestLaunchCapEnvOverride(t *testing.T) {
	t.Setenv(LaunchCapEnv, "1")
	// One live supervised session, cap overridden to one, so the next is refused.
	board := &capBoard{tasks: []map[string]any{
		{"id": "a", "status": "working", "supervised": true},
	}}
	srv := httptest.NewServer(board.handler())
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client()}

	_, _, err := c.launchHandler(context.Background(), ctlReq("a", "beta"),
		launchInput{Cwd: "/work/dir"})
	if err == nil {
		t.Fatal("with the cap overridden to 1 and one session live, launch should refuse")
	}
	if board.launched {
		t.Fatal("a refused launch must not be forwarded to the room")
	}
}

func TestLaunchReservationStopsTwoLaunchesOvershooting(t *testing.T) {
	// The board reports one under the cap and never changes: the second launch sees
	// the same live count the first did, exactly the race that a plain count loses.
	// The reservation the first launch takes is what the second must see.
	var tasks []map[string]any
	for i := 0; i < DefaultLaunchCap-1; i++ {
		tasks = append(tasks, map[string]any{
			"id": string(rune('a' + i)), "status": "working", "supervised": true,
		})
	}
	board := &capBoard{tasks: tasks}
	srv := httptest.NewServer(board.handler())
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client()}

	_, _, err1 := c.launchHandler(context.Background(), ctlReq("a", "beta"),
		launchInput{Cwd: "/work/dir"})
	if err1 != nil {
		t.Fatalf("first launch should fill the last slot: %v", err1)
	}
	_, _, err2 := c.launchHandler(context.Background(), ctlReq("a", "beta"),
		launchInput{Cwd: "/work/dir"})
	if err2 == nil {
		t.Fatal("second launch should be refused by the reservation the first took")
	}
}

func TestLaunchReservationLapsesAfterTTL(t *testing.T) {
	// At the cap only because of one reservation, and that reservation is stale.
	// A launch must sweep it and proceed, so a launch that died before its card
	// appeared cannot hold a slot forever.
	var tasks []map[string]any
	for i := 0; i < DefaultLaunchCap-1; i++ {
		tasks = append(tasks, map[string]any{
			"id": string(rune('a' + i)), "status": "working", "supervised": true,
		})
	}
	board := &capBoard{tasks: tasks}
	srv := httptest.NewServer(board.handler())
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client()}
	c.reservations = []reservation{{id: "stale", at: time.Now().Add(-2 * reservationTTL)}}

	_, _, err := c.launchHandler(context.Background(), ctlReq("a", "beta"),
		launchInput{Cwd: "/work/dir"})
	if err != nil {
		t.Fatalf("a lapsed reservation must not hold the slot: %v", err)
	}
	if !board.launched {
		t.Fatal("the launch should have been forwarded once the stale reservation lapsed")
	}
	if len(c.reservations) != 1 || c.reservations[0].id == "stale" {
		t.Fatalf("the stale reservation should be swept and the new one recorded: %+v", c.reservations)
	}
}

func TestLaunchAllowsWhenCountLookupFails(t *testing.T) {
	// The task list errors (500). Fail sane: allow the launch rather than brick it
	// on an infrastructure hiccup.
	var launched bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/tasks" {
			http.Error(w, `{"error":"board is down"}`, http.StatusInternalServerError)
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/v1/launch" {
			launched = true
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "new", "wire_name": "kid"})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client()}

	_, _, err := c.launchHandler(context.Background(), ctlReq("a", "beta"),
		launchInput{Cwd: "/work/dir"})
	if err != nil {
		t.Fatalf("a failed count lookup should not block the launch: %v", err)
	}
	if !launched {
		t.Fatal("the launch should have been forwarded when the count could not be read")
	}
}

// sayBoard stands in for the hub's own board: it lists one peer so resolvePeer
// finds it, and records the message body so a test can prove the caller is
// attached to the delivered message.
type sayBoard struct {
	gotFrom string
	sawMsg  bool
}

func (b *sayBoard) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/tasks":
			_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []map[string]any{
				{"id": "1", "wire_name": "bob", "status": "working"},
			}})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/tasks/1/message":
			b.sawMsg = true
			var body struct {
				From string `json:"from"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			b.gotFrom = body.From
			_ = json.NewEncoder(w).Encode(map[string]any{"delivered": "queued"})
		default:
			http.NotFound(w, r)
		}
	})
}

func TestSayAttachesTheCallerFromTheHeader(t *testing.T) {
	board := &sayBoard{}
	srv := httptest.NewServer(board.handler())
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client()}

	_, out, err := c.sayHandler(context.Background(), ctlReq("alice", "beta"),
		sayInput{To: "bob", Text: "have you got the lock"})
	if err != nil {
		t.Fatalf("say: %v", err)
	}
	if !board.sawMsg {
		t.Fatal("the message was never posted to the board")
	}
	if board.gotFrom != "alice" {
		t.Errorf("the board got from %q, want the caller read off %s", board.gotFrom, AgentHeader)
	}
	if out.Delivered != "queued" {
		t.Errorf("delivered = %q, want it carried through", out.Delivered)
	}
}

func TestSayWithNoCallerStillDelivers(t *testing.T) {
	board := &sayBoard{}
	srv := httptest.NewServer(board.handler())
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client()}

	// No agent header, as a human or non-atrium caller sends. It must deliver
	// with an empty sender rather than fail over the missing header.
	_, _, err := c.sayHandler(context.Background(), ctlReq("", "beta"),
		sayInput{To: "bob", Text: "your turn"})
	if err != nil {
		t.Fatalf("say with no caller: %v", err)
	}
	if !board.sawMsg {
		t.Fatal("the message was never posted to the board")
	}
	if board.gotFrom != "" {
		t.Errorf("the board got from %q, want empty when no header was sent", board.gotFrom)
	}
}

func TestRestartRefusesWithNoRoom(t *testing.T) {
	c := &controlMCP{hub: NewHub(Timings{})}
	_, _, err := c.restartHandler(context.Background(), ctlReq("a", ""), restartInput{})
	if err == nil {
		t.Fatal("a restart with no room named should be refused")
	}
}

func TestRestartForwardsToTheHub(t *testing.T) {
	// No room attached to this hub, so the forward fails at AskRestart, which is
	// enough to prove the handler routes a named room to the hub rather than
	// answering a stub. A live round trip is covered in restart_test.go.
	c := &controlMCP{hub: NewHub(Timings{})}
	_, out, err := c.restartHandler(context.Background(), ctlReq("a", "beta"), restartInput{})
	if err == nil {
		t.Fatal("restarting an unattached room should surface the hub's error")
	}
	if out.Room != "beta" {
		t.Errorf("the output should name the room it tried: %q", out.Room)
	}
}

func TestControlEndpointIsLoopbackOnly(t *testing.T) {
	hub := NewHub(Timings{})
	p := NewProxy(hub, nil, "", nil)
	p.SetControl(":7778")

	// A request from off the machine is refused before it reaches the MCP server.
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/_hub/mcp", nil)
	r.RemoteAddr = "203.0.113.7:5555"
	p.ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("off-machine control call = %d, want 403", rec.Code)
	}

	// A loopback request gets past the gate and into the MCP server, which
	// refuses a bare GET/POST without a proper MCP body. Anything but 403 proves
	// the gate let it through.
	rec = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPost, "/_hub/mcp", nil)
	r.RemoteAddr = "127.0.0.1:5555"
	p.ServeHTTP(rec, r)
	if rec.Code == http.StatusForbidden {
		t.Fatalf("loopback control call was refused by the gate")
	}
}

func TestControlUnmountedIs404(t *testing.T) {
	hub := NewHub(Timings{})
	p := NewProxy(hub, nil, "", nil) // no SetControl
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/_hub/mcp", nil)
	r.RemoteAddr = "127.0.0.1:5555"
	p.ServeHTTP(rec, r)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("a hub with no control server = %d, want 404", rec.Code)
	}
}

func TestLoopbackBase(t *testing.T) {
	cases := map[string]string{
		":7778":            "http://127.0.0.1:7778",
		"0.0.0.0:7778":     "http://127.0.0.1:7778",
		"127.0.0.1:7800":   "http://127.0.0.1:7800",
		"192.168.1.5:9000": "http://192.168.1.5:9000",
		"garbage":          "http://127.0.0.1:7778",
	}
	for in, want := range cases {
		if got := loopbackBase(in); got != want {
			t.Errorf("loopbackBase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLoopbackRemote(t *testing.T) {
	if !loopbackRemote("127.0.0.1:5555") {
		t.Errorf("127.0.0.1 should be loopback")
	}
	if !loopbackRemote("[::1]:5555") {
		t.Errorf("::1 should be loopback")
	}
	if loopbackRemote("203.0.113.7:5555") {
		t.Errorf("a public address is not loopback")
	}
}
