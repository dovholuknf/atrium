package link

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// roomsBoard stands in for the hub's board with several rooms behind it. A
// request naming a room gets that room's cards, and one naming none gets every
// room's, the way the proxy's aggregate does.
type roomsBoard struct {
	mu       sync.Mutex
	rooms    map[string][]map[string]any
	launched []string
}

func workers(n int) []map[string]any {
	var out []map[string]any
	for i := 0; i < n; i++ {
		out = append(out, map[string]any{"id": fmt.Sprint(i), "status": "working", "supervised": true,
			"tags": []string{OriginTag, SubagentTag}})
	}
	// Directors and a parked worker never count, however many there are.
	for i := 0; i < 20; i++ {
		out = append(out, map[string]any{"id": fmt.Sprint("d", i), "status": "needs-input",
			"supervised": true, "tags": []string{OriginTag, DirectorTag}})
	}
	return append(out, map[string]any{"id": "parked", "status": "needs-input", "supervised": false,
		"tags": []string{OriginTag, SubagentTag}})
}

func (b *roomsBoard) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		room := r.Header.Get(RoomHeader)
		b.mu.Lock()
		defer b.mu.Unlock()
		switch {
		case r.URL.Path == "/v1/tasks":
			var tasks []map[string]any
			for name, ts := range b.rooms {
				if room == "" || name == room {
					tasks = append(tasks, ts...)
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"tasks": tasks})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/launch":
			b.launched = append(b.launched, room)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "new", "wire_name": "kid"})
		default:
			http.NotFound(w, r)
		}
	})
}

func capsOf(def *int, rooms map[string]int) func(string) int {
	return func(room string) int { return LaunchCaps{Default: def, Rooms: rooms}.For(room) }
}

// A LAUNCH COUNTS ONLY THE ROOM IT GOES TO. sg3 full and the caller's room with
// room to spare: the caller's room takes the launch, sg3 refuses it, and the
// refusal names sg3. Together they are over the old single cap of 10, which is
// the refusal that used to happen with free slots on both.
func TestTheLaunchCapIsPerRoom(t *testing.T) {
	board := &roomsBoard{rooms: map[string][]map[string]any{
		"beta": workers(7), "sg3": workers(5),
	}}
	srv := httptest.NewServer(board.handler())
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client(),
		capFor: capsOf(nil, map[string]int{"beta": 10, "SG3": 5})}

	if _, _, err := c.launchHandler(context.Background(), ctlReq("a", "beta"),
		launchInput{Cwd: "/w"}); err != nil {
		t.Fatalf("7 of 10 on beta, with sg3 full, refused a launch onto beta: %v", err)
	}
	_, _, err := c.launchHandler(context.Background(), ctlReq("a", "beta"),
		launchInput{Cwd: "/w", Room: "sg3"})
	if err == nil {
		t.Fatal("a launch onto sg3 with 5 of 5 running went through")
	}
	if !strings.Contains(err.Error(), "5 running workers on room sg3") {
		t.Errorf("the refusal does not name the room and its cap: %v", err)
	}
	if len(board.launched) != 1 || board.launched[0] != "beta" {
		t.Errorf("launches forwarded to %v, want only beta", board.launched)
	}
}

// A room that is not listed gets the default, and with no default either, the
// old cap.
func TestAnUnlistedRoomGetsTheDefaultCap(t *testing.T) {
	three := 3
	if n := (LaunchCaps{Default: &three, Rooms: map[string]int{"sg3": 5}}).For("m1mini"); n != 3 {
		t.Errorf("an unlisted room got %d, not the default 3", n)
	}
	if n := (LaunchCaps{}).For("m1mini"); n != DefaultLaunchCap {
		t.Errorf("with nothing set a room got %d, not %d", n, DefaultLaunchCap)
	}
	t.Setenv(LaunchCapEnv, "4")
	if n := (LaunchCaps{Rooms: map[string]int{"sg3": 5}}).For("m1mini"); n != 4 {
		t.Errorf("with no default, an unlisted room got %d, not the env cap 4", n)
	}
}

