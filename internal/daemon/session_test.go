package daemon

import (
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// A session that has only just opened has made no tool call, so the permission
// hook has nothing to report. SessionStart is what makes it visible, and it
// costs nothing because it is a hook rather than a model turn.
func TestSessionStartRegistersImmediately(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer func() {
		cancel()
		<-errCh
	}()

	if err := d.onSession(SessionEvent{
		Agent: "fresh-1234", Event: "start", Cwd: `D:\git\atrium`, PID: 4242, Source: "startup",
	}); err != nil {
		t.Fatal(err)
	}
	tasks, err := d.Store().List()
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("want one card, got %d", len(tasks))
	}
	got := tasks[0]
	if got.WireName != "fresh-1234" || got.PID != 4242 {
		t.Fatalf("card did not take the session's identity: %+v", got)
	}
	if got.Worktree != "D:/git/atrium" {
		t.Errorf("worktree is %q, want forward slashes", got.Worktree)
	}
	// Ready, not running. SessionStart fires before the session has done
	// anything, so it is sitting at its prompt, which is what ready means.
	// Landing in running had every terminal opened all morning claiming to be
	// working.
	if got.Status != store.StatusNeedsInput {
		t.Errorf("status is %q, want a fresh session to be ready", got.Status)
	}
}

// And it leaves ready the moment it does something, which is what makes
// starting there safe. Both halves are needed: a card that started ready and
// stayed there through a whole turn would be worse than the bug it fixed.
func TestAFreshSessionLeavesReadyWhenItWorks(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer func() {
		cancel()
		<-errCh
	}()

	if err := d.onSession(SessionEvent{
		Agent: "fresh-work", Event: "start", Cwd: `D:\git\atrium`, PID: 4242,
	}); err != nil {
		t.Fatal(err)
	}
	task, err := d.st.GetByWireName("fresh-work")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != store.StatusNeedsInput {
		t.Fatalf("a fresh session is %q, wanted ready", task.Status)
	}

	// A tool call is work, so it is running from here.
	d.turnResumed(task.ID)
	task, err = d.st.GetByWireName("fresh-work")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != store.StatusRunning {
		t.Fatalf("status is %q, want running once it did something", task.Status)
	}
}

// SessionEnd is the only reliable signal that a session is over. Without it a
// card sits in running forever.
func TestSessionEndMarksDead(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer func() {
		cancel()
		<-errCh
	}()

	ev := SessionEvent{Agent: "ends-1", Event: "start", Cwd: "d:/w", PID: 10}
	if err := d.onSession(ev); err != nil {
		t.Fatal(err)
	}
	ev.Event = "end"
	if err := d.onSession(ev); err != nil {
		t.Fatal(err)
	}
	tasks, err := d.Store().List()
	if err != nil {
		t.Fatal(err)
	}
	if tasks[0].Status != store.StatusDead {
		t.Fatalf("status is %q, want dead", tasks[0].Status)
	}
}

// Starting again revives the card rather than making a second one, which is
// what a resume looks like from atrium's side.
func TestSessionStartRevivesADeadCard(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer func() {
		cancel()
		<-errCh
	}()

	ev := SessionEvent{Agent: "revive-1", Event: "start", Cwd: "d:/w", PID: 11}
	if err := d.onSession(ev); err != nil {
		t.Fatal(err)
	}
	ev.Event = "end"
	if err := d.onSession(ev); err != nil {
		t.Fatal(err)
	}
	ev.Event = "start"
	ev.Source = "resume"
	if err := d.onSession(ev); err != nil {
		t.Fatal(err)
	}
	tasks, err := d.Store().List()
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("resume made a second card: %d", len(tasks))
	}
	// Revived, and ready rather than running: a resumed session is sitting at
	// its prompt exactly like a new one. What matters here is that it left
	// dead at all.
	if tasks[0].Status != store.StatusNeedsInput {
		t.Fatalf("status is %q, want ready after a resume", tasks[0].Status)
	}
}

// A card put down by hand stays down when its session ends.
func TestSessionEndLeavesShelvedAlone(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer func() {
		cancel()
		<-errCh
	}()

	ev := SessionEvent{Agent: "shelf-1", Event: "start", Cwd: "d:/w", PID: 12}
	if err := d.onSession(ev); err != nil {
		t.Fatal(err)
	}
	tasks, _ := d.Store().List()
	if err := d.Store().SetStatus(tasks[0].ID, store.StatusShelved); err != nil {
		t.Fatal(err)
	}
	ev.Event = "end"
	if err := d.onSession(ev); err != nil {
		t.Fatal(err)
	}
	after, err := d.Store().Get(tasks[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != store.StatusShelved {
		t.Fatalf("status is %q, want the card to stay shelved", after.Status)
	}
}

// A launched runner is told which card it belongs to, so it must bind to that
// one rather than opening a second.
func TestSessionBindsToLaunchedCard(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer func() {
		cancel()
		<-errCh
	}()

	pre, _, err := d.Store().Register(store.Observed{
		WireName: "atrium-99", Worktree: "d:/git/atrium", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.onSession(SessionEvent{
		Agent: "atrium-99", Event: "start", Cwd: "d:/git/atrium", PID: 77, TaskID: pre.ID,
	}); err != nil {
		t.Fatal(err)
	}
	tasks, err := d.Store().List()
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("launched session made an extra card: %d", len(tasks))
	}
	if tasks[0].ID != pre.ID {
		t.Fatal("session did not bind to the card that launched it")
	}
}
