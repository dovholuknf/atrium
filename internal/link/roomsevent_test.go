package link

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// The `rooms` event and the approver that now runs on events. Both replaced a
// one second ticker, so what these pin is that the news arrives without it, that
// it arrives once, and that nothing is asked of a room while nothing is
// happening.

// stock is an Inventory whose answers a test can change under a running hub. The
// switch and its deadline are read under a lock because the approver reads them
// from another goroutine, at the moment of deciding.
type stock struct {
	remembering
	mu     sync.Mutex
	state  map[string]string
	auto   bool
	until  *time.Time
	rooms2 []string
}

func newStock(names ...string) *stock { return &stock{state: map[string]string{}, rooms2: names} }

func (s *stock) Known() ([]Known, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Known, 0, len(s.rooms2))
	for _, n := range s.rooms2 {
		st := s.state[n]
		if st == "" {
			st = "active"
		}
		out = append(out, Known{Name: n, Transport: "direct", State: st})
	}
	return out, nil
}

func (s *stock) MarkRoom(name string, marked bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if marked {
		s.state[name] = "marked"
	} else {
		delete(s.state, name)
	}
	return nil
}

func (s *stock) BoardAuto() (bool, *time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.auto, s.until, nil
}

func (s *stock) set(on bool, until *time.Time) {
	s.mu.Lock()
	s.auto, s.until = on, until
	s.mu.Unlock()
}

// bench is a hub with a proxy in front, to which rooms can be added one at a time.
type bench struct {
	t     *testing.T
	hub   *Hub
	proxy *Proxy
	front *httptest.Server
	ctx   context.Context
	addr  string
}

func newBench(t *testing.T) *bench {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hub := NewHub(Timings{Beat: 200 * time.Millisecond, Silence: 2 * time.Second, Warm: 2})
	ctx, stop := context.WithCancel(context.Background())
	go func() { _ = hub.Serve(ctx, ln) }()
	proxy := NewProxy(hub, nil, "", nil)
	front := httptest.NewServer(proxy)
	t.Cleanup(func() { front.Close(); stop(); ln.Close() })
	return &bench{t: t, hub: hub, proxy: proxy, front: front, ctx: ctx, addr: ln.Addr().String()}
}

func (b *bench) attach(name string, h http.Handler) {
	b.t.Helper()
	room := &Room{
		Name: name, Dial: plain{addr: b.addr}, Handler: h,
		T: Timings{Beat: 200 * time.Millisecond, Warm: 2, Backoff: 50 * time.Millisecond},
	}
	go func() { _ = room.Run(b.ctx) }()
	waitFor(b.t, 5*time.Second, func() bool { return b.hub.Has(name) })
}

// roomsEvent waits for the next `rooms` event and returns its fields.
func roomsEvent(t *testing.T, ch <-chan Event, within time.Duration) map[string]any {
	t.Helper()
	deadline := time.After(within)
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				t.Fatalf("the stream ended before a rooms event")
			}
			if e.Kind == "rooms" {
				return fields(t, e.Data)
			}
		case <-deadline:
			t.Fatalf("no rooms event within %s", within)
		}
	}
}

func namesOf(obj map[string]any) []string {
	var out []string
	list, _ := obj["rooms"].([]any)
	for _, n := range list {
		out = append(out, n.(string))
	}
	return out
}

func fetchFields(t *testing.T, url string) map[string]any {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return fields(t, raw)
}

func postMark(t *testing.T, front, name string, marked bool) {
	t.Helper()
	res, err := http.Post(front+"/_hub/inventory/mark", "application/json",
		strings.NewReader(fmt.Sprintf(`{"name":%q,"marked":%v}`, name, marked)))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("mark answered %d", res.StatusCode)
	}
}

// A board scoped to one room hears the set change, which it could not before.
func TestARoomScopedBoardHearsARoomAttach(t *testing.T) {
	b := newBench(t)
	b.proxy.SetInventory(newStock("alpha", "beta"))
	b.attach("alpha", cards("alpha", "c1"))

	ch, shut := listen(t, b.front.URL+"/v1/events/room/alpha")
	defer shut()
	first := roomsEvent(t, ch, 5*time.Second)
	if got := namesOf(first); len(got) != 1 || got[0] != "alpha" {
		t.Fatalf("the first event named %v", got)
	}

	b.attach("beta", cards("beta", "c2"))
	second := roomsEvent(t, ch, 5*time.Second)
	if got := namesOf(second); len(got) != 2 {
		t.Fatalf("a second room attached and the event named %v", got)
	}
}

func TestARoomScopedBoardHearsAMark(t *testing.T) {
	b := newBench(t)
	b.proxy.SetInventory(newStock("alpha"))
	b.attach("alpha", cards("alpha", "c1"))

	ch, shut := listen(t, b.front.URL+"/v1/events/room/alpha")
	defer shut()
	roomsEvent(t, ch, 5*time.Second)

	postMark(t, b.front.URL, "alpha", true)
	e := roomsEvent(t, ch, 5*time.Second)
	inv, _ := e["inventory"].([]any)
	if len(inv) != 1 || inv[0].(map[string]any)["state"] != "marked" {
		t.Fatalf("the event did not carry the mark: %v", e["inventory"])
	}
}

