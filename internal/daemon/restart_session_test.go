package daemon

import (
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// Restarting a card atrium owns no terminal for has to say so, not fail deeper
// with a message about a directory or a resume id. A window-mode launch owns
// itself and a joined session belongs to whoever started it, so there is no
// terminal here to exit and nothing to relaunch.
func TestRestartRunnerRefusesACardItDoesNotOwn(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task, _, err := d.st.Register(store.Observed{
		WireName: "unowned", Worktree: "/tmp/atrium-test", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = d.RestartRunner(task.ID)
	if err == nil {
		t.Fatal("restarting a card with no runner succeeded")
	}
	if !strings.Contains(err.Error(), "nothing") {
		t.Fatalf("the refusal does not explain there is nothing to restart: %v", err)
	}
}

// A card that does not exist names the card rather than failing somewhere deeper.
func TestRestartRunnerOnAMissingCardSaysSo(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	if _, err := d.RestartRunner("no-such-card"); err == nil {
		t.Fatal("restarting a card that does not exist succeeded")
	}
}

// A runner that ignores its exit keys is left running, and the refusal says so.
// Restart is not terminate: nothing is closed or killed behind the operator's back.
func TestRestartRunnerLeavesAStubbornRunnerUp(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer func() {
		cancel()
		<-errCh
	}()

	taskID := launchSlow(t, d)
	// Control-a means nothing to a shell that is asked to leave.
	if _, err := d.st.SaveHarness(store.Harness{
		ID: "slowtest", Label: "slow test", Enabled: true, Cmd: slowCmd(),
		LaunchMode: store.LaunchPTY, ExitKeys: []string{"ctrl-a"},
	}); err != nil {
		t.Fatal(err)
	}
	oldGap, oldGrace := windDownKeyGap, restartAskGrace
	windDownKeyGap, restartAskGrace = 50*time.Millisecond, 300*time.Millisecond
	defer func() { windDownKeyGap, restartAskGrace = oldGap, oldGrace }()

	r := d.sup.get(taskID)
	_, err := d.RestartRunner(taskID)
	if err == nil || !strings.Contains(err.Error(), "did not exit when asked") {
		t.Fatalf("a runner that ignored its exit keys should be refused with a reason, got %v", err)
	}
	if d.sup.get(taskID) != r {
		t.Fatal("the stubborn runner was replaced or removed, so something closed or killed it")
	}
	select {
	case <-r.done:
		t.Fatal("the stubborn runner was killed")
	default:
	}
	if r.leaving.Load() {
		t.Fatal("the runner is still marked as leaving after it refused to")
	}
	_ = d.StopRunner(taskID)
}
