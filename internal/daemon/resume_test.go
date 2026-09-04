package daemon

import (
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func boolp(b bool) *bool { return &b }

// A session id is only worth storing once there is a conversation behind it.
// A session opened and never typed into has no transcript, and resuming its id
// answers "no conversation found". Storing it replaces one that works with one
// that cannot.
//
// The symptom was a fixture that lost its thread on every restart: resume
// fails, atrium starts fresh, the fresh session reports its own unresumable
// id, and tomorrow it fails again a little further from the original.
func TestAnUnwrittenSessionDoesNotClobberAGoodResumeID(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer func() {
		cancel()
		<-errCh
	}()

	// A conversation that happened.
	if err := d.onSession(SessionEvent{
		Agent: "keeper", Event: "start", Cwd: "d:/w", PID: 1,
		Resume: "real-conversation", Resumable: boolp(true),
	}); err != nil {
		t.Fatal(err)
	}
	task, err := d.st.GetByWireName("keeper")
	if err != nil {
		t.Fatal(err)
	}
	if task.ResumeID != "real-conversation" {
		t.Fatalf("resume id is %q, wanted the first one to be stored", task.ResumeID)
	}

	// A session with nothing written, ENDING. "which event" was the first
	// attempt at this rule and does not work: a fresh session that does
	// nothing still ends, and its end carried the same useless id.
	if err := d.onSession(SessionEvent{
		Agent: "keeper", Event: "end", Cwd: "d:/w", PID: 2,
		Resume: "nothing-written", Resumable: boolp(false),
	}); err != nil {
		t.Fatal(err)
	}
	task, err = d.st.GetByWireName("keeper")
	if err != nil {
		t.Fatal(err)
	}
	if task.ResumeID != "real-conversation" {
		t.Fatalf("an unwritten session overwrote the resume id with %q", task.ResumeID)
	}
}

// A session that HAS written a transcript replaces whatever was there. Without
// this the card would be stuck on the first conversation it ever saw.
func TestAWrittenSessionDoesUpdateTheResumeID(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer func() {
		cancel()
		<-errCh
	}()

	if err := d.onSession(SessionEvent{
		Agent: "mover", Event: "start", Cwd: "d:/w", PID: 1,
		Resume: "first", Resumable: boolp(true),
	}); err != nil {
		t.Fatal(err)
	}
	if err := d.onSession(SessionEvent{
		Agent: "mover", Event: "end", Cwd: "d:/w", PID: 1,
		Resume: "second", Resumable: boolp(true),
	}); err != nil {
		t.Fatal(err)
	}
	task, err := d.st.GetByWireName("mover")
	if err != nil {
		t.Fatal(err)
	}
	if task.ResumeID != "second" {
		t.Fatalf("resume id is %q, wanted the written one", task.ResumeID)
	}
}

// A hook that says nothing is trusted, which is what every hook did before
// this existed. An older binary must not stop recording resume ids entirely.
func TestAHookThatSaysNothingIsTrusted(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer func() {
		cancel()
		<-errCh
	}()

	if err := d.onSession(SessionEvent{
		Agent: "old-hook", Event: "start", Cwd: "d:/w", PID: 1, Resume: "whatever",
	}); err != nil {
		t.Fatal(err)
	}
	task, err := d.st.GetByWireName("old-hook")
	if err != nil {
		t.Fatal(err)
	}
	if task.ResumeID != "whatever" {
		t.Fatalf("resume id is %q, wanted it recorded when nothing was claimed", task.ResumeID)
	}
}

// A session STARTING has no transcript to report, so the hook sends no opinion
// and the transcript check cannot help. What is known is that its id names
// nothing yet and the card's names something that worked, so the card wins.
//
// This is the case that actually bit: the transcript check alone was not
// enough, because at SessionStart there is nothing to look at.
func TestAStartingSessionKeepsTheCardsResumeID(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer func() {
		cancel()
		<-errCh
	}()

	if err := d.onSession(SessionEvent{
		Agent: "held", Event: "start", Cwd: "d:/w", PID: 1,
		Resume: "the-real-one", Resumable: boolp(true),
	}); err != nil {
		t.Fatal(err)
	}
	// A later session starting, saying nothing about a transcript because it
	// has none yet.
	if err := d.onSession(SessionEvent{
		Agent: "held", Event: "start", Cwd: "d:/w", PID: 2, Resume: "brand-new",
	}); err != nil {
		t.Fatal(err)
	}
	task, err := d.st.GetByWireName("held")
	if err != nil {
		t.Fatal(err)
	}
	if task.ResumeID != "the-real-one" {
		t.Fatalf("a starting session took the resume id: %q", task.ResumeID)
	}

	// And it is picked up once that session has actually written something.
	if err := d.onSession(SessionEvent{
		Agent: "held", Event: "end", Cwd: "d:/w", PID: 2,
		Resume: "brand-new", Resumable: boolp(true),
	}); err != nil {
		t.Fatal(err)
	}
	task, err = d.st.GetByWireName("held")
	if err != nil {
		t.Fatal(err)
	}
	if task.ResumeID != "brand-new" {
		t.Fatalf("resume id is %q, wanted the written conversation", task.ResumeID)
	}
}

// A card with nothing stored takes whatever the first session offers. Refusing
// would leave a brand new card with no way to resume at all.
func TestAFirstStartStillRecordsItsResumeID(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer func() {
		cancel()
		<-errCh
	}()

	if err := d.onSession(SessionEvent{
		Agent: "fresh-resume", Event: "start", Cwd: "d:/w", PID: 1, Resume: "only-one",
	}); err != nil {
		t.Fatal(err)
	}
	task, err := d.st.GetByWireName("fresh-resume")
	if err != nil {
		t.Fatal(err)
	}
	if task.ResumeID != "only-one" {
		t.Fatalf("resume id is %q, wanted only-one", task.ResumeID)
	}
	if task.Status != store.StatusNeedsInput {
		t.Fatalf("status is %q, wanted a fresh session to be ready", task.Status)
	}
}

// Two runners on one conversation braid its transcript, and nothing reports an
// error while it happens. The file is append only, so each process writes turns
// that do not account for the other's, and the next resume replays the braid as
// one confused thread.
//
// Claude Code guards its own BACKGROUNDED sessions and does not guard this
// case: two foreground resumes of the same id both open, silently, which is how
// this was found.
func TestASecondRunnerCannotResumeALiveConversation(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer func() {
		cancel()
		<-errCh
	}()

	if err := d.resumeIsFree("busy-conversation"); err != nil {
		t.Fatalf("nothing is running it yet, so it should be free: %v", err)
	}

	if err := d.onSession(SessionEvent{
		Agent: "first", Event: "start", Cwd: "d:/w", PID: 1,
		Resume: "busy-conversation", Resumable: boolp(true),
	}); err != nil {
		t.Fatal(err)
	}
	task, err := d.st.GetByWireName("first")
	if err != nil {
		t.Fatal(err)
	}

	// Still free: the card carries the id, but atrium owns no runner on it.
	// Refusing here would block resuming a session that has ended, which is the
	// case resume exists for.
	if err := d.resumeIsFree("busy-conversation"); err != nil {
		t.Fatalf("no runner owns it, so it should still be free: %v", err)
	}

	// Now atrium owns a terminal on that card.
	d.sup.add(&runner{taskID: task.ID})
	if err := d.resumeIsFree("busy-conversation"); err == nil {
		t.Fatal("a second runner was allowed onto a conversation atrium is already running")
	}
	// An unrelated conversation is unaffected. The guard is about the session
	// id, not about the directory or the card.
	if err := d.resumeIsFree("some-other-conversation"); err != nil {
		t.Fatalf("an unrelated conversation was blocked: %v", err)
	}
	// A fresh start is never blocked. Two runners in one directory with no
	// shared conversation write to different transcripts, which is a real thing
	// to want.
	if err := d.resumeIsFree(""); err != nil {
		t.Fatalf("a fresh start was blocked: %v", err)
	}
}
