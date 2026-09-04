package daemon

import (
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// Auto mode with a deadline, at the moment it matters: a request arriving.
//
// The deadline is never enforced by a timer. Every test here sets a deadline
// in the past rather than waiting for one to pass, which is the same thing the
// chain does and is only possible BECAUSE nothing has to fire.

// Still on before the deadline, which is the case that would be easy to break
// while making the expiry work.
func TestAutoModeWithATimeLeftStillApproves(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	task := autoTask(t, d, "auto-until-1")

	until := time.Now().UTC().Add(time.Hour)
	if err := d.st.SetAutoApproveUntil(task.ID, true, &until); err != nil {
		t.Fatal(err)
	}

	_, auto := ask(t, d, "auto-until-1", "Bash", "go test ./...")
	if auto == nil {
		t.Fatal("auto mode with an hour left asked a human")
	}
	if auto.Decision != "approve" {
		t.Fatalf("it answered %q", auto.Decision)
	}
}

// Past the deadline, the request reaches a human. Nothing ran to make this
// true: the deadline is read when the decision is made.
func TestAutoModeAsksAgainOnceItsTimeIsUp(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	task := autoTask(t, d, "auto-until-2")

	past := time.Now().UTC().Add(-time.Minute)
	if err := d.st.SetAutoApproveUntil(task.ID, true, &past); err != nil {
		t.Fatal(err)
	}

	_, auto := ask(t, d, "auto-until-2", "Bash", "rm -rf /")
	if auto != nil {
		t.Fatalf("auto mode answered after its deadline: %+v", auto)
	}
	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.StatusNeedsPermission {
		t.Fatalf("the card is %s, wanted needs-permission", got.Status)
	}
}

// And the flag is turned off on the way through, so the badge on the board
// stops claiming something that stopped being true.
func TestAnExpiredDeadlineTidiesItselfUp(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	task := autoTask(t, d, "auto-until-3")

	past := time.Now().UTC().Add(-time.Minute)
	if err := d.st.SetAutoApproveUntil(task.ID, true, &past); err != nil {
		t.Fatal(err)
	}
	ask(t, d, "auto-until-3", "Bash", "ls")

	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.AutoApprove {
		t.Fatal("an expired card still says auto mode is on")
	}
	if got.AutoUntil != nil {
		t.Fatal("an expired card kept its deadline, so the badge has something to argue about")
	}
}

// The board-wide switch expires the same way, and expiring it does not touch a
// card that was turned loose on its own. They are different answers to give six
// hours later and they stay different.
func TestGlobalAutoExpiringLeavesAPerCardSwitchAlone(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	task := autoTask(t, d, "auto-until-4")

	past := time.Now().UTC().Add(-time.Minute)
	if err := d.st.SetGlobalAutoUntil(true, &past); err != nil {
		t.Fatal(err)
	}
	if d.st.GlobalAuto() {
		t.Fatal("an expired board-wide switch is still on")
	}

	// The card's own switch has no deadline, so it still answers.
	_, auto := ask(t, d, "auto-until-4", "Bash", "ls")
	if auto == nil {
		t.Fatal("a card turned loose on its own stopped working when the board-wide switch expired")
	}
	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.AutoApprove {
		t.Fatal("the board-wide switch expiring turned off a card's own")
	}
}

// A board-wide deadline with time left approves a session that was never set
// up, which is what board-wide means.
func TestGlobalAutoWithATimeLeftApprovesAnyone(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	if _, _, err := d.st.Register(store.Observed{
		WireName: "auto-until-5", Worktree: "/tmp/atrium-test", Runner: "claude",
	}); err != nil {
		t.Fatal(err)
	}

	until := time.Now().UTC().Add(time.Hour)
	if err := d.st.SetGlobalAutoUntil(true, &until); err != nil {
		t.Fatal(err)
	}
	_, auto := ask(t, d, "auto-until-5", "Bash", "ls")
	if auto == nil {
		t.Fatal("a board-wide switch with an hour left asked a human")
	}
}

// A never rule still blocks, deadline or not. Auto mode stops new questions
// and does not discard answers already given, and a deadline changes nothing
// about the order of the chain.
func TestADeadlineDoesNotReorderTheChain(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	task := autoTask(t, d, "auto-until-6")

	until := time.Now().UTC().Add(time.Hour)
	if err := d.st.SetAutoApproveUntil(task.ID, true, &until); err != nil {
		t.Fatal(err)
	}
	if _, err := d.st.AddRule("Bash", "rm -rf", "block", "no", ""); err != nil {
		t.Fatal(err)
	}

	_, auto := ask(t, d, "auto-until-6", "Bash", "rm -rf /")
	if auto == nil || auto.Decision != "block" {
		t.Fatalf("a never rule was overruled by auto mode with a deadline: %+v", auto)
	}
}
