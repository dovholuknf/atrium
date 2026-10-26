package link

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// See docs/rnd/room-transport-order.md for the rules these pin.

func kinds(ps []Path) string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Transport
	}
	return strings.Join(out, ",")
}

func TestParseOrderNamesKindsOnceAndRefusesTheUnknown(t *testing.T) {
	got, err := ParseOrder(" Direct, openziti ,direct,zrok")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "direct,ziti,zrok" {
		t.Fatalf("got %v, want direct,ziti,zrok (openziti is ziti, a repeat is dropped)", got)
	}
	if _, err := ParseOrder("direct,carrier-pigeon"); err == nil {
		t.Fatal("an unknown transport was accepted")
	}
	if got, err := ParseOrder(""); err != nil || len(got) != 0 {
		t.Fatalf("an empty order is no order: %v %v", got, err)
	}
}

func TestArrangeFollowsTheOrderAndPutsTheRestLast(t *testing.T) {
	ps := []Path{{Transport: "zrok"}, {Transport: "direct"}, {Transport: "ziti"}}
	if got := kinds(Arrange(ps, []string{"ziti"})); got != "ziti,direct,zrok" {
		t.Fatalf("got %s, want ziti first and the rest in the default order", got)
	}
	if got := kinds(Arrange(ps, nil)); got != "ziti,direct,zrok" {
		t.Fatalf("no order is the default order, got %s", got)
	}
}

// A room joined over direct that was told about a ziti identity locally climbs
// ziti then direct, with nothing heard from the hub. This is m1mini and sg3.
func TestCandidatesWithoutTheHubUseTheJoinAndTheLocalPaths(t *testing.T) {
	saved := Saved{Transport: "direct", Hub: "sg4:7779", Room: "m1mini"}
	got, skipped := Candidates(saved, RoomTransports{
		Paths: []Path{{Transport: "ziti", Identity: "/r/m1mini.json"}},
	})
	if kinds(got) != "ziti,direct" {
		t.Fatalf("got %s, want ziti,direct (the default puts ziti first)", kinds(got))
	}
	if got[1].Addr != "sg4:7779" || got[1].From != "join" {
		t.Fatalf("direct rung %+v, want the joined address from the join", got[1])
	}
	if got[0].Service != DefaultZitiService || got[0].From != "local" {
		t.Fatalf("ziti rung %+v, want the default service and a local identity", got[0])
	}
	if len(skipped) != 0 {
		t.Fatalf("nothing should be skipped: %v", skipped)
	}
}

// The precedence is join, then the hub, then what was typed on the machine. And
// the hub's order applies until the room has one of its own.
func TestCandidatesLayerJoinHubAndLocalInThatOrder(t *testing.T) {
	saved := Saved{Transport: "direct", Hub: "old:7779"}
	tr := RoomTransports{
		Hub:   []Path{{Transport: "ziti", Service: "hub-svc"}, {Transport: "direct", Addr: "new:7779"}},
		Paths: []Path{{Transport: "ziti", Identity: "/id.json", Service: "local-svc"}},
	}
	got, _ := Candidates(saved, tr)
	if kinds(got) != "ziti,direct,direct" {
		t.Fatalf("the hub's order applies with no local one: got %s", kinds(got))
	}
	if got[0].Service != "local-svc" {
		t.Fatalf("local wins over the hub: %+v", got)
	}
	if got[1].Addr != "old:7779" || got[1].From != "join" || got[2].Addr != "new:7779" || got[2].From != "hub" {
		t.Fatalf("the joined address and the hub's advertise are two rungs, the join first: %+v", got)
	}
	tr.Order = []string{"direct"}
	got, _ = Candidates(saved, tr)
	if kinds(got) != "direct,direct,ziti" {
		t.Fatalf("a local order wins over the hub's: got %s", kinds(got))
	}
}

