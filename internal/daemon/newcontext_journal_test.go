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

func TestARestartEndsAClearItCutOffAndSaysWhere(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, f, _ := ncCard(t, d)

	if err := d.StartNewContext(task.ID); err != nil {
		t.Fatal(err)
	}
	until(t, "the capture prompt", func() bool { return strings.Contains(f.written(), "HANDOFF.") })
	if !strings.Contains(journalOf(t, d), task.ID) {
		t.Fatalf("the run in flight is not journalled: %q", journalOf(t, d))
	}

	// The process dies: what it held in memory is gone, what it wrote is not.
	d.nctx.stopAll()
	d.nctx.mu.Lock()
	delete(d.nctx.by, task.ID)
	d.nctx.mu.Unlock()
	if d.newContextFor(task.ID) != nil {
		t.Fatal("the chip survived the simulated restart")
	}

	d.endAbandonedNewContexts()

	v, _ := d.newContextFor(task.ID).(map[string]any)
	if v == nil || v["step"] != NewContextFailed {
		t.Fatalf("the cut-off run is not shown as failed: %v", d.newContextFor(task.ID))
	}
	reason, _ := v["reason"].(string)
	if !strings.Contains(reason, "restarted during step 1 of 3 (capture)") {
		t.Fatalf("the reason does not say where it was cut off: %q", reason)
	}
	if d.nctx.holding(task.ID) {
		t.Fatal("a failed chip is still holding the card's messages")
	}
	if journalOf(t, d) != "" {
		t.Fatalf("the journal was not emptied: %q", journalOf(t, d))
	}
	evs, err := d.st.Events(task.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range evs {
		if e.Kind == store.EventNotified && strings.Contains(string(e.Payload), "restarted during step 1") {
			found = true
		}
	}
	if !found {
		t.Fatal("the card's history does not record the abandoned run")
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
