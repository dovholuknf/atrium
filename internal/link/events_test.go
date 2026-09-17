package link

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// streamer is a room that answers `/v1/events` and says what it is told to.
func streamer(say <-chan string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/events" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"tasks":[]}`)
			return
		}
		f, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flush", 500)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		f.Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case line, ok := <-say:
				if !ok {
					return
				}
				fmt.Fprint(w, line)
				f.Flush()
			}
		}
	})
}

func sse(kind, data string) string { return "event: " + kind + "\ndata: " + data + "\n\n" }

// listen opens a stream and reads events off it.
func listen(t *testing.T, url string) (<-chan Event, func()) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Accept", "text/event-stream")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("the stream answered %d", res.StatusCode)
	}
	out := make(chan Event, 16)
	go func() {
		defer close(out)
		br := bufio.NewReader(res.Body)
		kind, data := "", ""
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			switch {
			case line == "":
				if data != "" {
					out <- Event{Kind: kind, Data: []byte(data)}
				}
				kind, data = "", ""
			case strings.HasPrefix(line, "event: "):
				kind = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				data = strings.TrimPrefix(line, "data: ")
			}
		}
	}()
	return out, func() { res.Body.Close() }
}

func waitEvent(t *testing.T, ch <-chan Event, kind string) Event {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				t.Fatalf("the stream ended before a %q event", kind)
			}
			if e.Kind == kind {
				return e
			}
		case <-deadline:
			t.Fatalf("no %q event arrived", kind)
		}
	}
}

func fields(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("the payload was not an object: %s", raw)
	}
	return obj
}

// THE MERGED STREAM: every room, one connection, and every event says where it
// came from and carries an id the board can click.
func TestTheHubStreamCarriesEveryRoomTagged(t *testing.T) {
	a, b := make(chan string, 4), make(chan string, 4)
	front, _, done := two(t, streamer(a), streamer(b))
	defer done()

	ch, shut := listen(t, front.URL+"/v1/events/hub")
	defer shut()

	// Both rooms speak. The pump takes a moment to attach, so this is sent
	// until it lands rather than once.
	go func() {
		for i := 0; i < 40; i++ {
			a <- sse("task", `{"id":"card1","title":"from alpha"}`)
			b <- sse("task", `{"id":"card2","title":"from beta"}`)
			time.Sleep(100 * time.Millisecond)
		}
	}()

	seen := map[string]string{}
	deadline := time.After(8 * time.Second)
	for len(seen) < 2 {
		select {
		case e := <-ch:
			if e.Kind != "task" {
				continue
			}
			obj := fields(t, e.Data)
			room, _ := obj["room"].(string)
			id, _ := obj["id"].(string)
			if room == "" {
				t.Fatalf("an event lost its room: %s", e.Data)
			}
			seen[room] = id
		case <-deadline:
			t.Fatalf("only saw %v", seen)
		}
	}
	// THE ID IS TAGGED, matching what `/v1/tasks` handed the board, or the
	// board would be told about a card it has never heard of.
	if seen["alpha"] != "alpha~card1" {
		t.Errorf("alpha's id came through as %q", seen["alpha"])
	}
	if seen["beta"] != "beta~card2" {
		t.Errorf("beta's id came through as %q", seen["beta"])
	}
}

// A ROOM-SCOPED STREAM IS THAT ROOM'S STREAM, byte for byte in meaning. No tag,
// no room field, because a board scoped to one room asked that room directly.
func TestARoomStreamArrivesUntouched(t *testing.T) {
	a, b := make(chan string, 4), make(chan string, 4)
	front, _, done := two(t, streamer(a), streamer(b))
	defer done()

	ch, shut := listen(t, front.URL+"/v1/events/room/beta")
	defer shut()

	go func() {
		for i := 0; i < 40; i++ {
			a <- sse("task", `{"id":"card1"}`)
			b <- sse("task", `{"id":"card2"}`)
			time.Sleep(100 * time.Millisecond)
		}
	}()

	e := waitEvent(t, ch, "task")
	obj := fields(t, e.Data)
	if obj["id"] != "card2" {
		t.Fatalf("a scoped stream carried %v, so it was either tagged or crossed", obj["id"])
	}
	if _, ok := obj["room"]; ok {
		t.Errorf("a scoped stream was rewritten: %s", e.Data)
	}
}

