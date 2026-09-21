package link

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// REPRODUCING THE ATTACH-WS FLICKER, at the layer that owns it.
//
// The board attaches a terminal by opening a websocket to /v1/tasks/<id>/attach.
// It is always served THROUGH the hub proxy, never straight to a room, so the
// upgrade rides a pooled link connection. The symptom is a socket that closes
// before it is established: the browser never sees onopen, and the client retry
// loop spins. It is worse with more rooms.
//
// This mimics the daemon's attach handler with coder/websocket (accept, send a
// text caps frame, then hold the socket open) and drives real websocket dials
// through the hub, both one at a time and concurrently, to see whether every one
// establishes.

// attachLikeDaemon is a stand-in for internal/daemon.attach: it accepts the
// upgrade, writes one text frame the way the real handler writes caps, and holds
// the socket until the client goes.
func attachLikeDaemon(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/attach") {
		http.NotFound(w, r)
		return
	}
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer c.CloseNow()
	ctx := r.Context()
	_ = c.Write(ctx, websocket.MessageText, []byte(`{"t":"caps"}`))
	// Hold the socket. A real attach then writes backlog and streams output.
	for {
		_, _, err := c.Read(ctx)
		if err != nil {
			return
		}
	}
}

// dialAttach opens one real attach websocket through the front (hub) and reports
// whether it established and received the caps frame.
func dialAttach(t *testing.T, frontURL, path string) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(frontURL, "http") + path
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer c.CloseNow()
	typ, data, err := c.Read(ctx)
	if err != nil {
		return fmt.Errorf("read caps: %w", err)
	}
	if typ != websocket.MessageText || !strings.Contains(string(data), "caps") {
		return fmt.Errorf("first frame was %v %q, wanted the caps text frame", typ, data)
	}
	return nil
}

// A SINGLE ROOM, ATTACHES ONE AT A TIME. Each opens, gets caps, closes. Nothing
// long-lived is held, so this is the pool at its healthiest.
func TestAttachEstablishesThroughTheHubRepeatedly(t *testing.T) {
	front, _, done := pair(t, http.HandlerFunc(attachLikeDaemon))
	defer done()

	for i := 0; i < 30; i++ {
		if err := dialAttach(t, front.URL, "/v1/tasks/card"+fmt.Sprint(i)+"/attach"); err != nil {
			t.Fatalf("attach %d never established: %v", i, err)
		}
	}
}

// TERMINALS ARE HELD OPEN, and the board keeps polling while they are. This is
// the real shape: a few long-lived attaches sitting on pooled connections, then
// more attaches arriving. If holding sockets starves the pool, a later attach
// fails to establish.
func TestAttachEstablishesWhileOthersAreHeldOpen(t *testing.T) {
	front, _, done := pair(t, http.HandlerFunc(attachLikeDaemon))
	defer done()

	// Hold several terminals open, more than the warm pool (2 in pair()).
	var held []*websocket.Conn
	for i := 0; i < 6; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		url := "ws" + strings.TrimPrefix(front.URL, "http") + "/v1/tasks/held" + fmt.Sprint(i) + "/attach"
		c, _, err := websocket.Dial(ctx, url, nil)
		cancel()
		if err != nil {
			t.Fatalf("could not hold terminal %d open: %v", i, err)
		}
		held = append(held, c)
	}
	defer func() {
		for _, c := range held {
			c.CloseNow()
		}
	}()

	// A new terminal must still attach.
	if err := dialAttach(t, front.URL, "/v1/tasks/newone/attach"); err != nil {
		t.Fatalf("a new attach could not establish while others were held: %v", err)
	}
}

// CONCURRENT ATTACHES, which is a restart reloading every popped-out window at
// once, and the board reattaching everything it had open. Several upgrades race
// for pooled connections at the same instant.
func TestConcurrentAttachesAllEstablish(t *testing.T) {
	front, _, done := pair(t, http.HandlerFunc(attachLikeDaemon))
	defer done()

	const n = 12
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- dialAttach(t, front.URL, "/v1/tasks/c"+fmt.Sprint(i)+"/attach")
		}(i)
	}
	wg.Wait()
	close(errs)
	failed := 0
	for err := range errs {
		if err != nil {
			failed++
			t.Logf("a concurrent attach failed: %v", err)
		}
	}
	if failed > 0 {
		t.Fatalf("%d of %d concurrent attaches never established", failed, n)
	}
}

