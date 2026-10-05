package daemon

import (
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// A restart must end a clear it cut off, with the reason on the card.

func journalOf(t *testing.T, d *Daemon) string {
	t.Helper()
	v, err := d.st.Setting(settingNewContextJournal)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// simulateRestart drops what the process held in memory and runs the startup pass.
func simulateRestart(t *testing.T, d *Daemon, id string) {
	t.Helper()
	d.nctx.stopAll()
	d.nctx.mu.Lock()
	delete(d.nctx.by, id)
	d.nctx.mu.Unlock()
	if d.newContextFor(id) != nil {
		t.Fatal("the chip survived the simulated restart")
	}
	d.endAbandonedNewContexts()
}

func historyHas(t *testing.T, d *Daemon, id, what string) bool {
	t.Helper()
	evs, err := d.st.Events(id, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range evs {
		if e.Kind == store.EventNotified && strings.Contains(string(e.Payload), what) {
			return true
		}
	}
	return false
}

func TestARestartEndsAClearItCutOffAndSaysWhere(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, f, _ := ncCard(t, d)
	ncToClear(t, d, task, f)
	if !strings.Contains(journalOf(t, d), task.ID) {
		t.Fatalf("the run in flight is not journalled: %q", journalOf(t, d))
	}
	simulateRestart(t, d, task.ID)

	v, _ := d.newContextFor(task.ID).(map[string]any)
	if v == nil || v["step"] != NewContextFailed {
		t.Fatalf("the cut-off run is not shown as failed: %v", d.newContextFor(task.ID))
	}
	reason, _ := v["reason"].(string)
	if !strings.Contains(reason, "restarted during step 2 of 3 (clear)") {
		t.Fatalf("the reason does not say where it was cut off: %q", reason)
	}
	if d.nctx.holding(task.ID) {
		t.Fatal("a failed chip is still holding the card's messages")
	}
	if journalOf(t, d) != "" {
		t.Fatalf("the journal was not emptied: %q", journalOf(t, d))
	}
	if !historyHas(t, d, task.ID, "restarted during step 2") {
		t.Fatal("the card's history does not record the abandoned run")
	}
	// The ack came before the restart, so a rerun resumes at the clear, and asks for nothing.
	if err := d.StartNewContext(task.ID); err != nil {
		t.Fatal(err)
	}
	if v, _ := d.newContextFor(task.ID).(map[string]any); v == nil || v["step"] != NewContextClear {
		t.Fatalf("the rerun did not resume at the clear: %v", d.newContextFor(task.ID))
	}
}

// Cut off before the ack: nothing was cleared, so no chip, only a line in the history.
func TestARestartOnTheLimitStepLeavesNoChip(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, f, _ := ncCard(t, d)
	if err := d.StartNewContext(task.ID); err != nil {
		t.Fatal(err)
	}
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), limitPrompt) })
	simulateRestart(t, d, task.ID)
	if d.newContextFor(task.ID) != nil {
		t.Fatalf("a run cut off before the ack left a chip: %v", d.newContextFor(task.ID))
	}
	if !historyHas(t, d, task.ID, "before atrium ready came") {
		t.Fatal("the card's history does not say the run was dropped")
	}
	if journalOf(t, d) != "" {
		t.Fatalf("the journal was not emptied: %q", journalOf(t, d))
	}
}

func TestTheJournalEmptiesWhenARunIsDismissed(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, _, _ := ncCard(t, d)
	if err := d.StartNewContext(task.ID); err != nil {
		t.Fatal(err)
	}
	if journalOf(t, d) == "" {
		t.Fatal("not journalled")
	}
	d.nctx.clear(task.ID)
	if journalOf(t, d) != "" {
		t.Fatalf("a dismissed run is still journalled: %q", journalOf(t, d))
	}
}

func TestAFailedRunIsNotJournalled(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, _, _ := ncCard(t, d)
	if err := d.StartNewContext(task.ID); err != nil {
		t.Fatal(err)
	}
	d.nctx.mu.Lock()
	gen := d.nctx.by[task.ID].gen
	d.nctx.mu.Unlock()
	d.nctx.fail(task.ID, gen, "boom")
	if journalOf(t, d) != "" {
		t.Fatalf("a failed run is still journalled: %q", journalOf(t, d))
	}
}
