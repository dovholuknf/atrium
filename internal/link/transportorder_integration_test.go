//go:build integration

package link

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A hub on two listeners, and a room with a ladder over both. The "direct" and
// "ziti" here are plain TCP: what is under test is the order, the fallback, the
// move back and what the hub says about it, not either transport.
// See docs/rnd/room-transport-order.md.

type switchable struct {
	addr string
	down atomic.Bool
}

func (s *switchable) Dial(ctx context.Context) (net.Conn, error) {
	if s.down.Load() {
		return nil, errors.New("connection refused")
	}
	var d net.Dialer
	return d.DialContext(ctx, "tcp", s.addr)
}

func (s *switchable) Describe() string { return s.addr }

func attachedAs(h *Hub, name string) (Attached, bool) {
	for _, a := range h.Rooms() {
		if a.Name == name {
			return a, true
		}
	}
	return Attached{}, false
}

func TestRoomFallsBackInOrderAndComesBack(t *testing.T) {
	lightSlot(t)
	lnD, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	lnZ, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hub := NewHub(Timings{Beat: 100 * time.Millisecond, Silence: time.Second, Warm: 1})
	hub.Paths = func() []Path {
		return []Path{{Transport: "direct", Addr: lnD.Addr().String()}, {Transport: "ziti", Service: "atrium-hub"}}
	}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go func() { _ = hub.ServeVia(ctx, lnD, "direct") }()
	go func() { _ = hub.ServeVia(ctx, lnZ, "ziti") }()

	direct := &switchable{addr: lnD.Addr().String()}
	direct.down.Store(true)
	ziti := &switchable{addr: lnZ.Addr().String()}
	ladder := NewLadder([]Rung{
		{Path: Path{Transport: "direct"}, Dial: direct},
		{Path: Path{Transport: "ziti"}, Dial: ziti},
	})
	var mu sync.Mutex
	var heard []Path
	room := &Room{
		Name: "m1mini", Dial: ladder,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }),
		T: Timings{Beat: 100 * time.Millisecond, Warm: 1, Backoff: 20 * time.Millisecond,
			ProbeEvery: 50 * time.Millisecond},
		OnPaths: func(ps []Path) {
			mu.Lock()
			heard = ps
			mu.Unlock()
		},
	}
	go func() { _ = room.Run(ctx) }()

	waitFor(t, 5*time.Second, func() bool { return hub.Has("m1mini") })
	a, _ := attachedAs(hub, "m1mini")
	if a.Via != "ziti" || !a.Fallback {
		t.Fatalf("attached via %q fallback=%v, want ziti as a fallback", a.Via, a.Fallback)
	}
	if s := room.State(); s.Transport != "ziti" || !s.Fallback {
		t.Fatalf("the room's own state says %q fallback=%v", s.Transport, s.Fallback)
	}
	waitFor(t, 5*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(heard) == 2 && heard[0].Transport == "direct"
	})

	// Direct comes back. Two good probes later the room moves up on its own.
	direct.down.Store(false)
	waitFor(t, 10*time.Second, func() bool {
		a, ok := attachedAs(hub, "m1mini")
		return ok && a.Via == "direct" && !a.Fallback
	})
	if s := room.State(); s.Transport != "direct" || s.Fallback {
		t.Fatalf("after moving up the room says %q fallback=%v", s.Transport, s.Fallback)
	}
}

// A room the hub refuses does not walk the ladder: the answer would be the same
// on every transport.
func TestARefusalDoesNotFallBack(t *testing.T) {
	lightSlot(t)
	lnD, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	lnZ, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hub := NewHub(Timings{Beat: 100 * time.Millisecond, Silence: time.Second, Warm: 1})
	hub.Attaching = func(name, host, version string) error { return errors.New("no record of " + name) }
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go func() { _ = hub.ServeVia(ctx, lnD, "direct") }()
	var viaZiti atomic.Int32
	go func() {
		for {
			c, err := lnZ.Accept()
			if err != nil {
				return
			}
			viaZiti.Add(1)
			c.Close()
		}
	}()
	ladder := NewLadder([]Rung{
		{Path: Path{Transport: "direct"}, Dial: &switchable{addr: lnD.Addr().String()}},
		{Path: Path{Transport: "ziti"}, Dial: &switchable{addr: lnZ.Addr().String()}},
	})
	room := &Room{Name: "ghost", Dial: ladder, Handler: http.NotFoundHandler(),
		T: Timings{Beat: 100 * time.Millisecond, Backoff: 20 * time.Millisecond}}
	go func() { _ = room.Run(ctx) }()
	waitFor(t, 5*time.Second, func() bool { return room.State().Error != "" })
	time.Sleep(300 * time.Millisecond)
	if n := viaZiti.Load(); n != 0 {
		t.Fatalf("a refusal on direct sent the room to ziti %d times", n)
	}
}
