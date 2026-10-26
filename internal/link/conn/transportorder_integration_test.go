//go:build integration

package conn

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
	addr  string
	down  atomic.Bool
	tries atomic.Int32
}

func (s *switchable) n() int32 { return s.tries.Load() }

func (s *switchable) Dial(ctx context.Context) (net.Conn, error) {
	s.tries.Add(1)
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

// claude-sg4 on 2026-10-14. The room joined at the address it reaches the hub on,
// and the hub's welcome advertised one from a network the hub had left. Learning
// that list must not move a single data dial off the address the control
// connection reached, or the pool never fills while the room looks attached.
func TestDataFollowsTheControlWhenTheHubAdvertisesAnotherAddress(t *testing.T) {
	lightSlot(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	const stale = "192.168.1.68:7779"
	hub := NewHub(Timings{Beat: 100 * time.Millisecond, Silence: time.Second, Warm: 2})
	hub.Paths = func() []Path { return []Path{{Transport: "direct", Addr: stale}} }
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go func() { _ = hub.ServeVia(ctx, ln, "direct") }()

	dialers := map[string]Dialer{ln.Addr().String(): &switchable{addr: ln.Addr().String()}}
	gone := &switchable{addr: stale}
	gone.down.Store(true)
	dialers[stale] = gone
	rungsFor := func(saved Saved, tr RoomTransports) []Rung {
		ps, _ := Candidates(saved, tr)
		var out []Rung
		for _, p := range ps {
			out = append(out, Rung{Path: p, Dial: dialers[p.Addr]})
		}
		return out
	}
	saved := Saved{Transport: "direct", Hub: ln.Addr().String(), Room: "claude-sg4"}
	ladder := NewLadder(rungsFor(saved, RoomTransports{}))
	// What learnHubPaths does: the hub's list written down and the ladder rebuilt.
	// Before the fix the advertise replaced the joined address right here.
	learn := func(ps []Path) {
		ladder.Set(rungsFor(saved, RoomTransports{Hub: ps}))
	}
	// The room's path watcher dials every rung it is not standing on, the stale
	// advertise included, so the board can show it as down. That is a probe and
	// it is meant. What must never reach the stale address is a data dial, and
	// those are the ones that come through Dial on the ladder.
	spy := &dataSpy{Ladder: ladder, want: ln.Addr().String()}
	room := &Room{
		Name: "claude-sg4", Dial: spy,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }),
		T:       Timings{Beat: 100 * time.Millisecond, Warm: 2, Backoff: 20 * time.Millisecond, DialWait: 200 * time.Millisecond},
		OnPaths: learn,
	}
	go func() { _ = room.Run(ctx) }()
	waitFor(t, 5*time.Second, func() bool { return hub.Has("claude-sg4") })

	// The hub asks for more, as a board request does, after the room learnt.
	dctx, dcancel := context.WithTimeout(ctx, 3*time.Second)
	defer dcancel()
	for i := 0; i < 3; i++ {
		c, err := hub.Dial(dctx, "claude-sg4")
		if err != nil {
			t.Fatalf("data connection %d never arrived: %v (data dials went to the advertise %d times)",
				i, err, spy.off.Load())
		}
		c.Close()
	}
	if n := spy.off.Load(); n != 0 {
		t.Fatalf("the room sent %d data dials to an address other than the one it joined on", n)
	}
}

// dataSpy is a ladder that counts the dials made on the rung it stands on that
// are not to `want`. Probes of the other rungs do not come through Dial.
type dataSpy struct {
	*Ladder
	want string
	off  atomic.Int32
}

func (d *dataSpy) Dial(ctx context.Context) (net.Conn, error) {
	if d.Ladder.Describe() != d.want {
		d.off.Add(1)
	}
	return d.Ladder.Dial(ctx)
}

