package link

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// EVERY WAY A BOARD'S STREAM REACHES THE HUB IS A BOARD TO THE GATE, and every
// one of them hears the countdown. A stream the gate did not count would let a
// deploy restart under it, and a stream that missed the countdown would lose
// its pane with no warning.

// soloHub is a hub with one room attached, which is `only` mode: a bare
// `/v1/events` is scoped to that room.
func soloHub(t *testing.T, name string) (*httptest.Server, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hub := NewHub(Timings{Beat: 200 * time.Millisecond, Silence: 2 * time.Second, Warm: 2})
	ctx, stop := context.WithCancel(context.Background())
	go func() { _ = hub.Serve(ctx, ln) }()
	room := &Room{
		Name: name, Dial: plain{addr: ln.Addr().String()}, Handler: streamer(make(chan string)),
		T: Timings{Beat: 200 * time.Millisecond, Warm: 2, Backoff: 50 * time.Millisecond},
	}
	go func() { _ = room.Run(ctx) }()
	waitFor(t, 5*time.Second, func() bool { return hub.Has(name) })
	if hub.Only() != name {
		t.Fatalf("one room attached is not only mode: only=%q", hub.Only())
	}
	front := httptest.NewServer(NewProxy(hub, nil, "", nil))
	return front, func() { front.Close(); stop(); ln.Close() }
}

// listenWith opens a stream with a header set, which `listen` cannot.
func listenWith(t *testing.T, url, header, value string) (<-chan Event, func()) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set(header, value)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("the stream answered %d", res.StatusCode)
	}
	return readSSE(res), func() { res.Body.Close() }
}

// gateState reads what the gate says through the proxy.
func gateState(t *testing.T, base string) map[string]any {
	t.Helper()
	res, err := http.Get(base + "/_hub/restart")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var st map[string]any
	_ = json.NewDecoder(res.Body).Decode(&st)
	return st
}

// askGo runs one ask through the proxy and returns its answer body.
func askGo(t *testing.T, base string) <-chan map[string]any {
	t.Helper()
	out := make(chan map[string]any, 1)
	go func() {
		res, err := http.Post(base+"/_hub/restart", "application/json",
			strings.NewReader(`{"countdown":0.2,"idle":0.05,"wait":30}`))
		if err != nil {
			out <- nil
			return
		}
		defer res.Body.Close()
		var body map[string]any
		_ = json.NewDecoder(res.Body).Decode(&body)
		out <- body
	}()
	return out
}

type boardStream struct {
	name string
	open func(t *testing.T, base string) (<-chan Event, func())
}

func byURL(path string) func(*testing.T, string) (<-chan Event, func()) {
	return func(t *testing.T, base string) (<-chan Event, func()) { return listen(t, base+path) }
}

// countsAndHears opens every stream, checks the gate counts each one, then
// runs an ask and checks each one hears the countdown and the restart.
func countsAndHears(t *testing.T, base string, streams []boardStream) {
	t.Helper()
	chans := make([]<-chan Event, len(streams))
	for i, s := range streams {
		ch, shut := s.open(t, base)
		defer shut()
		chans[i] = ch
	}
	if n, _ := gateState(t, base)["boards"].(float64); int(n) != len(streams) {
		t.Fatalf("%d board streams open, the gate counts %v", len(streams), n)
	}
	answer := askGo(t, base)
	for i, s := range streams {
		if st := fields(t, waitEvent(t, chans[i], restartEvent).Data)["state"]; st != "countdown" {
			t.Fatalf("%s: the first restart event said %v", s.name, st)
		}
		if st := fields(t, waitEvent(t, chans[i], restartEvent).Data)["state"]; st != "restarting" {
			t.Fatalf("%s: the second restart event said %v", s.name, st)
		}
	}
	body := <-answer
	if body["answer"] != "go" {
		t.Fatalf("the ask ended %v", body)
	}
	if why, _ := body["why"].(string); !strings.Contains(why, "counted down") {
		t.Errorf("a go after a countdown said why %q", why)
	}
}

// A SINGLE-ROOM HUB, the live shape: every spelling of the stream a board or a
// popped-out window can use.
func TestTheGateCountsEveryStreamOnASingleRoomHub(t *testing.T) {
	front, done := soloHub(t, "solo")
	defer done()
	countsAndHears(t, front.URL, []boardStream{
		{"merged", byURL("/v1/events/hub")},
		{"room path", byURL("/v1/events/room/solo")},
		{"bare, only mode", byURL("/v1/events")},
		{"room param", byURL("/v1/events?" + RoomParam + "=solo")},
		{"room header", func(t *testing.T, base string) (<-chan Event, func()) {
			return listenWith(t, base+"/v1/events", RoomHeader, "solo")
		}},
	})
}

// TWO ROOMS: the merged stream and a board scoped to either room.
func TestTheGateCountsEveryStreamOnATwoRoomHub(t *testing.T) {
	front, _, done := two(t, streamer(make(chan string)), streamer(make(chan string)))
	defer done()
	countsAndHears(t, front.URL, []boardStream{
		{"merged", byURL("/v1/events/hub")},
		{"bare, merged", byURL("/v1/events")},
		{"alpha path", byURL("/v1/events/room/alpha")},
		{"beta param", byURL("/v1/events?" + RoomParam + "=beta")},
		{"beta header", func(t *testing.T, base string) (<-chan Event, func()) {
			return listenWith(t, base+"/v1/events", RoomHeader, "beta")
		}},
	})
}

// A go with no board open says so, which is how a deploy tells it from a
// countdown the boards saw.
func TestAGoWithNoBoardSaysSo(t *testing.T) {
	front, done := soloHub(t, "solo")
	defer done()
	body := <-askGo(t, front.URL)
	if body["answer"] != "go" || body["why"] != "no board is open" {
		t.Fatalf("a go with no board open answered %v", body)
	}
}

// A board scoped to a room reports input the same as any other: the board's
// fetch wrapper puts the room on every request, and the gate is the hub's.
func TestInputFromAScopedBoardReachesTheGate(t *testing.T) {
	front, done := soloHub(t, "solo")
	defer done()
	events, shut := listen(t, front.URL+"/v1/events/room/solo")
	defer shut()
	for _, how := range []string{"header", "param"} {
		url := front.URL + "/_hub/restart/input"
		if how == "param" {
			url += "?" + RoomParam + "=solo"
		}
		req, _ := http.NewRequest(http.MethodPost, url, nil)
		if how == "header" {
			req.Header.Set(RoomHeader, "solo")
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("input by %s answered %d", how, res.StatusCode)
		}
	}
	// Input just now means the gate waits: no countdown inside the idle window.
	res, err := http.Post(front.URL+"/_hub/restart", "application/json",
		strings.NewReader(`{"countdown":0.2,"idle":30,"wait":0.4}`))
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	_ = json.NewDecoder(res.Body).Decode(&body)
	res.Body.Close()
	if body["answer"] != "busy" {
		t.Fatalf("an ask right after scoped input answered %v", body)
	}
	select {
	case e := <-events:
		if e.Kind == restartEvent {
			t.Errorf("a countdown went out while the board was in use: %s", e.Data)
		}
	default:
	}
}