// claude-sg4 on 2026-10-14: joined at 127.0.0.1:7779, on the hub's own machine,
// while the hub advertised 192.168.1.68:7779 from a network it had left. The
// advertise replaced the join, every data dial timed out, and the room looked
// attached because the control connection had been dialled first.
func TestCandidatesNeverLayTheHubsAdvertiseOverTheJoinedAddress(t *testing.T) {
	saved := Saved{Transport: "direct", Hub: "127.0.0.1:7779", Room: "claude-sg4"}
	tr := RoomTransports{Hub: []Path{{Transport: "direct", Addr: "192.168.1.68:7779"}}}
	got, _ := Candidates(saved, tr)
	if len(got) != 2 || got[0].Addr != "127.0.0.1:7779" || got[1].Addr != "192.168.1.68:7779" {
		t.Fatalf("got %+v, want the joined address first and the advertise after it", got)
	}
	// The same address from both is one rung.
	tr.Hub[0].Addr = "127.0.0.1:7779"
	if got, _ = Candidates(saved, tr); len(got) != 1 {
		t.Fatalf("one address said twice made %d rungs", len(got))
	}
	// An address typed on the machine is tried before the join's, and keeps it.
	tr.Paths = []Path{{Transport: "direct", Addr: "10.20.40.118:7779"}}
	got, _ = Candidates(saved, tr)
	if len(got) != 2 || got[0].Addr != "10.20.40.118:7779" || got[0].From != "local" || got[1].Addr != "127.0.0.1:7779" {
		t.Fatalf("got %+v, want local then join", got)
	}
}

// claude-sg4 runs on the hub's own machine and still attached over ziti, so every board call and keystroke paid the
// overlay's 85ms where loopback is 1ms. A direct rung on this machine's loopback climbs first whatever the order
// says, and ziti stays below it as the fallback.
func TestALoopbackDirectRungClimbsBeforeZitiWhateverTheOrderSays(t *testing.T) {
	saved := Saved{Transport: "direct", Hub: "127.0.0.1:7779", Room: "claude-sg4"}
	tr := RoomTransports{
		Hub:   []Path{{Transport: "ziti"}, {Transport: "direct", Addr: "192.168.1.68:7779"}},
		Paths: []Path{{Transport: "ziti", Identity: "/id.json"}},
		Order: []string{"ziti", "direct"},
	}
	got, _ := Candidates(saved, tr)
	if kinds(got) != "direct,ziti,direct" || got[0].Addr != "127.0.0.1:7779" || got[2].Addr != "192.168.1.68:7779" {
		t.Fatalf("got %+v, want loopback direct, then ziti, then the advertised direct", got)
	}
	// A hub on another machine keeps the order it was given.
	saved.Hub = "sg4:7779"
	tr.Hub = nil
	if got, _ = Candidates(saved, tr); kinds(got) != "ziti,direct" {
		t.Fatalf("a remote hub reordered to %s, want ziti,direct", kinds(got))
	}
	for addr, want := range map[string]bool{
		"127.0.0.1:7779": true, "localhost:7779": true, "[::1]:7779": true, "127.0.0.1": true,
		"192.168.1.68:7779": false, "sg4:7779": false, "": false,
	} {
		if got := isLoopbackDirect(Path{Transport: "direct", Addr: addr}); got != want {
			t.Errorf("isLoopbackDirect(%q) = %v, want %v", addr, got, want)
		}
	}
	if isLoopbackDirect(Path{Transport: "ziti", Addr: "127.0.0.1:1"}) {
		t.Error("a ziti rung is never lifted for its address")
	}
}