// One builder, two readers: what the event carries is what the endpoints answer.
func TestTheRoomsEventEqualsTheEndpoints(t *testing.T) {
	b := newBench(t)
	b.proxy.SetInventory(newStock("alpha", "beta"))
	b.attach("alpha", cards("alpha", "c1"))
	b.attach("beta", cards("beta", "c2"))

	ch, shut := listen(t, b.front.URL+"/v1/events/hub")
	defer shut()
	postMark(t, b.front.URL, "beta", true)
	var e map[string]any
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		e = roomsEvent(t, ch, 5*time.Second)
		if inv, _ := e["inventory"].([]any); len(inv) == 2 &&
			inv[1].(map[string]any)["state"] == "marked" {
			break
		}
	}

	rooms := fetchFields(t, b.front.URL+"/_hub/rooms")
	inv := fetchFields(t, b.front.URL+"/_hub/inventory")

	// `idle` and `last_beat` move between two reads, so compare what a board keys on.
	names := func(v any) string {
		var out []string
		for _, r := range v.([]any) {
			m := r.(map[string]any)
			out = append(out, fmt.Sprint(m["name"], m["os"], m["arch"], m["version"]))
		}
		return strings.Join(out, ",")
	}
	if names(e["attached"]) != names(rooms["rooms"]) {
		t.Errorf("attached differs: event %s, endpoint %s", names(e["attached"]), names(rooms["rooms"]))
	}
	got, _ := json.Marshal(e["inventory"])
	want, _ := json.Marshal(inv["rooms"])
	if string(got) != string(want) {
		t.Errorf("inventory differs:\n event    %s\n endpoint %s", got, want)
	}
	if e["durable"] != inv["durable"] {
		t.Errorf("durable differs: %v vs %v", e["durable"], inv["durable"])
	}
}

// Five marks in a row are one event, or two if the settle window straddles.
func TestABurstOfChangesIsOneOrTwoEvents(t *testing.T) {
	b := newBench(t)
	b.proxy.SetInventory(newStock("alpha"))
	b.attach("alpha", cards("alpha", "c1"))

	ch, shut := listen(t, b.front.URL+"/v1/events/hub")
	defer shut()
	roomsEvent(t, ch, 5*time.Second)

	for i := 0; i < 5; i++ {
		postMark(t, b.front.URL, "alpha", i%2 == 0)
	}
	n := 0
	quiet := time.After(700 * time.Millisecond)
loop:
	for {
		select {
		case e := <-ch:
			if e.Kind == "rooms" {
				n++
			}
		case <-quiet:
			break loop
		}
	}
	// Five toggles end marked, so the payload differs from the last one sent.
	if n < 1 || n > 2 {
		t.Fatalf("five changes gave %d rooms events", n)
	}
}

// No ticker: a newly attached room's pump starts on the attach.
func TestAPumpStartsOnTheAttachWithNoTicker(t *testing.T) {
	b := newBench(t)
	b.attach("alpha", cards("alpha", "c1"))
	_, shut := listen(t, b.front.URL+"/v1/events/hub")
	defer shut()
	waitFor(t, 2*time.Second, func() bool { return b.proxy.feeds.count() == 1 })

	b.attach("beta", cards("beta", "c2"))
	waitFor(t, 100*time.Millisecond, func() bool { return b.proxy.feeds.count() == 2 })
}

// A room that counts what it is asked, and streams what it is told to.
type permRoom struct {
	mu       sync.Mutex
	gets     int
	pending  map[string]bool
	decides  map[string]int
	say      chan string
	reasoned string
}

func newPermRoom() *permRoom {
	return &permRoom{pending: map[string]bool{}, decides: map[string]int{}, say: make(chan string, 8)}
}

func (l *permRoom) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/events":
			f := w.(http.Flusher)
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			f.Flush()
			for {
				select {
				case <-r.Context().Done():
					return
				case line := <-l.say:
					fmt.Fprint(w, line)
					f.Flush()
				}
			}
		case r.URL.Path == "/v1/permissions" && r.Method == http.MethodGet:
			l.mu.Lock()
			l.gets++
			var ids []string
			for id, p := range l.pending {
				if p {
					ids = append(ids, fmt.Sprintf(`{"id":%q,"tool":"Bash"}`, id))
				}
			}
			l.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"permissions":[%s]}`, strings.Join(ids, ","))
		case strings.HasPrefix(r.URL.Path, "/v1/permissions/") && r.Method == http.MethodPost:
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/permissions/"), "/decide")
			l.mu.Lock()
			l.decides[id]++
			l.pending[id] = false
			l.mu.Unlock()
			fmt.Fprint(w, `{"ok":true}`)
		default:
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"cards":[]}`)
		}
	})
}

