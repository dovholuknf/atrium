package daemon

import (
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// Stage 1 of docs/runtime/a2a-reliability-design.md: every failure mode it covers has
// a test here. F1 silent stop, F5 an ambiguous report, F6 no loop, F7 a stuck
// tool, F8 lineage, F13 notices not rate limited, F14 an unverified sha, F15 a
// message nothing will deliver.

func prompt(t *testing.T, d *Daemon, id string) {
	t.Helper()
	if err := d.st.SetStatus(id, store.StatusRunning); err != nil {
		t.Fatal(err)
	}
	// As the launch records it: the session that asked is the sender.
	ev := map[string]any{"text": "do the thing", "via": "launch"}
	if c, err := d.st.Get(id); err == nil && c.Launched() {
		ev["from_peer"] = c.SpawnedBy
	}
	if err := d.st.AppendEvent(id, store.EventPrompted, ev); err != nil {
		t.Fatal(err)
	}
}
