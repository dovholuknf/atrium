package daemon

import (
	"os"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// A CARD THAT SAYS `dead` WHILE ITS RUNNER IS ALIVE IS THE BUG THESE ARE ABOUT.
//
// The way it got broken: a daemon restart kills every supervised runner and
// files its card dead, the cards are started again onto with `task_id`, the
// pid on the card is updated and the status is not. The board draws a dead
// card, the sweep archives it off the board, and the process behind it goes on
// raising permission requests against a card nobody can see. Some of those
// recovered only because a SessionStart hook happened to fire, which is why
// these tests assert the rule rather than the hook.

func ontoCard(status string) *store.Task {
	return &store.Task{ID: "card-1", Title: "the work", Status: status}
}

func TestALaunchOntoADeadCardBringsItBack(t *testing.T) {
	got, move := statusAfterLaunchOnto(store.StatusDead)
	if !move {
		t.Fatal("a dead card was left dead with a process on it")
	}
	if got != store.StatusNeedsInput {
		t.Fatalf("a revived card landed in %q, want %q", got, store.StatusNeedsInput)
	}
}

// `done` is never swept and `turnResumed` only revives a card from a waiting
// state, so a live runner left under a done card would work forever in the
// finished column with nothing able to move it.
func TestALaunchOntoADoneCardPicksItBackUp(t *testing.T) {
	got, move := statusAfterLaunchOnto(store.StatusDone)
	if !move {
		t.Fatal("starting a runner onto a finished card left it finished")
	}
	if got != store.StatusNeedsInput {
		t.Fatalf("a resumed done card landed in %q, want %q", got, store.StatusNeedsInput)
	}
}

// An offered item that nobody had started. Starting it is what the inbox is
// for, and a backlog card with a process on it is the same lie as a dead one.
func TestALaunchOntoABacklogCardTakesItOutOfTheInbox(t *testing.T) {
	got, move := statusAfterLaunchOnto(store.StatusBacklog)
	if !move {
		t.Fatal("an offered card kept sitting in the inbox with a runner on it")
	}
	if got != store.StatusNeedsInput {
		t.Fatalf("a started backlog card landed in %q, want %q", got, store.StatusNeedsInput)
	}
}

// A session that came up and got to work during the settle window must not be
// dragged back to `needs-input` to announce work it has already begun.
func TestALaunchDoesNotReopenACardThatIsAlreadyWorking(t *testing.T) {
	for _, status := range []string{
		store.StatusRunning, store.StatusNeedsInput, store.StatusNeedsPermission,
	} {
		if _, move := statusAfterLaunchOnto(status); move {
			t.Errorf("a launch moved a card that was already %q", status)
		}
	}
}

// THE REFUSAL THAT MATTERS. Shelving is an operator putting the work down, and
// the permission chain in daemon.go spends that: every request from a shelved
// card is refused unanswered. So a launch onto one either freezes the runner it
// just started, or quietly overturns the one status somebody chose by hand.
// Neither, and it says which way through.
func TestALaunchOntoAShelvedCardIsRefused(t *testing.T) {
	err := ontoRefusal(ontoCard(store.StatusShelved), false)
	if err == nil {
		t.Fatal("a runner was started onto a shelved card")
	}
	if !strings.Contains(err.Error(), "unshelve") {
		t.Fatalf("the refusal does not name the way through: %v", err)
	}
}

// And it must never do it by moving the card instead.
func TestShelvedIsNeverQuietlyUnshelved(t *testing.T) {
	if _, move := statusAfterLaunchOnto(store.StatusShelved); move {
		t.Fatal("a launch unshelved a card the operator had put down")
	}
}

// Two processes on one card write to one directory and the card ends up
// describing whichever spoke last. `handOverTo` already refuses this for the
// adopt path; the `task_id` path had no such guard at all.
func TestALaunchOntoALiveCardIsRefused(t *testing.T) {
	err := ontoRefusal(ontoCard(store.StatusRunning), true)
	if err == nil {
		t.Fatal("a second runner was started onto a card that already had one")
	}
	if !strings.Contains(err.Error(), "already has a runner") {
		t.Fatalf("the refusal does not say what is in the way: %v", err)
	}
}

// The ordinary case has to stay ordinary. A dead card with nothing on it is
// exactly what a relaunch is for, and refusing that would be worse than the
// bug.
func TestADeadCardWithNothingOnItIsNotRefused(t *testing.T) {
	if err := ontoRefusal(ontoCard(store.StatusDead), false); err != nil {
		t.Fatalf("relaunching a dead card was refused: %v", err)
	}
}

// A pid is never an identity. The operating system recycles them, so the pid
// on a card that died an hour ago can be true about somebody else's process,
// and believing it would refuse the relaunch this whole change exists to make
// work. Only a column that claims to be running gets the pid asked about.
func TestAStalePidOnADeadCardDoesNotCountAsLive(t *testing.T) {
	// No listeners and no store: `runnerIsLive` asks the supervisor and the
	// operating system, and neither of those is a daemon that has to be up.
	d := &Daemon{sup: newSupervisor()}

	// This test binary, which is very much alive, standing in for a recycled
	// pid on a card that is already filed dead.
	dead := &store.Task{ID: "recycled", Title: "old work", Status: store.StatusDead, PID: os.Getpid()}
	if d.runnerIsLive(dead) {
		t.Fatal("a dead card's stale pid was believed, which refuses the relaunch")
	}

	live := &store.Task{ID: "working", Title: "real work", Status: store.StatusRunning, PID: os.Getpid()}
	if !d.runnerIsLive(live) {
		t.Fatal("a running card with a live pid was not seen as live")
	}
}

// The supervisor is not a guess: it holds an entry only while the process
// atrium started is running. It answers before the status does.
func TestAnOwnedRunnerIsLiveWhateverTheCardSays(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer func() {
		cancel()
		<-errCh
	}()

	task, _, err := d.st.Register(store.Observed{
		WireName: "owned", Worktree: "/tmp/atrium-onto", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetStatus(task.ID, store.StatusDead); err != nil {
		t.Fatal(err)
	}
	d.sup.add(&runner{taskID: task.ID, done: make(chan struct{})})
	defer d.sup.remove(task.ID)

	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !d.runnerIsLive(got) {
		t.Fatal("a card atrium owns a runner for was not seen as live")
	}
	if err := ontoRefusal(got, d.runnerIsLive(got)); err == nil {
		t.Fatal("a second runner was allowed onto a card atrium already owns one for")
	}
}

// Launching onto a shelved card has to be refused by the endpoint, not only by
// the helper, and before anything is spawned so the card is left as it was.
func TestTheLaunchPathRefusesAShelvedCard(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer func() {
		cancel()
		<-errCh
	}()

	task, _, err := d.st.Register(store.Observed{
		WireName: "put-down", Worktree: "/tmp/atrium-shelved", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetStatus(task.ID, store.StatusShelved); err != nil {
		t.Fatal(err)
	}

	_, err = d.Launch(LaunchRequest{Harness: "claude", Cwd: t.TempDir(), TaskID: task.ID})
	if err == nil {
		t.Fatal("a launch onto a shelved card succeeded")
	}
	if !strings.Contains(err.Error(), "shelved") {
		t.Fatalf("the refusal does not say why: %v", err)
	}
	after, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != store.StatusShelved {
		t.Fatalf("a refused launch moved the card to %q", after.Status)
	}
}

// startedOnto is what the launch calls once the process has proved it is
// staying. It reads the card again rather than trusting what it saw before the
// spawn, so a SessionStart hook that landed during the settle window wins
// instead of racing.
func TestStartedOntoLeavesACardAHookAlreadyMoved(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer func() {
		cancel()
		<-errCh
	}()

	task, _, err := d.st.Register(store.Observed{
		WireName: "hooked", Worktree: "/tmp/atrium-hooked", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	// The hook got there first and the session is working.
	if err := d.st.SetStatus(task.ID, store.StatusRunning); err != nil {
		t.Fatal(err)
	}
	d.startedOnto(task.ID)

	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.StatusRunning {
		t.Fatalf("a working session was dragged back to %q", got.Status)
	}
}

// And the case the whole item is about: a card filed dead, started onto, and
// left saying dead while its process posted activity.
func TestStartedOntoBringsADeadCardBackWithAReason(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer func() {
		cancel()
		<-errCh
	}()

	task, _, err := d.st.Register(store.Observed{
		WireName: "restarted", Worktree: "/tmp/atrium-restarted", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetStatus(task.ID, store.StatusDead); err != nil {
		t.Fatal(err)
	}
	d.startedOnto(task.ID)

	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.StatusNeedsInput {
		t.Fatalf("a relaunched dead card is still %q", got.Status)
	}
	if got.WaitingReason != store.WaitingStarted {
		t.Fatalf("the card does not say it has only just started: %q", got.WaitingReason)
	}
}
