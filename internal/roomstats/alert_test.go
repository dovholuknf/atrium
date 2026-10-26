package roomstats

import (
	"testing"
	"time"
)

type stepClock struct{ now time.Time }

func (c *stepClock) Now() time.Time { return c.now }
func (c *stepClock) Ticker(time.Duration) (<-chan time.Time, func()) {
	return make(chan time.Time), func() {}
}

// hotRig is a sampler over a machine at busy percent and a settable agent count.
type hotRig struct {
	clk    *stepClock
	s      *Sampler
	busy   uint64
	agents int
	reads  int
	n      uint64
}

func newHotRig(t *testing.T) *hotRig {
	r := &hotRig{clk: &stepClock{now: time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)}, busy: 90}
	r.s = New(Sources{
		Room: "m1mini", Clock: r.clk,
		Machine: func() (MachineReading, error) {
			r.n++
			return MachineReading{HasCPU: true, CPUIdle: r.n * (100 - r.busy), CPUTotal: r.n * 100, MemTotal: 1}, nil
		},
		Agents: func() int { return r.agents },
		Procs: func() ([]Proc, error) {
			r.reads++
			return []Proc{{PID: 42, Kind: ProcCardClosed, Card: "c1", CPUPct: 99}}, nil
		},
	})
	return r
}

func (r *hotRig) tickFor(d time.Duration) {
	for end := r.clk.now.Add(d); !r.clk.now.After(end); r.clk.now = r.clk.now.Add(Interval) {
		r.s.Tick()
	}
}

// HOT WITH NOBODY THERE for ten minutes raises the alert with the process list, read once a minute and no more.
func TestHotAlertAfterTenMinutesWithNoAgents(t *testing.T) {
	r := newHotRig(t)
	r.tickFor(HotFor - 2*Interval)
	if r.s.snap.Alert != nil || r.reads != 0 {
		t.Fatalf("alert before the window: %+v, %d reads", r.s.snap.Alert, r.reads)
	}
	r.tickFor(3 * Interval)
	a := r.s.snap.Alert
	if a == nil || a.HotSince == "" || a.CPUPct != 90 || len(a.Procs) != 1 || a.Procs[0].PID != 42 {
		t.Fatalf("alert = %+v", a)
	}
	r.tickFor(30 * time.Second)
	if r.reads != 1 {
		t.Fatalf("procs read %d times in under a minute", r.reads)
	}
}

// AN AGENT MID-TURN is a reason to be hot, and a cool tick clears the alert and starts the window again.
func TestHotAlertNeedsNoAgentsAndRearms(t *testing.T) {
	r := newHotRig(t)
	r.agents = 1
	r.tickFor(HotFor + time.Minute)
	if r.s.snap.Alert != nil {
		t.Fatalf("alert with an agent running: %+v", r.s.snap.Alert)
	}
	r.agents = 0
	r.tickFor(HotFor + Interval)
	if r.s.snap.Alert == nil {
		t.Fatal("no alert after the window with no agents")
	}
	r.busy = 10
	r.tickFor(Interval)
	if r.s.snap.Alert != nil {
		t.Fatalf("alert stayed after the room cooled: %+v", r.s.snap.Alert)
	}
	r.busy = 90
	r.tickFor(HotFor - 2*Interval)
	if r.s.snap.Alert != nil {
		t.Fatal("re-armed alert fired before a fresh window")
	}
}

// AN ENABLED RUNNER THAT DOES NOT RESOLVE is an alert at once. A disabled one is not.
func TestRunnerMissingAlert(t *testing.T) {
	clk := &stepClock{now: time.Now()}
	runners := []Runner{{Kind: "claude", Resolves: true, Enabled: true}, {Kind: "codex", Enabled: true}, {Kind: "pi"}}
	s := New(Sources{Clock: clk, Runners: func() ([]Runner, error) { return runners, nil }})
	s.Tick()
	if a := s.snap.Alert; a == nil || len(a.RunnersMissing) != 1 || a.RunnersMissing[0] != "codex" || a.HotSince != "" {
		t.Fatalf("alert = %+v", a)
	}
	runners[1].Resolves = true
	s.Tick()
	if s.snap.Alert != nil {
		t.Fatalf("alert stayed after the runner resolved: %+v", s.snap.Alert)
	}
}

func TestParsePSAndTop(t *testing.T) {
	ps := parsePS("  1     0 10-01:02:03   0.0 /sbin/launchd\n 812   1    05:01  97.5 /usr/bin/node /x/work/server.js --watch\nbad line\n 90  1 00:10 12.0 git status\n")
	if len(ps) != 3 || ps[1].PID != 812 || ps[1].PPID != 1 || ps[1].Etime != "05:01" || ps[1].CPUPct != 97.5 ||
		ps[1].Command != "/usr/bin/node /x/work/server.js --watch" {
		t.Fatalf("parsed %+v", ps)
	}
	top := TopByCPU(ps, 2)
	if len(top) != 2 || top[0].PID != 812 || top[1].PID != 90 {
		t.Fatalf("top %+v", top)
	}
	long := TopByCPU([]Proc{{Command: string(make([]rune, 300))}}, 1)
	if len([]rune(long[0].Command)) != cmdMax {
		t.Fatalf("command not cut to %d", cmdMax)
	}
}

func TestEtime(t *testing.T) {
	for d, want := range map[time.Duration]string{
		65 * time.Second: "01:05", 3*time.Hour + 2*time.Second: "03:00:02", 50*time.Hour + time.Minute: "2-02:01:00",
	} {
		if got := etime(d); got != want {
			t.Errorf("etime(%v) = %q, want %q", d, got, want)
		}
	}
}
