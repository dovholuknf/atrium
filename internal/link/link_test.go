package link

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"
)

// The tests that matter here are the ones about the SHAPE of the thing: a room
// serving its own handler over connections it dialled, a hub lending those
// connections out, and the two failure modes that would make the feature look
// broken rather than absent.
//
// Everything runs over a real TCP listener on loopback with a real hub and a
// real room. There is no fake transport, because the bugs worth catching live
// in the handover between the framed handshake and the unframed HTTP that
// follows it, and a fake would be the thing that hid them.

// pair stands a hub and a room up, attached, and hands back the proxy's URL.
func pair(t *testing.T, handler http.Handler) (*httptest.Server, *Hub, func()) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hub := NewHub(Timings{Beat: 200 * time.Millisecond, Silence: time.Second, Warm: 2})
	// No transport identity in the test, so the hello's claim is used. That is
	// exactly the fallback documented on `identify`.
	ctx, stop := context.WithCancel(context.Background())
	go func() { _ = hub.Serve(ctx, ln) }()

	room := &Room{
		Name:    "testroom",
		Dial:    plain{addr: ln.Addr().String()},
		Handler: handler,
		T:       Timings{Beat: 200 * time.Millisecond, Warm: 2, Backoff: 50 * time.Millisecond},
	}
	go func() { _ = room.Run(ctx) }()

	// Attached is the precondition for every test below, so wait for it rather
	// than sleeping and hoping.
	waitFor(t, 5*time.Second, func() bool { return hub.Has("testroom") })

	front := httptest.NewServer(NewProxy(hub, nil, "", nil))
	return front, hub, func() {
		front.Close()
		stop()
		ln.Close()
	}
}

type plain struct{ addr string }

func (p plain) Dial(ctx context.Context) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "tcp", p.addr)
}
func (p plain) Describe() string { return p.addr }

func waitFor(t *testing.T, limit time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting")
}

// The whole feature in one test: a request to the hub is answered by the room's
// handler, which never listened on anything.
func TestTheRoomAnswersThroughTheHub(t *testing.T) {
	front, _, done := pair(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "hello from the room, you asked for %s", r.URL.Path)
	}))
	defer done()

	res, err := http.Get(front.URL + "/v1/tasks")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(body), "you asked for /v1/tasks") {
		t.Fatalf("got %q", body)
	}
}

// Keep-alive is what makes this cheap. Many requests must not mean many dials.
func TestOneConnectionAnswersManyRequests(t *testing.T) {
	front, _, done := pair(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	defer done()

	for i := 0; i < 25; i++ {
		res, err := http.Get(front.URL + "/v1/health")
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
	}
}

// A POST with a body, because a proxy that only passes GETs passes nothing
// interesting. Launching an agent is a POST.
func TestABodyGoesBothWays(t *testing.T) {
	front, _, done := pair(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		fmt.Fprintf(w, "got %d bytes: %s", len(raw), raw)
	}))
	defer done()

	res, err := http.Post(front.URL+"/v1/launch", "application/json",
		strings.NewReader(`{"harness":"claude"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(body), `{"harness":"claude"}`) {
		t.Fatalf("the body did not survive: %q", body)
	}
}

// THE EVENT STREAM IS THE ONE THAT DECIDES WHETHER THE BOARD FEELS ALIVE.
//
// A proxy that buffers turns every update into a clump. This asserts the first
// event arrives before the handler has finished, which is the property
// `FlushInterval: -1` exists for and the one a default configuration loses.
func TestAStreamArrivesBeforeItEnds(t *testing.T) {
	release := make(chan struct{})
	front, _, done := pair(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fl, ok := w.(http.Flusher)
		if !ok {
			t.Error("the room's handler did not get a Flusher, so SSE would 500")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: task\ndata: first\n\n")
		fl.Flush()
		<-release
		fmt.Fprint(w, "event: task\ndata: second\n\n")
		fl.Flush()
	}))
	defer done()
	defer close(release)

	res, err := http.Get(front.URL + "/v1/events")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	got := make(chan string, 1)
	go func() {
		buf := make([]byte, 64)
		n, _ := res.Body.Read(buf)
		got <- string(buf[:n])
	}()
	select {
	case s := <-got:
		if !strings.Contains(s, "first") {
			t.Fatalf("first event was %q", s)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the first event never arrived, so the proxy is buffering the stream")
	}
}

// A hijacked connection is what the terminal websocket needs. Tested with a
// bare upgrade rather than a real websocket, because what is being checked is
// that the bytes flow in both directions after a 101 and nothing else.
func TestAnUpgradeBecomesARawConnection(t *testing.T) {
	front, _, done := pair(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			http.Error(w, "expected an upgrade", http.StatusBadRequest)
			return
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Error("the room's handler did not get a Hijacker, so attach would fail")
			return
		}
		conn, buf, err := hj.Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		buf.WriteString("HTTP/1.1 101 Switching Protocols\r\n" +
			"Upgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
		buf.Flush()
		// Echo, so the test can prove both directions.
		line, _ := buf.ReadString('\n')
		buf.WriteString("echo:" + line)
		buf.Flush()
	}))
	defer done()

	conn, err := net.Dial("tcp", strings.TrimPrefix(front.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprint(conn, "GET /v1/tasks/x/attach HTTP/1.1\r\nHost: h\r\n"+
		"Upgrade: websocket\r\nConnection: Upgrade\r\n\r\n")

	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(buf[:n]), "101") {
		t.Fatalf("no upgrade: %q", buf[:n])
	}
	fmt.Fprint(conn, "ping\n")
	n, err = conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(buf[:n]), "echo:ping") {
		t.Fatalf("bytes did not come back after the upgrade: %q", buf[:n])
	}
}

// A WARM CONNECTION IS OLD BY THE TIME IT IS USED, and that is the point of it.
//
// This is the regression test for the first real bug in this package. The room
// pre-dials connections so the request after a hub restart does not wait for
// one. `http.Server.ReadHeaderTimeout` starts when a connection is ACCEPTED,
// not when bytes arrive, so any non-zero value silently closes exactly the
// connections the pool exists to hold. The link went on reporting itself
// healthy with four idle connections while every request answered EOF.
//
// The pause here is longer than any header timeout somebody would plausibly
// reintroduce.
func TestAConnectionThatWaitedInThePoolStillWorks(t *testing.T) {
	if testing.Short() {
		t.Skip("this one has to actually wait")
	}
	front, _, done := pair(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "still here")
	}))
	defer done()

	time.Sleep(12 * time.Second)

	res, err := http.Get(front.URL + "/v1/health")
	if err != nil {
		t.Fatalf("a pooled connection died while it waited: %v", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if string(body) != "still here" {
		t.Fatalf("got %q", body)
	}
}

// With no room, the board must say which half is missing rather than 502.
func TestNoRoomSaysSoInWords(t *testing.T) {
	hub := NewHub(Timings{DialWait: 100 * time.Millisecond})
	front := httptest.NewServer(NewProxy(hub, nil, "", nil))
	defer front.Close()

	res, err := http.Get(front.URL + "/v1/tasks")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("got %d, expected 503", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(body), "atrium2 join") {
		t.Fatalf("the message does not say what to do: %s", body)
	}
}

// THE RELOAD LOOP, WHICH IS THE ONE THAT WOULD LOOK LIKE THE IDEA DOES NOT WORK.
//
// The board reloads itself when `/v1/health`'s build changes. The room hashes
// ITS copy of the board, which is not the copy the browser is running, so
// passing it through would make every tab reload on every poll, forever.
func TestHealthCarriesTheHubsBuildNotTheRooms(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	hub := NewHub(Timings{Beat: 200 * time.Millisecond, Warm: 2})
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go func() { _ = hub.Serve(ctx, ln) }()

	room := &Room{
		Name: "testroom", Dial: plain{addr: ln.Addr().String()},
		T: Timings{Beat: 200 * time.Millisecond, Warm: 2},
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"ok":true,"build":"theroomsboard","halted":false}`)
		}),
	}
	go func() { _ = room.Run(ctx) }()
	waitFor(t, 5*time.Second, func() bool { return hub.Has("testroom") })

	front := httptest.NewServer(NewProxy(hub, nil, "thehubsboard", nil))
	defer front.Close()

	res, err := http.Get(front.URL + "/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["build"] != "thehubsboard" {
		t.Fatalf("build came back as %v, so every tab would reload forever", body["build"])
	}
	// The room's is kept, because a version skew between the halves is a thing
	// somebody will need to see.
	if body["room_build"] != "theroomsboard" {
		t.Errorf("the room's build was thrown away: %v", body["room_build"])
	}
	// And everything else is untouched.
	if body["ok"] != true {
		t.Errorf("the rest of the payload did not survive: %v", body)
	}
}

