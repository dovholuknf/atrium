package link

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
