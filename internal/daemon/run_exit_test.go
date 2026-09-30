package daemon

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// An exit is filed once per START, and the tests are named for the ways that
// breaks: filed twice, filed against the wrong start, or a shell's end taken
// for a card's.

func filedExits(t *testing.T, d *Daemon, id string) []map[string]any {
	t.Helper()
	evs, err := d.st.Events(id, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	for _, e := range evs {
		if e.Kind != store.EventExited {
			continue
		}
		m := map[string]any{}
		_ = json.Unmarshal(e.Payload, &m)
		out = append(out, m)
	}
	return out
}

func deadChanges(t *testing.T, d *Daemon, id string) int {
	t.Helper()
	evs, err := d.st.Events(id, 0)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range evs {
		if e.Kind != store.EventStatusChanged {
			continue
		}
		m := map[string]any{}
		_ = json.Unmarshal(e.Payload, &m)
		if m["to"] == store.StatusDead {
			n++
		}
	}
	return n
}

func someShell(t *testing.T) (string, []string) {
	t.Helper()
	for _, c := range []struct {
		name string
		args []string
	}{
		{"pwsh", []string{"-NoProfile", "-NoLogo", "-Command"}},
		{"powershell.exe", []string{"-NoProfile", "-NoLogo", "-Command"}},
		{"sh", []string{"-c"}},
	} {
		if _, err := exec.LookPath(c.name); err == nil {
			return c.name, c.args
		}
	}
	t.Skip("no shell to run")
	return "", nil
}

func TestFilingTheSameExitTwiceRecordsOneEventAndOneStatusChange(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	task := cardAt(t, d, "twice-filed", t.TempDir())
	runID := store.NewRunID()
	if err := d.st.RecordRun(store.PtyRun{RunID: runID, TaskID: task.ID, Kind: store.RunKindRunner}); err != nil {
		t.Fatal(err)
	}
	x := runExit{taskID: task.ID, runID: runID, code: 2, lived: time.Hour}
	if !d.fileExit(x) {
		t.Fatal("the first filing did nothing")
	}
	if d.fileExit(x) {
		t.Fatal("the second filing was not a no-op")
	}
	if n := len(filedExits(t, d, task.ID)); n != 1 {
		t.Fatalf("%d exit events, want 1", n)
	}
	if n := deadChanges(t, d, task.ID); n != 1 {
		t.Fatalf("%d status changes to dead, want 1", n)
	}
}

func TestALaterRunsExitIsFiledSeparately(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	task := cardAt(t, d, "restarted", t.TempDir())
	first, second := store.NewRunID(), store.NewRunID()
	for _, id := range []string{first, second} {
		if err := d.st.RecordRun(store.PtyRun{RunID: id, TaskID: task.ID, Kind: store.RunKindRunner}); err != nil {
			t.Fatal(err)
		}
	}
	if !d.fileExit(runExit{taskID: task.ID, runID: first, code: 1, lived: time.Hour}) {
		t.Fatal("first run not filed")
	}
	if !d.fileExit(runExit{taskID: task.ID, runID: second, code: 0, lived: time.Hour}) {
		t.Fatal("a later run's exit was taken for the earlier one")
	}
	if n := len(filedExits(t, d, task.ID)); n != 2 {
		t.Fatalf("%d exit events, want 2", n)
	}
	// And the earlier one still cannot be filed again.
	if d.fileExit(runExit{taskID: task.ID, runID: first, code: 1, lived: time.Hour}) {
		t.Fatal("an earlier run filed twice after a later one")
	}
}

func TestARunnerAndAShellOnOneCardHaveTheirOwnRuns(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	shell, args := someShell(t)
	dir := t.TempDir()
	task := cardAt(t, d, "both", dir)

	sleep := "Start-Sleep -Seconds 60"
	if shell == "sh" {
		sleep = "sleep 60"
	}
	if _, err := d.spawnPTY(task.ID, shell, append(args, sleep), dir, os.Environ()); err != nil {
		t.Skipf("could not start a runner here: %v", err)
	}
	r := d.sup.get(task.ID)
	t.Cleanup(func() { r.closePTY() })
	if err := d.EnsureShell(task.ID); err != nil {
		t.Skipf("could not open a shell here: %v", err)
	}
	sh := d.sup.getShell(task.ID)
	defer d.CloseShell(task.ID)

	if r.runID == "" || sh.runID == "" || r.runID == sh.runID {
		t.Fatalf("run ids %q and %q must both be set and differ", r.runID, sh.runID)
	}
	for id, kind := range map[string]string{r.runID: store.RunKindRunner, sh.runID: store.RunKindShell} {
		got, err := d.st.Run(id)
		if err != nil || got.TaskID != task.ID || got.Kind != kind || got.Host != "" {
			t.Fatalf("run %s: %+v %v", id, got, err)
		}
	}

	// The shell's end is marked filed and writes nothing to the card.
	d.CloseShell(task.ID)
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if got, _ := d.st.Run(sh.runID); got != nil && got.Filed {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got, _ := d.st.Run(sh.runID); got == nil || !got.Filed {
		t.Fatal("the shell's run was never marked filed")
	}
	if n := len(filedExits(t, d, task.ID)); n != 0 {
		t.Fatalf("a shell closing wrote %d exit events onto the card", n)
	}
	if got, _ := d.st.Run(r.runID); got == nil || got.Filed {
		t.Fatalf("the runner's run was marked filed by the shell: %+v", got)
	}

	// The runner's own exit files against the runner's run.
	windDown(r, time.Second, nil)
	deadline = time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) && len(filedExits(t, d, task.ID)) == 0 {
		time.Sleep(50 * time.Millisecond)
	}
	if n := len(filedExits(t, d, task.ID)); n != 1 {
		t.Fatalf("%d exit events after the runner ended, want 1", n)
	}
	if got, _ := d.st.Run(r.runID); got == nil || !got.Filed {
		t.Fatalf("the runner's run was not marked filed: %+v", got)
	}
}

// A start is on disk by the time the launch has returned.
func TestAStartWritesItsRunRowBeforeTheLaunchIsComplete(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	shell, args := someShell(t)
	dir := t.TempDir()
	task := cardAt(t, d, "recorded", dir)
	sleep := "Start-Sleep -Seconds 60"
	if shell == "sh" {
		sleep = "sleep 60"
	}
	if _, err := d.spawnPTY(task.ID, shell, append(args, sleep), dir, os.Environ()); err != nil {
		t.Skipf("could not start a runner here: %v", err)
	}
	r := d.sup.get(task.ID)
	t.Cleanup(func() {
		windDown(r, time.Second, nil)
		select {
		case <-r.done:
		case <-time.After(10 * time.Second):
		}
		// The directory is released a beat after the process is reaped.
		time.Sleep(500 * time.Millisecond)
	})
	got, err := d.st.Run(r.runID)
	if err != nil {
		t.Fatalf("the launch returned with no run row: %v", err)
	}
	if got.TaskID != task.ID || got.Kind != store.RunKindRunner || got.Host != "" || got.Filed {
		t.Fatalf("row %+v", got)
	}
}

// A runner that dies inside the startup window is explained the way it always
// was: its last output on the event and on the card.
func TestAStartupFailureKeepsItsTailAndWhy(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	task := cardAt(t, d, "stillborn", t.TempDir())
	runID := store.NewRunID()
	tail := "error: no such model\nrun it again"
	if !d.fileExit(runExit{taskID: task.ID, runID: runID, code: 1, tail: tail, lived: time.Second}) {
		t.Fatal("not filed")
	}
	evs := filedExits(t, d, task.ID)
	if len(evs) != 1 || evs[0]["output"] != tail || evs[0]["exit_code"] != float64(1) || evs[0]["by"] != "supervisor" {
		t.Fatalf("event %+v", evs)
	}
	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got.Why, "failed to start: error: no such model") {
		t.Fatalf("why = %q", got.Why)
	}
	// An hour of work explains nothing, so nothing is attached.
	task2 := cardAt(t, d, "long-lived", t.TempDir())
	d.fileExit(runExit{taskID: task2.ID, runID: store.NewRunID(), code: 1, tail: tail, lived: time.Hour})
	if ev := filedExits(t, d, task2.ID); len(ev) != 1 || ev[0]["output"] != nil {
		t.Fatalf("a long run kept a tail: %+v", ev)
	}
}