func (l *permRoom) raise(id string) {
	l.mu.Lock()
	l.pending[id] = true
	l.mu.Unlock()
	l.say <- sse("permission", fmt.Sprintf(`{"id":%q,"tool":"Bash","command":"ls"}`, id))
}

func (l *permRoom) counts() (gets int, decides map[string]int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := map[string]int{}
	for k, v := range l.decides {
		out[k] = v
	}
	return l.gets, out
}

// Auto on and no board open: the request is approved from its event, and the room
// is read only at turn-on, attach and reconnect, never on a clock.
func TestAutoApprovesFromTheEventWithNoBoardAndNoPolling(t *testing.T) {
	b := newBench(t)
	l := newPermRoom()
	b.attach("alpha", l.handler())
	s := newStock("alpha")
	s.set(true, nil)
	b.proxy.SetInventory(s)

	// The turn-on sweep and the pump's connect sweep are the reads allowed.
	waitFor(t, 5*time.Second, func() bool { return b.proxy.feeds.count() == 1 })
	time.Sleep(400 * time.Millisecond)
	base, _ := l.counts()

	l.raise("p9")
	waitFor(t, 5*time.Second, func() bool { _, d := l.counts(); return d["p9"] == 1 })

	// Idle for several of the old ticks.
	time.Sleep(2500 * time.Millisecond)
	gets, decides := l.counts()
	if gets != base {
		t.Errorf("the room was read %d more time(s) while nothing happened", gets-base)
	}
	if decides["p9"] != 1 {
		t.Errorf("p9 was decided %d times", decides["p9"])
	}
}

// A request answered by a sweep and announced by an event is decided once.
func TestASweepAndAnEventForOneRequestDecideOnce(t *testing.T) {
	b := newBench(t)
	l := newPermRoom()
	l.pending["p1"] = true
	b.attach("alpha", l.handler())
	s := newStock("alpha")
	s.set(true, nil)
	b.proxy.SetInventory(s)

	waitFor(t, 5*time.Second, func() bool { _, d := l.counts(); return d["p1"] >= 1 })
	// The event for the same request arrives after the sweep found it.
	l.mu.Lock()
	l.pending["p1"] = true
	l.mu.Unlock()
	l.say <- sse("permission", `{"id":"p1","tool":"Bash"}`)
	time.Sleep(600 * time.Millisecond)
	if _, d := l.counts(); d["p1"] != 1 {
		t.Fatalf("p1 was decided %d times", d["p1"])
	}
}

// The switch is read at the moment of deciding, not when the event arrived.
func TestAnEventInFlightWhenAutoIsSwitchedOffIsNotApproved(t *testing.T) {
	b := newBench(t)
	l := newPermRoom()
	b.attach("alpha", l.handler())
	s := newStock("alpha")
	s.set(true, nil)
	b.proxy.SetInventory(s)
	waitFor(t, 5*time.Second, func() bool { return b.proxy.feeds.count() == 1 })

	// Off first, with no nudge yet, so the approver is still subscribed when the
	// event lands. Only the re-read at decision time stands between it and an
	// approval.
	s.set(false, nil)
	l.raise("p2")
	time.Sleep(700 * time.Millisecond)
	if _, d := l.counts(); d["p2"] != 0 {
		t.Fatalf("a request was approved after the switch went off")
	}
}

func TestAnEventPastTheDeadlineIsNotApproved(t *testing.T) {
	b := newBench(t)
	l := newPermRoom()
	b.attach("alpha", l.handler())
	s := newStock("alpha")
	until := time.Now().Add(1500 * time.Millisecond)
	s.set(true, &until)
	b.proxy.SetInventory(s)
	waitFor(t, 5*time.Second, func() bool { return b.proxy.feeds.count() == 1 })

	// The deadline passes between the approver subscribing and the event.
	time.Sleep(time.Until(until) + 100*time.Millisecond)
	l.raise("p3")
	time.Sleep(700 * time.Millisecond)
	if _, d := l.counts(); d["p3"] != 0 {
		t.Fatalf("a request was approved after the deadline")
	}
}

// Auto off, nobody watching: nothing streams.
func TestNoPumpRunsWithAutoOffAndNoBoard(t *testing.T) {
	b := newBench(t)
	l := newPermRoom()
	b.attach("alpha", l.handler())
	b.proxy.SetInventory(newStock("alpha"))
	time.Sleep(600 * time.Millisecond)
	if n := b.proxy.feeds.count(); n != 0 {
		t.Fatalf("%d pump(s) ran with auto off and no board", n)
	}
	if gets, _ := l.counts(); gets != 0 {
		t.Fatalf("the room was read %d time(s) with auto off", gets)
	}
}

// The nudge is a POST from this machine and nothing else.
func TestTheNudgeEndpoint(t *testing.T) {
	b := newBench(t)
	res, err := http.Post(b.front.URL+"/_hub/nudge", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Errorf("a loopback POST answered %d", res.StatusCode)
	}
	res, err = http.Get(b.front.URL + "/_hub/nudge")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("a GET answered %d, wanted 405", res.StatusCode)
	}
}