// A room whose control connection is up while every data dial fails is flagged
// by the hub, not counted as reachable, and says why on its own state.
func TestTheHubFlagsARoomWhoseDataDialsFail(t *testing.T) {
	lightSlot(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hub := NewHub(Timings{Beat: 50 * time.Millisecond, Silence: time.Second, Warm: 1, Starve: 300 * time.Millisecond})
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go func() { _ = hub.ServeVia(ctx, ln, "direct") }()

	d := &onlyFirst{addr: ln.Addr().String()}
	room := &Room{
		Name: "starved", Dial: d, Handler: http.NotFoundHandler(),
		T: Timings{Beat: 50 * time.Millisecond, Warm: 1, Backoff: 20 * time.Millisecond, DialWait: 200 * time.Millisecond},
	}
	go func() { _ = room.Run(ctx) }()
	waitFor(t, 5*time.Second, func() bool {
		a, ok := attachedAs(hub, "starved")
		return ok && a.Starved
	})
	if s := room.State(); !s.Up || s.DataError == "" {
		t.Fatalf("the room's own state says %+v, want up with the data dial's error", s)
	}

	// Data dials get through again, and the next ask fills the pool.
	d.open.Store(true)
	dctx, dcancel := context.WithTimeout(ctx, 3*time.Second)
	defer dcancel()
	if c, err := hub.Dial(dctx, "starved"); err != nil {
		t.Fatal(err)
	} else {
		c.Close()
	}
	waitFor(t, 5*time.Second, func() bool {
		a, ok := attachedAs(hub, "starved")
		return ok && !a.Starved && room.State().DataError == ""
	})
}

// onlyFirst lets the first dial through, the control connection, and refuses
// every one after it until open.
type onlyFirst struct {
	addr  string
	first atomic.Bool
	open  atomic.Bool
}

func (o *onlyFirst) Dial(ctx context.Context) (net.Conn, error) {
	if o.first.Swap(true) && !o.open.Load() {
		return nil, errors.New("dial tcp 192.168.1.68:7779: i/o timeout")
	}
	var d net.Dialer
	return d.DialContext(ctx, "tcp", o.addr)
}

func (o *onlyFirst) Describe() string { return o.addr }

// The hub's order changes while the room stands on what is now the second rung.
// The room was not on a fallback when it attached, so nothing was probing.
func TestRoomFollowsAChangedOrder(t *testing.T) {
	for _, tc := range []struct {
		name      string
		topAnswer bool
	}{{"top rung answers, room moves", true}, {"top rung silent, room stays", false}} {
		t.Run(tc.name, func(t *testing.T) {
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
			ctx, stop := context.WithCancel(context.Background())
			defer stop()
			go func() { _ = hub.ServeVia(ctx, lnD, "direct") }()
			go func() { _ = hub.ServeVia(ctx, lnZ, "ziti") }()

			direct := &switchable{addr: lnD.Addr().String()}
			ziti := &switchable{addr: lnZ.Addr().String()}
			rungs := func(first string) []Rung {
				d := Rung{Path: Path{Transport: "direct"}, Dial: direct}
				z := Rung{Path: Path{Transport: "ziti"}, Dial: ziti}
				if first == "ziti" {
					return []Rung{z, d}
				}
				return []Rung{d, z}
			}
			ladder := NewLadder(rungs("ziti"))
			room := &Room{
				Name: "claude-sg4", Dial: ladder, Handler: http.NotFoundHandler(),
				T: Timings{Beat: 100 * time.Millisecond, Warm: 1, Backoff: 20 * time.Millisecond,
					DialWait: 200 * time.Millisecond, ProbeEvery: 50 * time.Millisecond},
			}
			go func() { _ = room.Run(ctx) }()
			waitFor(t, 5*time.Second, func() bool {
				a, ok := attachedAs(hub, "claude-sg4")
				return ok && a.Via == "ziti"
			})

			direct.down.Store(!tc.topAnswer)
			ladder.Set(rungs("direct"))
			if tc.topAnswer {
				waitFor(t, 10*time.Second, func() bool {
					a, ok := attachedAs(hub, "claude-sg4")
					return ok && a.Via == "direct" && !a.Fallback
				})
				return
			}
			// Probed, never answered: still on ziti.
			waitFor(t, 5*time.Second, func() bool { return direct.n() >= 3 })
			if a, ok := attachedAs(hub, "claude-sg4"); !ok || a.Via != "ziti" {
				t.Fatalf("attached %+v ok=%v, want still on ziti", a, ok)
			}
		})
	}
}
