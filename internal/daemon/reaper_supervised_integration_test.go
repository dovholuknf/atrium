//go:build integration

package daemon

import (
	"os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// r-011: a card atrium supervises is alive while the supervisor holds its
// runner, whatever its pid says and however long it has been silent. m1mini
// filed one dead for "15m silent and no pid", and the next tick revived it.
func TestASupervisedCardIsNeverAssumedGoneForSilence(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task := quietTask(t, d, "supervised-silent", 0)
	if err := d.st.BackdateActivity(task.ID, QuietAfter+time.Hour); err != nil {
		t.Fatal(err)
	}
	d.sup.add(&runner{taskID: task.ID, done: make(chan struct{})})
	defer d.sup.remove(task.ID)

	if err := d.reapOnce(); err != nil {
		t.Fatalf("reap: %v", err)
	}
	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.StatusRunning {
		t.Fatalf("a supervised card silent for %s was moved to %s", QuietAfter+time.Hour, got.Status)
	}
	if evs, _ := d.st.Events(task.ID, 0); countKind(evs, store.EventExited) != 0 {
		t.Fatal("the reaper wrote an exit for a card whose runner atrium still holds")
	}
}

// A stored pid can be a hook's report of some other process. While the runner
// is supervised, a gone pid is not the card's death either.
func TestASupervisedCardIgnoresAGonePid(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task := quietTask(t, d, "supervised-gonepid", deadPid(t))
	d.sup.add(&runner{taskID: task.ID, done: make(chan struct{})})
	defer d.sup.remove(task.ID)

	if err := d.reapOnce(); err != nil {
		t.Fatalf("reap: %v", err)
	}
	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.StatusRunning {
		t.Fatalf("a supervised card with a gone pid was moved to %s", got.Status)
	}
}

// Once the runner has left the supervisor, the old checks apply again.
func TestAnUnsupervisedCardStillFallsBackToSilence(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task := quietTask(t, d, "unsupervised-silent", 0)
	if err := d.st.BackdateActivity(task.ID, QuietAfter+time.Hour); err != nil {
		t.Fatal(err)
	}
	d.sup.add(&runner{taskID: task.ID, done: make(chan struct{})})
	d.sup.remove(task.ID)

	if err := d.reapOnce(); err != nil {
		t.Fatalf("reap: %v", err)
	}
	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.StatusDead {
		t.Fatalf("a card with no runner, no pid and %s of silence is still %s", QuietAfter+time.Hour, got.Status)
	}
}

// The second cause: a report that carries no pid must not erase the one the
// session hook recorded.
func TestAMessageHookKeepsThePidOnFile(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task := quietTask(t, d, "keeps-pid", 4242)
	// What the message and Stop path registers: a name and no pid.
	if _, _, err := d.st.Register(observedFor("keeps-pid")); err != nil {
		t.Fatal(err)
	}
	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.PID != 4242 {
		t.Fatalf("a report with no pid changed the stored pid to %d", got.PID)
	}
}

// deadPid is the pid of a process that has already exited.
func deadPid(t *testing.T) int {
	t.Helper()
	name, args := "true", []string{}
	if runtime.GOOS == "windows" {
		name, args = "cmd.exe", []string{"/c", "exit"}
	}
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		t.Skipf("could not spawn a throwaway process: %v", err)
	}
	pid := cmd.Process.Pid
	_ = cmd.Wait()
	return pid
}

func countKind(evs []*store.Event, kind string) int {
	n := 0
	for _, e := range evs {
		if e.Kind == kind {
			n++
		}
	}
	return n
}
