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
	if got := kinds(Arrange(ps, nil)); got != "direct,ziti,zrok" {
		t.Fatalf("no order is the default order, got %s", got)
	}
}

// A room joined over direct that was told about a ziti identity locally climbs
// direct then ziti, with nothing heard from the hub. This is m1mini and sg3.
func TestCandidatesWithoutTheHubUseTheJoinAndTheLocalPaths(t *testing.T) {
	saved := Saved{Transport: "direct", Hub: "sg4:7779", Room: "m1mini"}
	got, skipped := Candidates(saved, RoomTransports{
		Paths: []Path{{Transport: "ziti", Identity: "/r/m1mini.json"}},
	})
	if kinds(got) != "direct,ziti" {
		t.Fatalf("got %s, want direct,ziti", kinds(got))
	}
	if got[0].Addr != "sg4:7779" || got[0].From != "join" {
		t.Fatalf("direct rung %+v, want the joined address from the join", got[0])
	}
	if got[1].Service != DefaultZitiService || got[1].From != "local" {
		t.Fatalf("ziti rung %+v, want the default service and a local identity", got[1])
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
	if kinds(got) != "ziti,direct" {
		t.Fatalf("the hub's order applies with no local one: got %s", kinds(got))
	}
	if got[0].Service != "local-svc" || got[1].Addr != "new:7779" {
		t.Fatalf("local wins over the hub and the hub over the join: %+v", got)
	}
	tr.Order = []string{"direct"}
	got, _ = Candidates(saved, tr)
	if kinds(got) != "direct,ziti" {
		t.Fatalf("a local order wins over the hub's: got %s", kinds(got))
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

func TestClimbBoundsARungThatHangsAndSaysEveryFailure(t *testing.T) {
	hang, dead := &fakeDial{name: "hangs", hang: true}, &fakeDial{name: "dead"}
	dead.fail.Store(true)
	l := NewLadder([]Rung{{Path: Path{Transport: "ziti"}, Dial: hang}, {Path: Path{Transport: "direct"}, Dial: dead}})
	start := time.Now()
	_, err := l.Climb(context.Background(), 100*time.Millisecond)
	if err == nil {
		t.Fatal("every rung failed and the climb said nothing")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("a hanging rung held the climb past its wait")
	}
	if !strings.Contains(err.Error(), "ziti") || !strings.Contains(err.Error(), "direct") {
		t.Fatalf("the error should name each rung: %v", err)
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
