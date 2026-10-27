//go:build integration

package api

import (
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// Revive is a card a failure killed: live, no runner, and nobody closed, paused or parked it.
func TestReviveMarksOnlyACardAFailureKilled(t *testing.T) {
	t.Parallel()
	srv, st, _ := serverFor(t)
	mk := func(name string) string {
		t.Helper()
		task, _, err := st.Register(store.Observed{WireName: name, Worktree: t.TempDir(), Runner: "claude"})
		if err != nil {
			t.Fatal(err)
		}
		return task.ID
	}
	killed, running, closed, paused, never := mk("killed"), mk("running"), mk("closed"), mk("paused"), mk("never")
	for _, id := range []string{killed, running, closed, paused} {
		if err := st.MarkLive(id); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SetExitAsked(closed); err != nil {
		t.Fatal(err)
	}
	if err := st.SetStatus(paused, store.StatusPaused); err != nil {
		t.Fatal(err)
	}
	srv.hk.isSupervised = func(id string) bool { return id == running }

	var tasks []*store.Task
	for _, id := range []string{killed, running, closed, paused, never} {
		task, err := st.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		tasks = append(tasks, task)
	}
	got := map[string]bool{}
	for _, v := range srv.withAskCounts(srv.toViews(tasks)) {
		got[v.Task.ID] = v.Revive
	}
	want := map[string]bool{killed: true, running: false, closed: false, paused: false, never: false}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("%s: revive %v, want %v", id, got[id], w)
		}
	}
}