// The ladder stands on the rung the control connection reached, so a Set that
// drops that rung (the hub's list replaced it) does not move a single data dial.
func TestSetKeepsTheRungTheControlConnectionReached(t *testing.T) {
	here, gone := &fakeDial{name: "127.0.0.1:7779"}, &fakeDial{name: "192.168.1.68:7779"}
	gone.fail.Store(true)
	l := NewLadder([]Rung{{Path: Path{Transport: "direct", Addr: "127.0.0.1:7779"}, Dial: here}})
	if c, err := l.Climb(context.Background(), time.Second); err != nil {
		t.Fatal(err)
	} else {
		c.Close()
	}
	l.Set([]Rung{{Path: Path{Transport: "direct", Addr: "192.168.1.68:7779"}, Dial: gone}})
	c, err := l.Dial(context.Background())
	if err != nil {
		t.Fatalf("a data dial after Set went to the dropped address: %v", err)
	}
	c.Close()
	if gone.n.Load() != 0 {
		t.Fatal("a data dial went to an address the control connection never reached")
	}
	if !l.Fallback() {
		t.Fatal("standing on a rung the list no longer has is not the first choice")
	}
	// Two direct rungs: Set with both keeps the one that was reached, by address.
	l = NewLadder([]Rung{
		{Path: Path{Transport: "direct", Addr: "192.168.1.68:7779"}, Dial: gone},
		{Path: Path{Transport: "direct", Addr: "127.0.0.1:7779"}, Dial: here},
	})
	if c, err := l.Climb(context.Background(), time.Second); err != nil {
		t.Fatal(err)
	} else {
		c.Close()
	}
	l.Set(l.Rungs())
	if l.Describe() != "127.0.0.1:7779" {
		t.Fatalf("on %s after Set, want the address that answered", l.Describe())
	}
}

// A ziti rung without an identity cannot dial, so it is left out and said.
func TestCandidatesSkipARungThatCannotDial(t *testing.T) {
	got, skipped := Candidates(Saved{Transport: "direct", Hub: "h:1"},
		RoomTransports{Hub: []Path{{Transport: "direct", Addr: "h:1"}, {Transport: "ziti", Service: "s"}}})
	if kinds(got) != "direct" {
		t.Fatalf("got %s, want only direct", kinds(got))
	}
	if len(skipped) != 1 || !strings.Contains(skipped[0], "identity") {
		t.Fatalf("the skipped ziti rung should say it has no identity: %v", skipped)
	}
}

type fakeDial struct {
	name string
	fail atomic.Bool
	hang bool
	n    atomic.Int32
}

func (f *fakeDial) Dial(ctx context.Context) (net.Conn, error) {
	f.n.Add(1)
	if f.hang {
		<-make(chan struct{}) // like the ziti SDK, which takes no context
	}
	if f.fail.Load() {
		return nil, errors.New(f.name + " refused")
	}
	a, b := net.Pipe()
	go func() { _ = b.Close() }()
	return a, nil
}

func (f *fakeDial) Describe() string { return f.name }