// Membership is an event, so the room counter in the header is live rather
// than polled.
func TestTheStreamSaysWhichRoomsAreAttached(t *testing.T) {
	a, b := make(chan string, 1), make(chan string, 1)
	front, _, done := two(t, streamer(a), streamer(b))
	defer done()

	ch, shut := listen(t, front.URL+"/v1/events/hub")
	defer shut()

	e := waitEvent(t, ch, "rooms")
	obj := fields(t, e.Data)
	rooms, _ := obj["rooms"].([]any)
	if len(rooms) != 2 {
		t.Fatalf("the hub named %v rooms", obj["rooms"])
	}
}

// One room means the lists come through untagged, so the stream must be
// untagged too or the two describe different cards.
func TestOneRoomStreamsUntagged(t *testing.T) {
	say := make(chan string, 4)
	front, _, done := pair(t, streamer(say))
	defer done()

	ch, shut := listen(t, front.URL+"/v1/events/hub")
	defer shut()

	go func() {
		for i := 0; i < 40; i++ {
			say <- sse("task", `{"id":"card1"}`)
			time.Sleep(100 * time.Millisecond)
		}
	}()

	e := waitEvent(t, ch, "task")
	obj := fields(t, e.Data)
	if obj["id"] != "card1" {
		t.Fatalf("a single room's event was tagged: %s", e.Data)
	}
}

// The address the board has always used still works, and picks the right one
// of the two behaviours.
func TestThePlainEventsPathStillAnswers(t *testing.T) {
	say := make(chan string, 4)
	front, _, done := pair(t, streamer(say))
	defer done()

	ch, shut := listen(t, front.URL+"/v1/events")
	defer shut()

	go func() {
		for i := 0; i < 40; i++ {
			say <- sse("task", `{"id":"card1"}`)
			time.Sleep(100 * time.Millisecond)
		}
	}()
	waitEvent(t, ch, "task")
}

// A room that is not there is said so, rather than hanging a stream that will
// never carry anything.
func TestAnUnknownRoomStreamIsRefused(t *testing.T) {
	front, _, done := pair(t, streamer(make(chan string)))
	defer done()

	res, err := http.Get(front.URL + "/v1/events/room/nowhere")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("got %d, expected 503", res.StatusCode)
	}
}

// The upstream streams exist to serve connected boards. With nobody watching,
// the hub must not be holding one open per room across the network.
func TestNobodyWatchingMeansNothingStreaming(t *testing.T) {
	a, b := make(chan string, 1), make(chan string, 1)
	_, hub, done := two(t, streamer(a), streamer(b))
	defer done()

	// Its own front, so the pump count belongs to this test alone.
	p := NewProxy(hub, nil, "", nil)
	front := httptest.NewServer(p)
	defer front.Close()

	ch, shut := listen(t, front.URL+"/v1/events/hub")
	waitEvent(t, ch, "rooms")
	waitFor(t, 5*time.Second, func() bool { return p.feeds.count() == 2 })

	// The handler notices the client is gone, drops the last subscriber, and
	// the reconciler tears every pump down with it.
	shut()
	waitFor(t, 5*time.Second, func() bool { return p.feeds.count() == 0 })
}