// TWO ROOMS, which is the case the flicker gets worse in. Attaches are addressed
// to a named room by tag, held open on both, while new ones keep arriving.
func TestAttachAcrossTwoRoomsWhileHeld(t *testing.T) {
	front, _, done := twoRooms(t, http.HandlerFunc(attachLikeDaemon))
	defer done()

	var held []*websocket.Conn
	defer func() {
		for _, c := range held {
			c.CloseNow()
		}
	}()
	for _, room := range []string{"alpha", "beta"} {
		for i := 0; i < 5; i++ {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			url := "ws" + strings.TrimPrefix(front.URL, "http") +
				"/v1/tasks/" + room + "~held" + fmt.Sprint(i) + "/attach"
			c, _, err := websocket.Dial(ctx, url, nil)
			cancel()
			if err != nil {
				t.Fatalf("could not hold %s terminal %d: %v", room, i, err)
			}
			held = append(held, c)
		}
	}
	for _, room := range []string{"alpha", "beta"} {
		if err := dialAttach(t, front.URL, "/v1/tasks/"+room+"~fresh/attach"); err != nil {
			t.Fatalf("a fresh attach on %s could not establish: %v", room, err)
		}
	}
}

// A BARE ID WITH TWO ROOMS ATTACHED, which is what a terminal reconnects with
// when it was opened while one room was live and a second joined since, and what
// a solo window carries from localStorage. The hub must resolve the owning room
// and establish the upgrade. A room that is slow to answer the card lookup must
// not stall the attach to the room that owns it.
func TestBareIdAttachWithTwoRoomsIsNotStalledBySlowRoom(t *testing.T) {
	// beta owns nothing and is slow to say so; alpha owns the card.
	slow := make(chan struct{})
	defer close(slow)
	handlerFor := func(name string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/attach") {
				attachLikeDaemon(w, r)
				return
			}
			// GET /v1/tasks/<id>: alpha owns "wanted", beta owns nothing and is slow.
			if name == "alpha" && strings.Contains(r.URL.Path, "wanted") {
				fmt.Fprint(w, `{"id":"wanted","supervised":true}`)
				return
			}
			if name == "beta" {
				select {
				case <-slow:
				case <-time.After(3 * time.Second):
				case <-r.Context().Done():
				}
			}
			http.NotFound(w, r)
		})
	}
	front, _, done := twoRoomsWith(t, handlerFor)
	defer done()

	start := time.Now()
	if err := dialAttach(t, front.URL, "/v1/tasks/wanted/attach"); err != nil {
		t.Fatalf("bare-id attach to the owning room never established: %v", err)
	}
	if took := time.Since(start); took > 1*time.Second {
		t.Fatalf("attach to the owning room waited %s on the slow room's card lookup; "+
			"the upgrade should not be gated on a fan-out to every room", took)
	}
}

// twoRoomsWith stands up two rooms, each with its own handler built from its name.
func twoRoomsWith(t *testing.T, handlerFor func(name string) http.Handler) (*httptest.Server, *Hub, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hub := NewHub(Timings{Beat: 200 * time.Millisecond, Silence: 2 * time.Second, Warm: 2})
	ctx, stop := context.WithCancel(context.Background())
	go func() { _ = hub.Serve(ctx, ln) }()
	for _, name := range []string{"alpha", "beta"} {
		room := &Room{
			Name: name, Dial: plain{addr: ln.Addr().String()}, Handler: handlerFor(name),
			T: Timings{Beat: 200 * time.Millisecond, Warm: 2, Backoff: 50 * time.Millisecond},
		}
		go func() { _ = room.Run(ctx) }()
	}
	waitFor(t, 5*time.Second, func() bool { return hub.Has("alpha") && hub.Has("beta") })
	front := httptest.NewServer(NewProxy(hub, nil, "", nil))
	return front, hub, func() {
		front.Close()
		stop()
		ln.Close()
	}
}

// twoRooms stands up a hub with two attached rooms serving the same handler.
func twoRooms(t *testing.T, handler http.Handler) (*httptest.Server, *Hub, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hub := NewHub(Timings{Beat: 200 * time.Millisecond, Silence: 2 * time.Second, Warm: 2})
	ctx, stop := context.WithCancel(context.Background())
	go func() { _ = hub.Serve(ctx, ln) }()

	for _, name := range []string{"alpha", "beta"} {
		room := &Room{
			Name: name, Dial: plain{addr: ln.Addr().String()}, Handler: handler,
			T: Timings{Beat: 200 * time.Millisecond, Warm: 2, Backoff: 50 * time.Millisecond},
		}
		go func() { _ = room.Run(ctx) }()
	}
	waitFor(t, 5*time.Second, func() bool { return hub.Has("alpha") && hub.Has("beta") })

	front := httptest.NewServer(NewProxy(hub, nil, "", nil))
	return front, hub, func() {
		front.Close()
		stop()
		ln.Close()
	}
}
