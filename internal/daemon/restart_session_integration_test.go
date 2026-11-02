//go:build integration

package daemon

import (
	"github.com/dovholuknf/atrium/internal/daemon/cycle"
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
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
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
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	if _, err := d.RestartRunner("no-such-card"); err == nil {
		t.Fatal("restarting a card that does not exist succeeded")
	}
}

// A runner that ignores its exit keys is left running, and the refusal says so.
// Restart is not terminate: nothing is closed or killed behind the operator's back.
func TestRestartRunnerLeavesAStubbornRunnerUp(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
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
	d.hk.windDownKeyGap, d.hk.restartAskGrace = 50*time.Millisecond, 300*time.Millisecond

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
	_ = r.tm().Kill()
	<-r.done
}

// A RESTART TYPES ITS HARNESS'S EXIT KEYS AND NOTHING ELSE INTO THE OLD TERMINAL.
//
// The Ctrl-D, "Press Ctrl-D again to exit" and "Resume this session with: claude --resume" in the scrollback before
// every restart marker are claude answering those two keys. `/clear` is typed only by a new-context cycle or by the
// operator, never by a restart, and the resume that follows carries the card's own conversation id.
func TestARestartTypesTwoControlDsAndNeverClear(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	task, _, err := d.st.Register(store.Observed{WireName: "exit-keys", Worktree: t.TempDir(), Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	f := newFakePTY()
	t.Cleanup(func() { f.Close() })
	r := &runner{taskID: task.ID, pty: f, started: time.Now(), buf: newRing(1<<16, 80),
		watchers: map[chan []byte]struct{}{}, done: make(chan struct{})}
	d.sup.add(r)
	d.hk.windDownKeyGap = 20 * time.Millisecond

	_ = d.askExit(task.ID, 100*time.Millisecond)

	if got := f.written(); got != "\x04\x04" {
		t.Fatalf("a restart typed %q into the old terminal, want two control-d and nothing else", got)
	}
	if strings.Contains(f.written(), cycle.NewContextClearCommand) {
		t.Fatal("a restart typed /clear")
	}
}