// MEMBERSHIP CHANGES UNDER AN OPEN STREAM, and the stream has to change with
// it. A board that connected when one room was attached would otherwise keep
// hearing bare ids after a second room joined, while the lists had already
// started answering tagged ones.
func TestTaggingFollowsMembershipOnAnOpenStream(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	hub := NewHub(Timings{Beat: 200 * time.Millisecond, Silence: 2 * time.Second, Warm: 2})
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go func() { _ = hub.Serve(ctx, ln) }()

	first := make(chan string, 8)
	one := &Room{Name: "alpha", Dial: plain{addr: ln.Addr().String()}, Handler: streamer(first),
		T: Timings{Beat: 200 * time.Millisecond, Warm: 2, Backoff: 50 * time.Millisecond}}
	go func() { _ = one.Run(ctx) }()
	waitFor(t, 5*time.Second, func() bool { return hub.Has("alpha") })

	front := httptest.NewServer(NewProxy(hub, nil, "", nil))
	defer front.Close()

	ch, shut := listen(t, front.URL+"/v1/events/hub")
	defer shut()

	// One room: untagged, matching what the lists answer.
	go func() {
		for i := 0; i < 30; i++ {
			first <- sse("task", `{"id":"card1"}`)
			time.Sleep(100 * time.Millisecond)
		}
	}()
	if id := fields(t, waitEvent(t, ch, "task").Data)["id"]; id != "card1" {
		t.Fatalf("one room tagged its events: %v", id)
	}

	// A second room attaches. The lists start tagging, so this stream must too.
	second := make(chan string, 8)
	two := &Room{Name: "beta", Dial: plain{addr: ln.Addr().String()}, Handler: streamer(second),
		T: Timings{Beat: 200 * time.Millisecond, Warm: 2, Backoff: 50 * time.Millisecond}}
	go func() { _ = two.Run(ctx) }()
	waitFor(t, 5*time.Second, func() bool { return hub.Has("beta") })

	deadline := time.After(8 * time.Second)
	for {
		select {
		case e := <-ch:
			if e.Kind != "task" {
				continue
			}
			if fields(t, e.Data)["id"] == "alpha~card1" {
				return
			}
		case <-deadline:
			t.Fatal("the stream never started tagging after a second room attached")
		}
	}
}

// A ROOM WHOSE LISTENER STOPS IS GONE, not frozen in the list.
//
// A hub that is also a room serves a second listener, and turning that off
// cancels it while the hub keeps running. Nothing is watching that room any
// more, so nothing would ever notice it had stopped beating.
func TestARoomWhoseListenerStopsIsForgotten(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	hub := NewHub(Timings{Beat: 200 * time.Millisecond, Silence: 30 * time.Second, Warm: 1})

	// Its own context, the way a hub's own room gets one.
	own, stopOwn := context.WithCancel(context.Background())
	go func() { _ = hub.Serve(own, ln) }()

	rctx, stopRoom := context.WithCancel(context.Background())
	defer stopRoom()
	r := &Room{Name: "inhouse", Dial: plain{addr: ln.Addr().String()},
		Handler: http.NotFoundHandler(),
		T:       Timings{Beat: 200 * time.Millisecond, Warm: 1, Backoff: 50 * time.Millisecond}}
	go func() { _ = r.Run(rctx) }()
	waitFor(t, 5*time.Second, func() bool { return hub.Has("inhouse") })

	// The listener stops. Well inside `Silence`, so a heartbeat timeout cannot
	// be what removes it.
	stopOwn()
	waitFor(t, 5*time.Second, func() bool { return !hub.Has("inhouse") })
}

// A room's own tagging table has to agree with the list one, because the board
// builds a url from whichever it saw last.
func TestEventTaggingMatchesTheListTagging(t *testing.T) {
	e := Event{Room: "sg4", Kind: "permission", Data: []byte(`{"id":"p1","task_id":"c1"}`)}
	obj := fields(t, tagEvent(e))
	if obj["id"] != "sg4~p1" || obj["task_id"] != "sg4~c1" {
		t.Fatalf("a permission came out %v", obj)
	}
	if obj["room"] != "sg4" {
		t.Errorf("a permission lost its room: %v", obj)
	}

	// An event with no ids still says where it came from.
	plain := fields(t, tagEvent(Event{Room: "sg4", Kind: "settings", Data: []byte(`{"auto":true}`)}))
	if plain["room"] != "sg4" || plain["auto"] != true {
		t.Fatalf("a payload with no ids was mangled: %v", plain)
	}
}