func TestClimbTakesTheFirstRungThatAnswers(t *testing.T) {
	direct, ziti := &fakeDial{name: "direct"}, &fakeDial{name: "ziti"}
	direct.fail.Store(true)
	l := NewLadder([]Rung{{Path: Path{Transport: "direct"}, Dial: direct}, {Path: Path{Transport: "ziti"}, Dial: ziti}})
	c, err := l.Climb(context.Background(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if l.Transport() != "ziti" || !l.Fallback() {
		t.Fatalf("on %q fallback=%v, want ziti and a fallback", l.Transport(), l.Fallback())
	}
	// Every later dial goes the same way, so no connection wanders.
	before := direct.n.Load()
	c, err = l.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if direct.n.Load() != before {
		t.Fatal("a dial after the climb tried direct again")
	}
	// Climbing again starts from the top.
	direct.fail.Store(false)
	c, err = l.Climb(context.Background(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if l.Transport() != "direct" || l.Fallback() {
		t.Fatalf("a fresh climb should take direct back, on %q", l.Transport())
	}
}

func TestProbeAsksOnlyTheRungsAboveTheCurrentOne(t *testing.T) {
	direct, ziti := &fakeDial{name: "direct"}, &fakeDial{name: "ziti"}
	direct.fail.Store(true)
	l := NewLadder([]Rung{{Path: Path{Transport: "direct"}, Dial: direct}, {Path: Path{Transport: "ziti"}, Dial: ziti}})
	if c, err := l.Climb(context.Background(), time.Second); err == nil {
		c.Close()
	}
	if l.Probe(context.Background(), time.Second) {
		t.Fatal("direct is still down and the probe said better")
	}
	direct.fail.Store(false)
	if !l.Probe(context.Background(), time.Second) {
		t.Fatal("direct is back and the probe missed it")
	}
	if l.Transport() != "ziti" {
		t.Fatal("a probe moved the ladder. only a climb may")
	}
}

// Set keeps the room on the transport it is on, so learning the hub's list while
// attached does not move every later dial somewhere the control is not.
func TestSetKeepsTheCurrentTransport(t *testing.T) {
	direct, ziti := &fakeDial{name: "direct"}, &fakeDial{name: "ziti"}
	direct.fail.Store(true)
	l := NewLadder([]Rung{{Path: Path{Transport: "direct"}, Dial: direct}, {Path: Path{Transport: "ziti"}, Dial: ziti}})
	if c, err := l.Climb(context.Background(), time.Second); err == nil {
		c.Close()
	}
	ziti2 := &fakeDial{name: "ziti2"}
	l.Set([]Rung{{Path: Path{Transport: "ziti"}, Dial: ziti2}, {Path: Path{Transport: "direct"}, Dial: direct}})
	if l.Transport() != "ziti" || l.Fallback() {
		t.Fatalf("on %q fallback=%v after Set, want ziti and now first", l.Transport(), l.Fallback())
	}
	if c, err := l.Dial(context.Background()); err == nil {
		c.Close()
	}
	if ziti2.n.Load() != 1 {
		t.Fatal("a dial after Set did not use the new ziti rung")
	}
}

type countDialer struct{ closed atomic.Int32 }

func (d *countDialer) Dial(context.Context) (net.Conn, error) { return nil, errors.New("no") }
func (d *countDialer) Describe() string                       { return "count" }
func (d *countDialer) Close()                                 { d.closed.Add(1) }

func TestLadderSetClosesDroppedDialersAndKeepsTheOneItStandsOn(t *testing.T) {
	a, b, c := &countDialer{}, &countDialer{}, &countDialer{}
	pa, pb, pc := Path{Transport: "ziti", Service: "a"}, Path{Transport: "ziti", Service: "b"}, Path{Transport: "ziti", Service: "c"}
	l := NewLadder([]Rung{{pa, a}, {pb, b}, {pc, c}})
	for i := 0; i < 100; i++ {
		l.Set([]Rung{{pa, a}, {pb, b}, {pc, c}})
	}
	if a.closed.Load()+b.closed.Load()+c.closed.Load() != 0 {
		t.Fatal("a Set with the same dialers closed one")
	}
	l.Set([]Rung{{pa, a}})
	if a.closed.Load() != 0 || b.closed.Load() != 1 || c.closed.Load() != 1 {
		t.Fatalf("closed a=%d b=%d c=%d, want 0 1 1", a.closed.Load(), b.closed.Load(), c.closed.Load())
	}
	l.Set([]Rung{{pb, b}})
	if a.closed.Load() != 0 {
		t.Fatal("closed the rung the ladder stands on")
	}
}

// A room whose every rung fails says each address it tried, and asks for its addresses afresh before the next attempt.
func TestARoomThatReachesNoRungSaysEveryAddressAndRereadsThem(t *testing.T) {
	direct, ziti := &fakeDial{name: "192.168.1.68:7779"}, &fakeDial{name: "atrium-hub"}
	direct.fail.Store(true)
	ziti.fail.Store(true)
	l := NewLadder([]Rung{{Path: Path{Transport: "direct"}, Dial: direct}, {Path: Path{Transport: "ziti"}, Dial: ziti}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	asked := make(chan State, 1)
	r := &Room{Name: "sgg", Dial: l, T: Timings{DialWait: time.Second}}
	r.OnUnreachable = func() {
		asked <- r.State()
		cancel()
	}
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	var s State
	select {
	case s = <-asked:
	case <-time.After(10 * time.Second):
		t.Fatal("a room that reached no rung never asked for its addresses again")
	}
	<-done
	want := []string{"direct 192.168.1.68:7779", "ziti atrium-hub"}
	if s.Room != "sgg" || s.Up || strings.Join(s.Unreachable, "|") != strings.Join(want, "|") {
		t.Fatalf("state %+v, want room sgg down having tried %v", s, want)
	}
	if !strings.Contains(s.Error, "192.168.1.68:7779: 192.168.1.68:7779 refused") {
		t.Fatalf("the error lost why each rung failed: %q", s.Error)
	}
}