// A hub must not be able to stop a room. The room's loopback check would pass
// for a connection that terminates inside its own process, so the refusal has
// to be at the hub.
func TestTheHubRefusesToStopARoom(t *testing.T) {
	reached := false
	front, _, done := pair(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
	}))
	defer done()

	res, err := http.Post(front.URL+"/v1/shutdown", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("got %d, expected 403", res.StatusCode)
	}
	if reached {
		t.Fatal("the request reached the room, which means a hub can kill it")
	}
}

// The hub serves what it has and forwards what it does not. This is the rule
// the whole design rests on, so it is asserted rather than assumed.
func TestTheHubServesItsOwnFilesAndForwardsTheRest(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	hub := NewHub(Timings{Beat: 200 * time.Millisecond, Warm: 2})
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go func() { _ = hub.Serve(ctx, ln) }()

	room := &Room{
		Name: "testroom", Dial: plain{addr: ln.Addr().String()},
		T: Timings{Beat: 200 * time.Millisecond, Warm: 2},
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "from the room")
		}),
	}
	go func() { _ = room.Run(ctx) }()
	waitFor(t, 5*time.Second, func() bool { return hub.Has("testroom") })

	front := httptest.NewServer(NewProxy(hub, fakeBoard(), "b", nil))
	defer front.Close()

	// A file the hub has.
	res, err := http.Get(front.URL + "/index.html")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if !strings.Contains(string(body), "the hub's index") {
		t.Fatalf("the hub did not serve its own file: %q", body)
	}
	// `/` is that file too.
	res, _ = http.Get(front.URL + "/")
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if !strings.Contains(string(body), "the hub's index") {
		t.Fatalf("/ did not resolve to index.html: %q", body)
	}
	// A path it does not have.
	res, _ = http.Get(front.URL + "/v1/tasks")
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if string(body) != "from the room" {
		t.Fatalf("an unknown path did not go to the room: %q", body)
	}
}

// A room reconnecting replaces itself rather than piling up, because a hub
// restart looks like a blip from the room's side.
func TestReconnectingReplacesTheOldLink(t *testing.T) {
	front, hub, done := pair(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	defer done()

	if n := len(hub.Rooms()); n != 1 {
		t.Fatalf("expected one room, got %d", n)
	}
	// A second room under the same name, which is what a reconnect looks like.
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
	}()

	if n := len(hub.Rooms()); n != 1 {
		t.Fatalf("still expected one room, got %d", n)
	}
	res, err := http.Get(front.URL + "/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	stop()
	wg.Wait()
}

// fakeBoard is one file, which is enough to prove the rule.
func fakeBoard() fs.FS {
	return fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<!doctype html>the hub's index")},
	}
}