// A reservation holds a slot on its own room only.
func TestAReservationHoldsItsOwnRoomOnly(t *testing.T) {
	board := &roomsBoard{rooms: map[string][]map[string]any{"beta": workers(0), "delta": workers(0)}}
	srv := httptest.NewServer(board.handler())
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client(),
		capFor: capsOf(nil, map[string]int{"delta": 1, "beta": 1})}

	launch := func(room string) error {
		_, _, err := c.launchHandler(context.Background(), ctlReq("a", "beta"),
			launchInput{Cwd: "/w", Room: room})
		return err
	}
	if err := launch("delta"); err != nil {
		t.Fatalf("the first launch onto delta was refused: %v", err)
	}
	if err := launch("delta"); err == nil {
		t.Fatal("a second launch onto delta got past the first one's reservation")
	}
	if err := launch("beta"); err != nil {
		t.Fatalf("delta's reservation held a slot on beta: %v", err)
	}
}

// fakeSettings is HubSettings over a map.
type fakeSettings struct {
	mu sync.Mutex
	m  map[string]string
}

func (f *fakeSettings) HubSetting(name string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.m[name], nil
}

func (f *fakeSettings) SetHubSetting(name, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.m[name] = value
	return nil
}

// The caps are set and read on the hub, and a PUT reaches the next launch.
func TestTheLaunchCapsAreSetOnTheHub(t *testing.T) {
	p := NewProxy(NewHub(Timings{}), nil, "", nil)
	st := &fakeSettings{m: map[string]string{}}
	p.SetLaunchCaps(st)
	front := httptest.NewServer(p)
	defer front.Close()

	code, got := do(t, http.MethodGet, front.URL+"/_hub/launch-caps", "", "")
	if code != http.StatusOK || got["default"] != float64(DefaultLaunchCap) {
		t.Fatalf("GET with nothing set answered %d %s", code, got)
	}
	code, got = do(t, http.MethodPut, front.URL+"/_hub/launch-caps", "application/json",
		`{"default":5,"rooms":{"claude-sg4":10,"sg3":5,"m1mini":5}}`)
	if code != http.StatusOK {
		t.Fatalf("PUT answered %d %s", code, got)
	}
	lc := p.launchCaps()
	if lc.For("claude-sg4") != 10 || lc.For("sg3") != 5 || lc.For("sg4-wsl") != 5 {
		t.Errorf("after the PUT the caps read %+v", lc)
	}

	for _, bad := range []string{`{"rooms":{"sg3":-1}}`, `{"rooms":{"sg3":1000}}`, `{"rooms":{" ":2}}`,
		`{"default":-2}`, `not json`} {
		if code, _ := do(t, http.MethodPut, front.URL+"/_hub/launch-caps", "application/json", bad); code != http.StatusBadRequest {
			t.Errorf("PUT %s answered %d, not 400", bad, code)
		}
	}
	if p.launchCaps().For("sg3") != 5 {
		t.Error("a refused PUT changed the caps")
	}

	// A stored value that will not parse is the old single cap, never a refusal
	// of every launch.
	st.m[SettingLaunchCaps] = "{broken"
	if n := p.launchCaps().For("sg3"); n != DefaultLaunchCap {
		t.Errorf("a broken setting gave sg3 a cap of %d, not %d", n, DefaultLaunchCap)
	}
}

// Setting the caps from off the machine is refused, and reading them is not.
func TestTheLaunchCapsAreSetFromTheHubsMachineOnly(t *testing.T) {
	p := NewProxy(NewHub(Timings{}), nil, "", nil)
	st := &fakeSettings{m: map[string]string{}}
	p.SetLaunchCaps(st)
	from := func(method, body string) int {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, "/_hub/launch-caps", strings.NewReader(body))
		r.RemoteAddr = "192.0.2.7:5555"
		p.ServeHTTP(w, r)
		return w.Code
	}
	if code := from(http.MethodPut, `{"default":50}`); code != http.StatusForbidden {
		t.Errorf("a PUT from elsewhere answered %d, not 403", code)
	}
	if st.m[SettingLaunchCaps] != "" {
		t.Errorf("a refused PUT wrote %q", st.m[SettingLaunchCaps])
	}
	if code := from(http.MethodGet, ""); code != http.StatusOK {
		t.Errorf("a GET from elsewhere answered %d, not 200", code)
	}
}

// A hub with no store for the caps answers the route 404, like notify.
func TestTheLaunchCapsRouteNeedsAStore(t *testing.T) {
	front := httptest.NewServer(NewProxy(NewHub(Timings{}), nil, "", nil))
	defer front.Close()
	if code, _ := do(t, http.MethodGet, front.URL+"/_hub/launch-caps", "", ""); code != http.StatusNotFound {
		t.Errorf("GET without a store answered %d", code)
	}
}
