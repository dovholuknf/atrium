package daemon

import (
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// A plain card, with auto mode off, to prove the global switch is what
// answered rather than anything about the card.
func plainTask(t *testing.T, d *Daemon, name string) *store.Task {
	t.Helper()
	task, _, err := d.st.Register(store.Observed{
		WireName: name, Worktree: "/tmp/atrium-test", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

// The whole point: a session nobody turned loose individually is turned loose
// with everything else.
func TestGlobalAutoApprovesASessionThatWasNotSetUp(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	plainTask(t, d, "global-1")

	if _, auto := ask(t, d, "global-1", "Bash", "go test ./..."); auto != nil {
		t.Fatal("something answered before global auto was even on")
	}
	if err := d.st.SetGlobalAuto(true); err != nil {
		t.Fatal(err)
	}
	_, auto := ask(t, d, "global-1", "Bash", "go build ./...")
	if auto == nil {
		t.Fatal("global auto left the request for a human")
	}
	if auto.Decision != "approve" {
		t.Fatalf("global auto answered %q, wanted approve", auto.Decision)
	}
}

// The record has to say which switch answered. Six hours later, "I turned this
// session loose" and "I turned the whole board loose" are different answers.
func TestGlobalAutoIsRecordedUnderItsOwnName(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	plainTask(t, d, "global-2")
	if err := d.st.SetGlobalAuto(true); err != nil {
		t.Fatal(err)
	}

	id, _ := ask(t, d, "global-2", "Bash", "rm -rf /tmp/nothing")
	p, err := d.st.GetPermission(id)
	if err != nil {
		t.Fatal(err)
	}
	if p.DecidedBy != "global-auto" {
		t.Fatalf("recorded as decided by %q, wanted global-auto", p.DecidedBy)
	}
}

// A session with its own auto mode on keeps saying so, rather than being
// relabelled by the global switch being on at the same time.
func TestPerSessionAutoKeepsItsOwnMarker(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	autoTask(t, d, "global-3")
	if err := d.st.SetGlobalAuto(true); err != nil {
		t.Fatal(err)
	}

	id, _ := ask(t, d, "global-3", "Bash", "ls")
	p, err := d.st.GetPermission(id)
	if err != nil {
		t.Fatal(err)
	}
	if p.DecidedBy != "auto" {
		t.Fatalf("recorded as decided by %q, wanted auto", p.DecidedBy)
	}
}

// Global auto stops new questions. It does not discard answers already given,
// so a shelved card is still a standing no.
func TestGlobalAutoDoesNotOverrideShelving(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	task := plainTask(t, d, "global-4")
	if err := d.st.SetStatus(task.ID, store.StatusShelved); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetGlobalAuto(true); err != nil {
		t.Fatal(err)
	}

	_, auto := ask(t, d, "global-4", "Bash", "ls")
	if auto == nil || auto.Decision != "block" {
		t.Fatalf("a shelved card was let through by global auto: %+v", auto)
	}
}

// Off means asking again, immediately, with nothing to restart.
func TestGlobalAutoOffAsksAgain(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	plainTask(t, d, "global-5")
	if err := d.st.SetGlobalAuto(true); err != nil {
		t.Fatal(err)
	}
	if _, auto := ask(t, d, "global-5", "Bash", "ls"); auto == nil {
		t.Fatal("global auto did not answer while it was on")
	}
	if err := d.st.SetGlobalAuto(false); err != nil {
		t.Fatal(err)
	}
	if _, auto := ask(t, d, "global-5", "Bash", "ls -la"); auto != nil {
		t.Fatalf("still answering after global auto was turned off: %+v", auto)
	}
}

// It is kept in the database on purpose. A restart is not consent to start
// asking again, and it is not consent to keep approving either: it is whatever
// it was when you left.
func TestGlobalAutoSurvivesAReopen(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	path := d.opts.DBPath
	if err := d.st.SetGlobalAuto(true); err != nil {
		t.Fatal(err)
	}
	cancel()
	d.Close()

	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if !st.GlobalAuto() {
		t.Fatal("global auto was forgotten across a restart")
	}
}
