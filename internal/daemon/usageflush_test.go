package daemon

import (
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// A worker with no Stop hook is recorded by the other ways its work ends, and a
// Stop that also fires counts nothing twice.

func (f *usageFix) replies() int {
	f.t.Helper()
	rows, err := f.st.SessionUsageOf(f.task.ID, 100)
	if err != nil {
		f.t.Fatal(err)
	}
	n := 0
	for _, r := range rows {
		n += r.Replies
	}
	return n
}

// settled waits for the background record to land n replies in all.
func (f *usageFix) settled(n int) {
	f.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if f.replies() == n {
			time.Sleep(50 * time.Millisecond)
			if f.replies() == n {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	f.t.Fatalf("replies on record %d, want %d", f.replies(), n)
}

func TestFlushedRecordsWithoutAStop(t *testing.T) {
	f := newUsageFix(t)
	f.u.settle = 0
	f.line(f.base, "m1", 0, 1000, 9000, 50, false)
	f.line(f.base.Add(time.Second), "m2", 0, 100, 9000, 60, false)
	f.u.flushed(f.task)
	f.settled(2)
}

// Report, then Stop, then an exit: each reply once.
func TestReportThenStopThenExitCountEachReplyOnce(t *testing.T) {
	f := newUsageFix(t)
	f.u.settle = 0
	f.line(f.base, "m1", 0, 1000, 9000, 50, false)
	f.u.flushed(f.task) // the report
	f.settled(1)
	f.line(f.base.Add(time.Second), "m2", 0, 100, 9000, 60, false)
	f.u.stopped(f.task) // the Stop that follows, one new reply since
	f.settled(2)
	f.u.flushed(f.task) // the exit
	f.settled(2)
}

// Stop first, then the report, then the exit.
func TestStopThenReportThenExitCountEachReplyOnce(t *testing.T) {
	f := newUsageFix(t)
	f.u.settle = 0
	f.line(f.base, "m1", 0, 1000, 9000, 50, false)
	f.line(f.base.Add(time.Second), "m2", 0, 100, 9000, 60, false)
	f.u.stopped(f.task)
	f.settled(2)
	f.u.flushed(f.task)
	f.settled(2)
	f.u.flushed(f.task)
	f.settled(2)
}

// A flush leaves the turn open: the Stop that closes it still carries its cause.
func TestFlushedLeavesTheTurnsCauseForTheStop(t *testing.T) {
	f := newUsageFix(t)
	f.u.settle = 0
	f.u.prompted(f.task.ID, store.UsageSay)
	f.line(f.base, "m1", 0, 1000, 9000, 50, false)
	f.u.flushed(f.task)
	f.settled(1)
	f.line(f.base.Add(time.Second), "m2", 0, 100, 9000, 60, false)
	f.u.stopped(f.task)
	f.settled(2)
	rows, _ := f.st.SessionUsageOf(f.task.ID, 10)
	for _, r := range rows {
		if r.Cause != store.UsageSay {
			t.Fatalf("cause %q, want say on both rows", r.Cause)
		}
	}
}

// No transcript, no resume id, or a nil tracker: nothing recorded and nothing
// raised.
func TestFlushedWithoutATranscriptIsQuiet(t *testing.T) {
	f := newUsageFix(t)
	f.u.settle = 0
	f.line(f.base, "m1", 0, 1000, 9000, 50, false)
	gone := *f.task
	gone.ResumeID = "elsewhere" // the transcript lookup finds no file
	f.u.flushed(&gone)
	none := *f.task
	none.ResumeID = ""
	f.u.flushed(&none)
	var nilTracker *usageTracker
	nilTracker.flushed(f.task)
	time.Sleep(100 * time.Millisecond)
	if n := f.replies(); n != 0 {
		t.Fatalf("%d replies recorded, want none", n)
	}
}

// Subagent files are read on a flush as on a Stop.
func TestFlushedReadsSubagentFiles(t *testing.T) {
	f := newUsageFix(t)
	f.u.settle = 0
	f.line(f.base, "m1", 0, 1000, 9000, 50, false)
	f.lineTo(subagentDir(f.path)+"/agent-a1.jsonl", "claude-opus-5-5", f.base.Add(time.Second), "s1", 0, 500, 0, 40, true)
	f.u.flushed(f.task)
	f.settled(2)
	f.u.stopped(f.task)
	f.settled(2)
}

// siteDaemon is a daemon whose usage tracker reads a hand-written transcript, with
// a Claude card on it that has no Stop hook.
func siteDaemon(t *testing.T) (*Daemon, *usageFix) {
	t.Helper()
	f := newUsageFix(t)
	d := testDaemon(t)
	task, _, err := d.st.Register(store.Observed{WireName: "worker", Worktree: f.task.Worktree, Runner: "claude", PID: 999999})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetResumeID(task.ID, "s1"); err != nil {
		t.Fatal(err)
	}
	f.task, _ = d.st.Get(task.ID)
	f.st = d.st
	f.u = f.tracker()
	f.u.st = d.st
	f.u.isClaude = func(string) bool { return true }
	f.u.settle = 0
	d.usage = f.u
	f.line(f.base, "m1", 0, 1000, 9000, 50, false)
	return d, f
}

func TestAReportRecordsTheWorkersUsage(t *testing.T) {
	d, f := siteDaemon(t)
	if _, _, err := d.finish(f.task, FinishRequest{TaskID: f.task.ID, Status: ReportDone, Recap: "done", NoCommit: "nothing to commit"}); err != nil {
		t.Fatal(err)
	}
	f.settled(1)
}

func TestASessionEndRecordsTheWorkersUsage(t *testing.T) {
	d, f := siteDaemon(t)
	if err := d.onSession(SessionEvent{Event: "end", TaskID: f.task.ID, Agent: "worker", Reason: "other"}); err != nil {
		t.Fatal(err)
	}
	f.settled(1)
}

func TestARunnerExitRecordsTheWorkersUsage(t *testing.T) {
	d, f := siteDaemon(t)
	d.fileExit(runExit{taskID: f.task.ID, runID: "run-x", code: 0, lived: time.Hour})
	f.settled(1)
}

func TestATerminateRecordsTheWorkersUsage(t *testing.T) {
	d, f := siteDaemon(t)
	if err := d.Kill(f.task.ID); err != nil {
		t.Skipf("kill of a stand-in pid: %v", err)
	}
	f.settled(1)
}
