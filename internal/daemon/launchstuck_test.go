package daemon

import (
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

const modelSwitchFrame = "\x1b[2J\x1b[H" +
	"────────────────────────────────────────────────────────────\r\n" +
	" Model switch\r\n\r\n" +
	" Opus 5.5's safeguards flagged this session. Switch to Opus 4.8?\r\n\r\n" +
	" ❯ 1. Switch automatically\r\n   2. Stay on Opus 5.5\r\n\r\n" +
	" Enter to select · ↑/↓ to navigate · Esc to cancel\r\n"

const trustFrame = "\x1b[2J\x1b[H" +
	"────────────────────────────────────────────────────────────\r\n" +
	" Do you trust the files in this folder?\r\n\r\n" +
	" ❯ 1. Yes, I trust this folder\r\n   2. No, exit\r\n"

// stuckWorker is an agent-launched claude card whose runner started `age` ago and
// whose pty last spoke `quiet` ago showing `frame`, with no hook heard.
func stuckWorker(t *testing.T, d *Daemon, frame string, age, quiet time.Duration) (*store.Task, *runner) {
	t.Helper()
	orch := peerCard(t, d, "orchestrator")
	task := peerCard(t, d, "stuck-worker")
	if err := d.st.SetTags(task.ID, []string{OriginAgentTag, SubagentTag}); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetLineage(task.ID, "orchestrator", orch.ID); err != nil {
		t.Fatal(err)
	}
	prompt(t, d, task.ID)
	r := &runner{
		taskID: task.ID, pty: newFakePTY(), started: time.Now().Add(-age),
		buf: newRing(1<<16, 80), watchers: map[chan []byte]struct{}{}, done: make(chan struct{}),
	}
	_, _ = r.buf.Write([]byte(frame))
	r.lastOut.Store(time.Now().Add(-quiet).UnixNano())
	d.sup.add(r)
	got, _ := d.st.Get(task.ID)
	return got, r
}

func TestLaunchIdleAfterGraceWithNoHook(t *testing.T) {
	d := testDaemon(t)
	task, _ := stuckWorker(t, d, "starting up\r\n", 5*time.Minute, 5*time.Minute)
	x := d.launchStuck(task, time.Now())
	if x == nil || x.Source != NoticeLaunchIdle || x.Prompt != "" {
		t.Fatalf("got %+v, want launch-idle", x)
	}
	if !strings.Contains(x.Text, "no activity since launch, 5 min") {
		t.Fatalf("text %q", x.Text)
	}
	if x.Minutes != 5 || x.Count < 1 {
		t.Fatalf("minutes %d count %d", x.Minutes, x.Count)
	}
}

func TestLaunchIdleWaitsOutTheGrace(t *testing.T) {
	d := testDaemon(t)
	task, _ := stuckWorker(t, d, "starting up\r\n", 20*time.Second, time.Hour)
	if x := d.launchStuck(task, time.Now()); x != nil {
		t.Fatalf("%+v inside the grace", x)
	}
}

func TestLaunchIdleNeedsAQuietTerminal(t *testing.T) {
	d := testDaemon(t)
	task, _ := stuckWorker(t, d, "working\r\n", 5*time.Minute, time.Second)
	if x := d.launchStuck(task, time.Now()); x != nil {
		t.Fatalf("%+v on a terminal that is still drawing", x)
	}
}

func TestLaunchIdleIsForCardsWhoseHooksAreExpected(t *testing.T) {
	d := testDaemon(t)
	task := peerCard(t, d, "by-hand")
	r := &runner{taskID: task.ID, pty: newFakePTY(), started: time.Now().Add(-time.Hour),
		buf: newRing(1<<16, 80), watchers: map[chan []byte]struct{}{}, done: make(chan struct{})}
	r.lastOut.Store(time.Now().Add(-time.Hour).UnixNano())
	d.sup.add(r)
	if x := d.launchStuck(task, time.Now()); x != nil {
		t.Fatalf("%+v for a hand-started card with no hooks", x)
	}
}

func TestAHookClearsLaunchIdle(t *testing.T) {
	d := testDaemon(t)
	task, _ := stuckWorker(t, d, "x\r\n", 5*time.Minute, 5*time.Minute)
	d.act.set(task.ID, ActivityThinking, "")
	if x := d.launchStuck(task, time.Now()); x != nil {
		t.Fatalf("%+v after a hook", x)
	}
}

func TestLaunchPromptFolderTrust(t *testing.T) {
	d := testDaemon(t)
	task, _ := stuckWorker(t, d, trustFrame, 30*time.Second, 10*time.Second)
	x := d.launchStuck(task, time.Now())
	if x == nil || x.Source != NoticeLaunchPrompt || x.Prompt != PromptFolderTrust {
		t.Fatalf("got %+v", x)
	}
	if !strings.Contains(x.Text, "stuck at the folder-trust prompt") && !strings.Contains(x.Text, "STUCK at the folder-trust prompt") {
		t.Fatalf("text %q", x.Text)
	}
}

func TestLaunchPromptKinds(t *testing.T) {
	cases := map[string]string{
		"Select login method:\r\n 1. Claude account\r\n":                                PromptLogin,
		"Update available!\r\n 1. Update now\r\n Enter to select · ↑/↓ to navigate\r\n": PromptUpdate,
		" Weird\r\n 1. a\r\n Enter to select · ↑/↓ to navigate\r\n":                     PromptOther,
	}
	for frame, want := range cases {
		h := readFrame(frame)
		if h.prompt != want {
			t.Errorf("%q: prompt %q, want %q", frame, h.prompt, want)
		}
	}
	if h := readFrame(" Weird\r\n 1. a\r\n Enter to select · ↑/↓ to navigate\r\n"); h.title != "Weird" {
		t.Errorf("other carries %q", h.title)
	}
}

func TestLaunchPromptNeedsQuiet(t *testing.T) {
	d := testDaemon(t)
	task, _ := stuckWorker(t, d, trustFrame, 30*time.Second, 0)
	// a young, still drawing screen is neither a prompt nor idle
	if x := d.launchStuck(task, time.Now()); x != nil {
		t.Fatalf("%+v while the pty is still talking", x)
	}
}

func TestLaunchPromptBeatsLaunchIdle(t *testing.T) {
	d := testDaemon(t)
	task, _ := stuckWorker(t, d, trustFrame, 10*time.Minute, 10*time.Minute)
	if x := d.launchStuck(task, time.Now()); x == nil || x.Source != NoticeLaunchPrompt {
		t.Fatalf("got %+v", x)
	}
}

// The exact text of the 3.8 hour stall.
func TestTerminalMenuModelSwitch(t *testing.T) {
	d := testDaemon(t)
	task, _ := stuckWorker(t, d, modelSwitchFrame, 4*time.Hour, 4*time.Hour)
	d.act.set(task.ID, ActivityThinking, "")
	x := d.launchStuck(task, time.Now())
	if x == nil || x.Source != NoticeTerminalMenu || x.Prompt != PromptModelSwitch {
		t.Fatalf("got %+v", x)
	}
	if !strings.Contains(x.Text, "Model switch") {
		t.Fatalf("text %q lacks the menu title", x.Text)
	}
	if x.Count < 5 {
		t.Fatalf("count %d: a menu hours old should be past the early steps", x.Count)
	}
}

func TestTerminalMenuOtherAndOnNonReportingCard(t *testing.T) {
	d := testDaemon(t)
	task := peerCard(t, d, "by-hand")
	r := &runner{taskID: task.ID, pty: newFakePTY(), started: time.Now().Add(-time.Hour),
		buf: newRing(1<<16, 80), watchers: map[chan []byte]struct{}{}, done: make(chan struct{})}
	_, _ = r.buf.Write([]byte("\x1b[2J\x1b[H────────────────────────────\r\n Pick a thing\r\n 1. a\r\n Enter to select · ↑/↓ to navigate\r\n"))
	r.lastOut.Store(time.Now().Add(-time.Minute).UnixNano())
	d.sup.add(r)
	d.act.set(task.ID, ActivityThinking, "")
	if err := d.st.SetStatus(task.ID, store.StatusRunning); err != nil {
		t.Fatal(err)
	}
	if err := d.watchWorkers(time.Now()); err != nil {
		t.Fatal(err)
	}
	x := d.esc.get(task.ID)
	if x == nil || x.Source != NoticeTerminalMenu || x.Prompt != PromptOther || !strings.Contains(x.Text, "Pick a thing") {
		t.Fatalf("got %+v", x)
	}
}

func TestTerminalMenuNeedsQuietAndAMenu(t *testing.T) {
	d := testDaemon(t)
	task, r := stuckWorker(t, d, modelSwitchFrame, 4*time.Hour, 5*time.Second)
	d.act.set(task.ID, ActivityThinking, "")
	if x := d.launchStuck(task, time.Now()); x != nil {
		t.Fatalf("%+v on a menu that appeared five seconds ago", x)
	}
	r.lastOut.Store(time.Now().Add(-time.Hour).UnixNano())
	_, _ = r.buf.Write([]byte("\x1b[2J\x1b[Hback at the prompt\r\n"))
	if x := d.launchStuck(task, time.Now()); x != nil {
		t.Fatalf("%+v after the menu was answered", x)
	}
}

func TestTerminalMenuIsMemoizedWhileTheTerminalIsQuiet(t *testing.T) {
	d := testDaemon(t)
	task, r := stuckWorker(t, d, modelSwitchFrame, time.Hour, time.Hour)
	d.act.set(task.ID, ActivityThinking, "")
	now := time.Now()
	if d.launchStuck(task, now) == nil {
		t.Fatal("no escalation")
	}
	// Change the ring without a new last-output time: the memo answers, so the
	// frame was not rendered again.
	_, _ = r.buf.Write([]byte("\x1b[2J\x1b[Hgone\r\n"))
	if d.launchStuck(task, now) == nil {
		t.Fatal("the quiet card was rendered again")
	}
	r.lastOut.Store(time.Now().Add(-45 * time.Second).UnixNano())
	if d.launchStuck(task, time.Now()) != nil {
		t.Fatal("a card that spoke was not rendered again")
	}
}

func TestBusyCardIsNeverRendered(t *testing.T) {
	d := testDaemon(t)
	task, _ := stuckWorker(t, d, modelSwitchFrame, time.Hour, 2*time.Second)
	d.act.set(task.ID, ActivityThinking, "")
	_ = d.launchStuck(task, time.Now())
	if _, ok := d.scans.get(task.ID, 0); ok {
		t.Fatal("unexpected memo")
	}
	d.scans.mu.Lock()
	n := len(d.scans.by)
	d.scans.mu.Unlock()
	if n != 0 {
		t.Fatalf("a card that spoke two seconds ago was rendered (%d memos)", n)
	}
}

func TestAPendingPermissionIsNotAMenu(t *testing.T) {
	d := testDaemon(t)
	task, _ := stuckWorker(t, d, modelSwitchFrame, time.Hour, time.Hour)
	d.act.set(task.ID, ActivityThinking, "")
	d.act.dialogRaised(task.ID)
	if x := d.launchStuck(task, time.Now()); x != nil {
		t.Fatalf("%+v for a dialog a notification already raised", x)
	}
}

// Addendum 1: never the silent-stop wording for a card that never began.
func TestACardNoHookHasHeardIsLaunchIdleNeverSilentStop(t *testing.T) {
	d := testDaemon(t)
	task, _ := stuckWorker(t, d, "x\r\n", 10*time.Minute, 10*time.Minute)
	// The pty went quiet and the store moved it to waiting, with no hook behind it.
	if err := d.st.SetStatus(task.ID, store.StatusNeedsInput); err != nil {
		t.Fatal(err)
	}
	got, _ := d.st.Get(task.ID)
	if ended, _ := d.st.TurnEndedAt(task.ID); ended == nil {
		t.Skip("this store records no turn end without a hook")
	}
	if _, ok := d.stoppedSilently(got); ok {
		t.Fatal("stoppedSilently called a card that never began a turn")
	}
	x := d.stuckNow(got, time.Now().Add(time.Hour))
	if x == nil || x.Source != NoticeLaunchIdle || strings.Contains(x.Text, "stopped without reporting") {
		t.Fatalf("got %+v", x)
	}
}

func TestEscalationCountsFromWhenItBecameStuck(t *testing.T) {
	d := testDaemon(t)
	task, _ := stuckWorker(t, d, "x\r\n", 90*time.Minute, 90*time.Minute)
	x := d.launchStuck(task, time.Now())
	if x == nil || x.Count != escalationStep(time.Since(x.Since)) {
		t.Fatalf("got %+v", x)
	}
	if x.Since.After(time.Now().Add(-80 * time.Minute)) {
		t.Fatalf("since %v is not near the launch", x.Since)
	}
}

func TestLaunchStuckIsClaudeOnly(t *testing.T) {
	d := testDaemon(t)
	task, _ := stuckWorker(t, d, trustFrame, time.Hour, time.Hour)
	task.Runner = "codex"
	if x := d.launchStuck(task, time.Now()); x != nil {
		t.Fatalf("%+v for a runner whose screens are not known", x)
	}
}

func TestAToolHookAfterTheStartClearsItAndOneBeforeDoesNot(t *testing.T) {
	d := testDaemon(t)
	task, r := stuckWorker(t, d, "x\r\n", 10*time.Minute, 10*time.Minute)
	before := r.started.Add(-time.Hour)
	task.ToolHookSeenAt = &before
	if x := d.launchStuck(task, time.Now()); x == nil || x.Source != NoticeLaunchIdle {
		t.Fatalf("a hook from a previous run cleared it: %+v", x)
	}
	after := r.started.Add(time.Minute)
	task.ToolHookSeenAt = &after
	if x := d.launchStuck(task, time.Now()); x != nil {
		t.Fatalf("%+v after a tool hook", x)
	}
	task.ToolHookSeenAt = nil
	task.StopHookSeenAt = &after
	if x := d.launchStuck(task, time.Now()); x != nil {
		t.Fatalf("%+v after a stop hook", x)
	}
}

func TestOnlyTheGenericMenuFooterMakesAMenu(t *testing.T) {
	if h := readFrame(" Title\r\n 1. a\r\n Enter to select\r\n"); h.menu {
		t.Fatal("a footer without the navigate half is not the marker")
	}
	if h := readFrame(" Title\r\n 1. a\r\n Enter to select · ↑/↓ to navigate\r\n"); !h.menu {
		t.Fatal("the marker footer was missed")
	}
}

func TestASessionStartHookCountsAsHeard(t *testing.T) {
	d := testDaemon(t)
	task, _ := stuckWorker(t, d, "x\r\n", 10*time.Minute, 10*time.Minute)
	if x := d.launchStuck(task, time.Now()); x == nil {
		t.Fatal("not stuck before the hook")
	}
	if err := d.onSession(SessionEvent{Agent: task.WireName, Event: "start", Source: "startup", TaskID: task.ID}); err != nil {
		t.Fatal(err)
	}
	if x := d.launchStuck(task, time.Now()); x != nil {
		t.Fatalf("%+v after its session hook spoke", x)
	}
}

const clearScreen = "\x1b[2J\x1b[H"

// The review's probe: a grep output line quoting the footer, above claude's input box.
const quotedFooterFrame = clearScreen +
	"⏺ Bash(grep -n navigate launchstuck_test.go)\r\n" +
	"  ⎿  17:  \" Enter to select · ↑/↓ to navigate · Esc\" +\r\n" +
	"     18:  \" to cancel\"\r\n\r\n" +
	"╭──────────────────────────────────────────────────────╮\r\n" +
	"│ > \r\n" +
	"╰──────────────────────────────────────────────────────╯\r\n" +
	"  ? for shortcuts\r\n"

func TestAFooterQuotedAboveTheInputBoxIsNotAMenu(t *testing.T) {
	if h := readFrame(strings.TrimPrefix(quotedFooterFrame, clearScreen)); h.menu || h.prompt != "" {
		t.Fatalf("a quoted footer read as a menu: %+v", h)
	}
	d := testDaemon(t)
	task, _ := stuckWorker(t, d, quotedFooterFrame, time.Hour, time.Minute)
	d.act.set(task.ID, ActivityThinking, "")
	if x := d.launchStuck(task, time.Now()); x != nil {
		t.Fatalf("%+v for a footer quoted in tool output", x)
	}
}

func TestTheFooterMustBeWholeAndOnTheLastTwoLines(t *testing.T) {
	menu := " Pick\r\n 1. a\r\n Enter to select · ↑/↓ to navigate · Esc to cancel\r\n"
	if !readFrame(menu).menu {
		t.Fatal("the footer on the last line was missed")
	}
	if !readFrame(menu + " extra status line\r\n").menu {
		t.Fatal("the footer on the second to last line was missed")
	}
	if readFrame(menu + " one\r\n two\r\n").menu {
		t.Fatal("a footer three lines up read as a menu")
	}
	// the two halves apart, on separate lines at the bottom, are not the marker
	if readFrame(" Pick\r\n Enter to select\r\n ↑/↓ to navigate\r\n").menu {
		t.Fatal("the halves matched separately")
	}
}

func TestAnUpdateBannerIsNotAPromptButAnUpdateDialogIs(t *testing.T) {
	if h := readFrame(" Update available! Run claude update\r\n"); h.prompt != "" {
		t.Fatalf("a banner read as %q", h.prompt)
	}
	if h := readFrame(" Update available!\r\n 1. Update now\r\n Enter to select · ↑/↓ to navigate\r\n"); h.prompt != PromptUpdate {
		t.Fatalf("a dialog read as %q", h.prompt)
	}
}

// A real silent stop is not hidden behind a menu on the screen.
func TestASilentStopWinsOverAMenuOnAnEndedTurn(t *testing.T) {
	d := testDaemon(t)
	task, _ := stuckWorker(t, d, modelSwitchFrame, time.Hour, time.Hour)
	d.act.set(task.ID, ActivityThinking, "")
	stopTurn(t, d, "stuck-worker")
	got, _ := d.st.Get(task.ID)
	if _, ok := d.stoppedSilently(got); !ok {
		t.Fatal("setup: the turn ended and owes a report, so this is a silent stop")
	}
	x := d.stuckNow(got, time.Now().Add(time.Hour))
	if x == nil || x.Source != NoticeSilentStop {
		t.Fatalf("got %+v, want the silent stop", x)
	}
}

func TestAPendingPermissionOrNeedsPermissionIsNotAMenu(t *testing.T) {
	d := testDaemon(t)
	task, _ := stuckWorker(t, d, modelSwitchFrame, time.Hour, time.Hour)
	d.act.set(task.ID, ActivityThinking, "")
	if x := d.launchStuck(task, time.Now()); x == nil {
		t.Fatal("setup: a menu should escalate")
	}
	task.Status = store.StatusNeedsPermission
	if x := d.launchStuck(task, time.Now()); x != nil {
		t.Fatalf("%+v on a card waiting on a permission", x)
	}
	task.Status = store.StatusRunning
	if _, _, err := d.st.RecordPermission(task.ID, "Bash", "ls", "k1", ""); err != nil {
		t.Skipf("no way to raise a pending permission here: %v", err)
	}
	if x := d.launchStuck(task, time.Now()); x != nil {
		t.Fatalf("%+v on a card with a permission atrium holds", x)
	}
}

// A resume starts unheard: the last run's session hook must not count.
func TestAResumeAfterAnExitStartsUnheard(t *testing.T) {
	d := testDaemon(t)
	task, _ := stuckWorker(t, d, "x\r\n", 10*time.Minute, 10*time.Minute)
	d.act.sessionSpoke(task.ID)
	if x := d.launchStuck(task, time.Now()); x != nil {
		t.Fatalf("%+v after the session hook", x)
	}
	d.act.forget(task.ID)
	if x := d.launchStuck(task, time.Now()); x == nil || x.Source != NoticeLaunchIdle {
		t.Fatalf("a resumed card counted as heard from its last run: %+v", x)
	}
}
