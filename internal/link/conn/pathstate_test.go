package conn

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/link/wire"
)

func TestProbePathsSaysOnUpDownAndNone(t *testing.T) {
	direct, ziti := &fakeDial{name: "direct"}, &fakeDial{name: "ziti"}
	ziti.fail.Store(true)
	l := NewLadder([]Rung{
		{Path: Path{Transport: "direct", Addr: "h:1", From: "join"}, Dial: direct},
		{Path: Path{Transport: "ziti", Service: "atrium-hub"}, Dial: ziti},
	})
	got := l.ProbePaths(context.Background(), time.Second)
	if len(got) != 3 {
		t.Fatalf("want a state per rung and one for the missing kind, got %+v", got)
	}
	want := []struct{ kind, state string }{{"direct", PathOn}, {"ziti", PathDown}, {"zrok", PathNone}}
	for i, w := range want {
		if got[i].Kind != w.kind || got[i].State != w.state {
			t.Fatalf("path %d is %s %s, want %s %s", i, got[i].Kind, got[i].State, w.kind, w.state)
		}
	}
	if got[0].RTTMs < 0 || got[0].Stale {
		t.Fatalf("the on rung measures a connect time and is not stale: %+v", got[0])
	}
	if got[0].Addr != "h:1" || got[0].Source != "join" || got[1].Addr != "atrium-hub" {
		t.Fatalf("addr and source should ride along: %+v", got)
	}
	if !strings.Contains(got[1].LastError, "refused") {
		t.Fatalf("a down rung says why: %q", got[1].LastError)
	}
	ziti.fail.Store(false)
	if s := l.ProbePaths(context.Background(), time.Second)[1]; s.State != PathUp {
		t.Fatalf("a rung that answers is up, got %s", s.State)
	}
}

func TestZrokPathShowsItsShareMaskedNeverWhole(t *testing.T) {
	got := pathAddr(Path{Transport: "zrok", Share: "abcd1234wxyz"})
	if got != "share abcd..." {
		t.Fatalf("a zrok rung's addr is %q, want the first four of its share", got)
	}
	if got := pathAddr(Path{Transport: "zrok"}); got != "" {
		t.Fatalf("a zrok rung with no share says %q", got)
	}
}

func TestStampSinceMovesOnlyWhenTheStateDoes(t *testing.T) {
	t0 := time.Unix(100, 0)
	t1 := t0.Add(time.Minute)
	prev := []PathState{{Kind: "direct", Addr: "a", State: PathOn, Since: t0}, {Kind: "ziti", State: PathDown, Since: t0}}
	cur := []PathState{{Kind: "direct", Addr: "a", State: PathOn}, {Kind: "ziti", State: PathUp}, {Kind: "zrok", State: PathNone}}
	got := stampSince(prev, cur, t1)
	if !got[0].Since.Equal(t0) || !got[1].Since.Equal(t1) || !got[2].Since.Equal(t1) {
		t.Fatalf("since: %v %v %v", got[0].Since, got[1].Since, got[2].Since)
	}
}

