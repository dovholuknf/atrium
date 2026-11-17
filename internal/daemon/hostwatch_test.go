package daemon

import (
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/roomstats"
)

func hostConnsAll(state int) func([]int) map[int]int {
	return func(pids []int) map[int]int {
		out := map[int]int{}
		for _, p := range pids {
			out[p] = state
		}
		return out
	}
}

func TestHostHogFlagsAfterTheWindowAndClears(t *testing.T) {
	var w hostWatch
	t0 := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	started := t0.Add(-time.Hour)
	cpu := time.Duration(0)
	step := 30 * time.Second
	read := func(at time.Time, busy bool) []HostFlag {
		if busy {
			cpu += step
		}
		f, _ := w.hostObserve([]roomstats.HostProc{{PID: 7, Name: "spin.exe", CPU: cpu, Started: started}},
			hostConnsAll(roomstats.ConnNone), at, 90, 5*time.Minute)
		return f
	}
	at := t0
	if f := read(at, false); len(f) != 0 {
		t.Fatalf("first reading has no rate, flagged %+v", f)
	}
	for i := 1; i <= 9; i++ {
		at = at.Add(step)
		if f := read(at, true); len(f) != 0 {
			t.Fatalf("flagged after %s, before the 5 minute window: %+v", at.Sub(t0), f)
		}
	}
	at = at.Add(step)
	f := read(at, true)
	if len(f) != 1 || f[0].Shape != HostShapeHog || f[0].PID != 7 || f[0].CPUPct != 100 || !f[0].Since.Equal(t0) {
		t.Fatalf("want one hog since %s, got %+v", t0, f)
	}
	at = at.Add(step)
	if f := read(at, false); len(f) != 0 {
		t.Fatalf("an idle reading should clear the hog, got %+v", f)
	}
}

func TestHostHogLifetimeAverageCountsFromStart(t *testing.T) {
	var w hostWatch
	t0 := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	started := t0.Add(-2 * time.Hour)
	p := roomstats.HostProc{PID: 6688, Name: "sshd.exe", CPU: 2 * time.Hour, Started: started}
	w.hostObserve([]roomstats.HostProc{p}, hostConnsAll(roomstats.ConnSome), t0, 90, 5*time.Minute)
	p.CPU += 30 * time.Second
	f, _ := w.hostObserve([]roomstats.HostProc{p}, hostConnsAll(roomstats.ConnSome), t0.Add(30*time.Second), 90,
		5*time.Minute)
	if len(f) != 1 || !f[0].Since.Equal(started) {
		t.Fatalf("a process busy since it started is flagged at once, since its start: %+v", f)
	}
}

func TestHostPIDReuseStartsOver(t *testing.T) {
	var w hostWatch
	t0 := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	a := roomstats.HostProc{PID: 9, Name: "a", CPU: time.Hour, Started: t0.Add(-time.Hour)}
	w.hostObserve([]roomstats.HostProc{a}, hostConnsAll(roomstats.ConnNone), t0, 90, 5*time.Minute)
	b := roomstats.HostProc{PID: 9, Name: "b", CPU: time.Second, Started: t0}
	f, _ := w.hostObserve([]roomstats.HostProc{b}, hostConnsAll(roomstats.ConnNone), t0.Add(30*time.Second), 90,
		5*time.Minute)
	if len(f) != 0 {
		t.Fatalf("a new process on a reused pid has no rate yet: %+v", f)
	}
}

func sshdTree(t0 time.Time) []roomstats.HostProc {
	return []roomstats.HostProc{
		{PID: 1684, Name: "services.exe", Started: t0},
		{PID: 5008, PPID: 1684, Name: "sshd.exe", Started: t0},
		{PID: 6688, PPID: 5008, Name: "sshd.exe", Started: t0},
		{PID: 6690, PPID: 6688, Name: "sshd.exe", Started: t0},
		{PID: 14840, PPID: 5008, Name: "sshd-session.exe", Started: t0},
	}
}

func TestHostSSHDSessionWithNoTCPIsFlaggedOnTheSecondReading(t *testing.T) {
	var w hostWatch
	t0 := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	conns := func(pids []int) map[int]int {
		out := map[int]int{}
		for _, p := range pids {
			out[p] = roomstats.ConnNone
		}
		out[6690] = roomstats.ConnSome
		return out
	}
	if f, _ := w.hostObserve(sshdTree(t0), conns, t0, 90, 5*time.Minute); len(f) != 0 {
		t.Fatalf("one reading is not enough: %+v", f)
	}
	f, lim := w.hostObserve(sshdTree(t0), conns, t0.Add(30*time.Second), 90, 5*time.Minute)
	if len(f) != 1 || f[0].PID != 14840 || f[0].Shape != HostShapeSSHDBare || !f[0].Since.Equal(t0) || lim != nil {
		t.Fatalf("want 14840 flagged, its tree has no connection, and 6688 left, its child holds one: %+v %v", f, lim)
	}
}

func TestHostSSHDListenerHoldingAConnectionFlagsNothing(t *testing.T) {
	var w hostWatch
	t0 := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	conns := func(pids []int) map[int]int {
		out := map[int]int{}
		for _, p := range pids {
			out[p] = roomstats.ConnNone
		}
		out[5008] = roomstats.ConnSome
		return out
	}
	for i := 0; i < 3; i++ {
		if f, _ := w.hostObserve(sshdTree(t0), conns, t0.Add(time.Duration(i)*30*time.Second), 90, 5*time.Minute); len(f) != 0 {
			t.Fatalf("the listener may hold the sessions' sockets, so nothing is bare: %+v", f)
		}
	}
}

func TestHostSSHDConnectionsUnreadableIsALimit(t *testing.T) {
	var w hostWatch
	t0 := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	f, lim := w.hostObserve(sshdTree(t0), hostConnsAll(roomstats.ConnUnknown), t0, 90, 5*time.Minute)
	if len(f) != 0 || len(lim) != 1 || !strings.Contains(lim[0], "6688, 14840") {
		t.Fatalf("want the blind sessions named as a limit, got %+v %v", f, lim)
	}
}

func TestHostStopAndLimits(t *testing.T) {
	f := HostFlag{PID: 6688, Shape: HostShapeSSHDBare, IOPerSec: -1}
	if got := hostStop("windows", f, false); got != "taskkill /PID 6688 /T /F  (from an administrator prompt)" {
		t.Fatalf("windows stop: %q", got)
	}
	f.Shape = HostShapeHog
	if got := hostStop("windows", f, true); got != "taskkill /PID 6688 /F" {
		t.Fatalf("windows stop, mine: %q", got)
	}
	if got := hostStop("linux", f, false); got != "sudo kill 6688" {
		t.Fatalf("linux stop: %q", got)
	}
	lim := hostLimits(f)
	if len(lim) != 3 {
		t.Fatalf("owner, start time and IO are each a stated limit: %v", lim)
	}
	if !sameUser(`SGG\localai`, "localai") || sameUser(`NT AUTHORITY\SYSTEM`, "localai") {
		t.Fatal("sameUser compares the account part")
	}
}