// The beat shape. An older hub ignores the field, and an older room never sends it.
func TestBeatCarriesPathStatesAndAnOlderBeatStillReads(t *testing.T) {
	n := note{Beat: 5, PathStates: []PathState{{Kind: "direct", State: PathOn, RTTMs: 3}}}
	b, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"path_states":[{"kind":"direct","state":"on"`) {
		t.Fatalf("beat shape: %s", b)
	}
	var back note
	if err := json.Unmarshal(b, &back); err != nil || len(back.PathStates) != 1 || back.PathStates[0].RTTMs != 3 {
		t.Fatalf("round trip: %v %+v", err, back)
	}
	var old note
	if err := json.Unmarshal([]byte(`{"beat":9,"idle_cpu":50}`), &old); err != nil || old.PathStates != nil {
		t.Fatalf("a beat with no paths: %v %+v", err, old)
	}
	if b, _ := json.Marshal(note{Beat: 1}); strings.Contains(string(b), "path_states") {
		t.Fatalf("a room with nothing to say sends nothing: %s", b)
	}
}

func TestCleanPathsBoundsAndDropsWhatItDoesNotKnow(t *testing.T) {
	in := []PathState{{Kind: "direct", State: PathOn}, {Kind: "carrier-pigeon", State: PathUp},
		{Kind: "ziti", State: "unlistened"}, {Kind: "zrok", State: PathDown, LastError: strings.Repeat("x", 1000)}}
	got := cleanPaths(in)
	if len(got) != 2 || got[1].Kind != "zrok" || len(got[1].LastError) != 300 {
		t.Fatalf("got %+v", got)
	}
	many := make([]PathState, 40)
	for i := range many {
		many[i] = PathState{Kind: "direct", State: PathUp}
	}
	if len(cleanPaths(many)) != maxPathStates {
		t.Fatal("a beat can make the hub keep any number of paths")
	}
}

func TestListenersAreInOrderAndSinceMovesOnlyWhenUpFlips(t *testing.T) {
	var h Hub
	h.SetListener("ziti", false, errors.New("controller down"))
	h.SetListener("direct", true, nil)
	ls := h.Listeners()
	if len(ls) != 2 || ls[0].Kind != "direct" || !ls[0].Up || ls[1].Up || ls[1].LastError != "controller down" {
		t.Fatalf("got %+v", ls)
	}
	since := ls[1].Since
	h.SetListener("ziti", false, errors.New("still down"))
	if got := h.Listeners()[1]; !got.Since.Equal(since) || got.LastError != "still down" {
		t.Fatalf("since moved without a flip: %+v", got)
	}
	h.SetListener("ziti", true, nil)
	if got := h.Listeners()[1]; !got.Up || got.LastError != "" {
		t.Fatalf("coming up clears the error: %+v", got)
	}
}

func TestHealthListenersShape(t *testing.T) {
	var h Hub
	h.SetListener("direct", true, nil)
	b, err := json.Marshal(map[string]any{"ok": true, "listeners": h.Listeners()})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"listeners":[{"kind":"direct","up":true,"since":`) {
		t.Fatalf("health shape: %s", b)
	}
}

func TestMergePathsTellsUnlistenedFromNone(t *testing.T) {
	paths := []PathState{{Kind: "direct", State: PathOn}, {Kind: "ziti", State: PathDown, LastError: "no answer"},
		{Kind: "zrok", State: PathNone}}
	ls := []ListenerState{{Kind: "direct", Up: true}, {Kind: "ziti", LastError: "controller down"}}
	got := MergePaths(paths, ls)
	if got[0].State != PathOn || got[1].State != PathUnlistened || got[1].LastError != "controller down" {
		t.Fatalf("got %+v", got)
	}
	if got[2].State != PathUnlistened || !strings.Contains(got[2].LastError, "zrok") {
		t.Fatalf("a kind the hub has no side for is unlistened: %+v", got[2])
	}
	if paths[1].State != PathDown {
		t.Fatal("the room's own record was changed")
	}
	if got := MergePaths(paths, nil); got[1].State != PathDown {
		t.Fatal("a hub that records no listeners says nothing it does not know")
	}
}

func TestProbeOfTheOnRungKeepsItOnWhenTheExtraDialFails(t *testing.T) {
	direct := &fakeDial{name: "direct"}
	l := NewLadder([]Rung{{Path: Path{Transport: "direct", Addr: "h:1"}, Dial: direct}})
	direct.fail.Store(true)
	got := l.ProbePaths(context.Background(), time.Second)[0]
	if got.State != PathOn || got.RTTMs != 0 {
		t.Fatalf("a failed extra dial must leave the rung on with no rtt: %+v", got)
	}
}

func TestProbeNeverHasTwoDialsInFlightForOneRung(t *testing.T) {
	direct, ziti := &fakeDial{name: "direct", hang: true}, &fakeDial{name: "ziti", hang: true}
	l := NewLadder([]Rung{
		{Path: Path{Transport: "direct", Addr: "h:1"}, Dial: direct},
		{Path: Path{Transport: "ziti", Service: "s"}, Dial: ziti},
	})
	var last []PathState
	for i := 0; i < 10; i++ {
		last = l.ProbePaths(context.Background(), time.Millisecond)
	}
	if direct.n.Load() > 1 || ziti.n.Load() > 1 {
		t.Fatalf("dials in flight: direct %d ziti %d, want at most 1 each", direct.n.Load(), ziti.n.Load())
	}
	if !last[0].Stale || last[0].State != PathOn || !last[1].Stale || last[1].State != PathDown {
		t.Fatalf("a rung still probing is stale, the on one stays on: %+v", last)
	}
}

type mutualDial struct {
	fakeDial
	mtls bool
}

func (m *mutualDial) Mutual() bool { return m.mtls }

func TestMutualIsReportedOnlyWhenTheRungSaysSo(t *testing.T) {
	a, b := &mutualDial{mtls: true}, &mutualDial{}
	l := NewLadder([]Rung{{Path: Path{Transport: "direct", Addr: "a:1"}, Dial: a}, {Path: Path{Transport: "direct", Addr: "b:1"}, Dial: b}})
	got := l.ProbePaths(context.Background(), time.Second)
	if !got[0].Mutual || got[1].Mutual {
		t.Fatalf("mtls flags: %+v", got)
	}
}

func TestOnlyViaSkipsARoomWithAnotherPathUp(t *testing.T) {
	h := NewHub(Timings{})
	h.rooms[wire.KeyOf("m1mini")] = &attached{name: "m1mini", via: "ziti",
		paths: []PathState{{Kind: "direct", State: PathDown}, {Kind: "ziti", State: PathOn}}}
	h.rooms[wire.KeyOf("sg3")] = &attached{name: "sg3", via: "ziti",
		paths: []PathState{{Kind: "direct", State: PathUp}, {Kind: "ziti", State: PathOn}}}
	got := h.OnlyVia("ziti")
	if len(got) != 1 || got[0] != "m1mini" {
		t.Fatalf("only on ziti: %v", got)
	}
}
