package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// Stage 1 of docs/a2a-reliability-design.md: every failure mode it covers has
// a test here. F1 silent stop, F5 an ambiguous report, F6 no loop, F7 a stuck
// tool, F8 lineage, F13 notices not rate limited, F14 an unverified sha, F15 a
// message nothing will deliver.

// launchedPair is a launcher and a worker it launched, with the worker
// prompted and running, which is where a worker is after `atrium_launch`.
func launchedPair(t *testing.T, d *Daemon) (launcher, worker *store.Task) {
	t.Helper()
	launcher = peerCard(t, d, "orchestrator")
	worker = peerCard(t, d, "worker")
	if err := d.st.SetTags(worker.ID, []string{"sdk", OriginAgentTag}); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetLineage(worker.ID, "orchestrator", launcher.ID); err != nil {
		t.Fatal(err)
	}
	prompt(t, d, worker.ID)
	return launcher, worker
}

func prompt(t *testing.T, d *Daemon, id string) {
	t.Helper()
	if err := d.st.SetStatus(id, store.StatusRunning); err != nil {
		t.Fatal(err)
	}
	if err := d.st.AppendEvent(id, store.EventPrompted, map[string]any{"text": "do the thing"}); err != nil {
		t.Fatal(err)
	}
}

// stopTurn runs the Stop hook for a session and returns what it answered.
func stopTurn(t *testing.T, d *Daemon, agent string) string {
	t.Helper()
	raw, _ := json.Marshal(map[string]string{"agent": agent})
	rec := httptest.NewRecorder()
	d.handleStop(rec, httptest.NewRequest(http.MethodPost, "/stop", bytes.NewReader(raw)))
	return strings.TrimSpace(rec.Body.String())
}

func pendingFrom(t *testing.T, d *Daemon, id string) []*store.Message {
	t.Helper()
	msgs, err := d.st.PendingMessages(id)
	if err != nil {
		t.Fatal(err)
	}
	return msgs
}

func finishWith(t *testing.T, d *Daemon, in FinishRequest) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(in)
	rec := httptest.NewRecorder()
	d.handleFinish(rec, httptest.NewRequest(http.MethodPost, "/finish", bytes.NewReader(raw)))
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

// F1. The sa20 case: a worker ends its turn with nothing said. Its launcher
// hears about it as the turn ends, once, and the Stop hook does NOT block.
// Atrium never forces a turn.
func TestASilentStopTellsTheLauncherAndNeverBlocks(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)

	if got := stopTurn(t, d, "worker"); got != "{}" {
		t.Fatalf("the Stop hook answered %s, want nothing: atrium must never force a turn", got)
	}
	msgs := pendingFrom(t, d, launcher.ID)
	if len(msgs) != 1 {
		t.Fatalf("the launcher has %d messages, want one silent-stop notice", len(msgs))
	}
	if !strings.Contains(msgs[0].Text, "without reporting") || !strings.Contains(msgs[0].Text, worker.ID) {
		t.Fatalf("the notice says %q", msgs[0].Text)
	}
	if msgs[0].FromPeer != worker.WireName {
		t.Fatalf("the notice is from %q, want the worker so a reply reaches it", msgs[0].FromPeer)
	}
}

// F6. The same stop seen again is one notice, not two.
func TestASecondStopOnOnePromptIsNotASecondNotice(t *testing.T) {
	d := testDaemon(t)
	launcher, _ := launchedPair(t, d)
	stopTurn(t, d, "worker")
	stopTurn(t, d, "worker")
	if n := len(pendingFrom(t, d, launcher.ID)); n != 1 {
		t.Fatalf("%d notices for one prompt", n)
	}
}

// A new prompt is a new turn owed, so a silent stop after it is a new notice.
func TestEachPromptGetsItsOwnSilentStop(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	stopTurn(t, d, "worker")
	time.Sleep(5 * time.Millisecond)
	prompt(t, d, worker.ID)
	stopTurn(t, d, "worker")
	if n := len(pendingFrom(t, d, launcher.ID)); n != 2 {
		t.Fatalf("%d notices for two prompts", n)
	}
}

// A structured report pays the turn. The launcher gets the report, verbatim,
// and no silent-stop notice after it.
func TestAReportIsNotASilentStop(t *testing.T) {
	d := testDaemon(t)
	launcher, _ := launchedPair(t, d)
	rec, out := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone,
		NoCommit: "research only", Recap: "the matrix is written up"})
	if rec.Code != http.StatusOK || out["launcher_told"] != true {
		t.Fatalf("report answered %d: %s", rec.Code, rec.Body)
	}
	stopTurn(t, d, "worker")
	msgs := pendingFrom(t, d, launcher.ID)
	if len(msgs) != 1 || !strings.Contains(msgs[0].Text, "report from") ||
		!strings.Contains(msgs[0].Text, "the matrix is written up") {
		t.Fatalf("the launcher has %d: %v", len(msgs), msgs)
	}
}

// Clint's decision: an atrium_say to the launcher counts as a report.
func TestSayingSomethingToTheLauncherCountsAsAReport(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/tasks/"+launcher.ID+"/message",
		strings.NewReader(`{"text":"done, sha abc1234","from":"`+worker.WireName+`"}`))
	req.SetPathValue("id", launcher.ID)
	d.handleMessage(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("say answered %d: %s", rec.Code, rec.Body)
	}
	stopTurn(t, d, "worker")
	msgs := pendingFrom(t, d, launcher.ID)
	if len(msgs) != 1 || msgs[0].Text != "done, sha abc1234" {
		t.Fatalf("the launcher has %v, want only what the worker said", msgs)
	}
}

// A human's card is left exactly as it was.
func TestAHumanCardIsUntouchedByTheGuard(t *testing.T) {
	d := testDaemon(t)
	other := peerCard(t, d, "someone")
	mine := peerCard(t, d, "mine")
	prompt(t, d, mine.ID)
	if got := stopTurn(t, d, "mine"); got != "{}" {
		t.Fatalf("a human card's Stop answered %s", got)
	}
	if n := len(pendingFrom(t, d, other.ID)); n != 0 {
		t.Fatalf("somebody was told about a human card: %d", n)
	}
}

// The Stop hook still carries queued messages, exactly as it always did.
func TestTheStopHookStillDeliversQueuedMessages(t *testing.T) {
	d := testDaemon(t)
	_, worker := launchedPair(t, d)
	if _, err := d.st.QueueFromPeer(worker.ID, "rebase first", "orchestrator"); err != nil {
		t.Fatal(err)
	}
	got := stopTurn(t, d, "worker")
	if !strings.Contains(got, `"decision":"block"`) || !strings.Contains(got, "rebase first") {
		t.Fatalf("the Stop hook answered %s, want the message delivered", got)
	}
}

// F13. A notice is not a model looping, so the peer limit does not drop it.
func TestANoticeIsNotRateLimited(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	for i := 0; i < peerSendsPerMinute+1; i++ {
		d.peerLimit.allow(worker.WireName)
	}
	stopTurn(t, d, "worker")
	if n := len(pendingFrom(t, d, launcher.ID)); n != 1 {
		t.Fatalf("a notice was dropped to the rate limit: %d", n)
	}
}

// F5. A done with no commit, or a blocked with no ask, is refused with what
// is missing, and the card does not move.
func TestAnIncompleteReportIsRefused(t *testing.T) {
	d := testDaemon(t)
	_, worker := launchedPair(t, d)
	for _, in := range []FinishRequest{
		{Agent: "worker", Status: ReportDone},
		{Agent: "worker"},
		{Agent: "worker", Status: ReportBlocked},
		{Agent: "worker", Status: ReportQuestion},
		{Agent: "worker", Status: "finished-ish"},
		{Agent: "worker", Status: ReportDone, SHA: "not a sha"},
	} {
		rec, _ := finishWith(t, d, in)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%+v answered %d, want refused", in, rec.Code)
		}
	}
	got, _ := d.st.Get(worker.ID)
	if got.Status != store.StatusRunning {
		t.Fatalf("a refused report moved the card to %s", got.Status)
	}
}

// A blocked report puts the card in needs-input with what it needs.
func TestABlockedReportSaysWhatItNeeds(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	rec, _ := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportBlocked,
		Recap: "the build is red", Ask: "clint: which go version"})
	if rec.Code != http.StatusOK {
		t.Fatalf("answered %d: %s", rec.Code, rec.Body)
	}
	got, _ := d.st.Get(worker.ID)
	if got.Status != store.StatusNeedsInput {
		t.Fatalf("status %s", got.Status)
	}
	msgs := pendingFrom(t, d, launcher.ID)
	if len(msgs) != 1 || !strings.Contains(msgs[0].Text, "needs: clint: which go version") {
		t.Fatalf("launcher has %v", msgs)
	}
}

// A human's own `atrium finish` keeps working with no sha.
func TestAHumansFinishNeedsNoSha(t *testing.T) {
	d := testDaemon(t)
	peerCard(t, d, "mine")
	if rec, _ := finishWith(t, d, FinishRequest{Agent: "mine", Recap: "done"}); rec.Code != http.StatusOK {
		t.Fatalf("answered %d: %s", rec.Code, rec.Body)
	}
}

// F14. A sha the worktree does not have is accepted and flagged.
func TestAnUnverifiedShaIsAcceptedAndFlagged(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	was := commitExists
	commitExists = func(string, string) bool { return false }
	t.Cleanup(func() { commitExists = was })

	rec, out := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, SHA: "abc1234"})
	if rec.Code != http.StatusOK || out["unverified"] != true {
		t.Fatalf("answered %d: %s", rec.Code, rec.Body)
	}
	got, _ := d.st.Get(worker.ID)
	if !got.ReportUnverified || got.ReportSHA != "abc1234" || got.Status != store.StatusDone {
		t.Fatalf("card: unverified %v sha %q status %s", got.ReportUnverified, got.ReportSHA, got.Status)
	}
	msgs := pendingFrom(t, d, launcher.ID)
	if len(msgs) != 1 || !strings.Contains(msgs[0].Text, "unverified") {
		t.Fatalf("launcher has %v", msgs)
	}
}

// A sha that is there is not flagged.
func TestAVerifiedShaIsNotFlagged(t *testing.T) {
	d := testDaemon(t)
	_, worker := launchedPair(t, d)
	was := commitExists
	commitExists = func(string, string) bool { return true }
	t.Cleanup(func() { commitExists = was })
	finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, SHA: "abc1234"})
	got, _ := d.st.Get(worker.ID)
	if got.ReportUnverified {
		t.Fatal("a commit that exists was flagged")
	}
}

// The operator's backoff, measured from the moment a card got stuck.
func TestTheEscalationBackoff(t *testing.T) {
	for _, c := range []struct {
		after time.Duration
		step  int
	}{
		{0, 0}, {59 * time.Second, 0}, {time.Minute, 1}, {2 * time.Minute, 2}, {4 * time.Minute, 2},
		{5 * time.Minute, 3}, {10 * time.Minute, 4}, {30 * time.Minute, 5}, {time.Hour, 6},
		{8 * time.Hour, 9}, {24 * time.Hour, 10}, {48 * time.Hour, 11},
	} {
		if got := escalationStep(c.after); got != c.step {
			t.Errorf("after %s: step %d, want %d", c.after, got, c.step)
		}
	}
}

// F1 without a Stop hook: the watchdog tells the launcher, then escalates to
// the board on the backoff, and forgets it the moment the card moves.
func TestTheWatchdogCatchesASilentStopAndResetsWhenTheCardMoves(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	d.turnEnded(worker.ID)
	got, _ := d.st.Get(worker.ID)
	stopped := got.WaitingSinceOr(time.Now())

	if err := d.watchWorkers(stopped.Add(30 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if n := len(pendingFrom(t, d, launcher.ID)); n != 0 {
		t.Fatalf("the launcher was told before the grace was up: %d", n)
	}
	if x := d.esc.get(worker.ID); x == nil || x.Count != 0 {
		t.Fatalf("escalation %+v, want one not yet rung", x)
	}

	if err := d.watchWorkers(stopped.Add(3 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if n := len(pendingFrom(t, d, launcher.ID)); n != 1 {
		t.Fatalf("the launcher has %d notices, want one", n)
	}
	x := d.esc.get(worker.ID)
	if x == nil || x.Source != NoticeSilentStop || x.Count != 2 || !strings.Contains(x.Text, "STUCK") {
		t.Fatalf("escalation %+v, want a silent stop two steps in", x)
	}

	prompt(t, d, worker.ID)
	if err := d.watchWorkers(stopped.Add(4 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if x := d.esc.get(worker.ID); x != nil {
		t.Fatalf("a card that moved is still escalated: %+v", x)
	}
}

// F7. One tool call running too long: the launcher is told once, the board
// escalates, and nothing is killed.
func TestTheWatchdogReportsAStuckTool(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	began := time.Now().Add(-time.Hour)
	d.act.now = func() time.Time { return began }
	d.act.set(worker.ID, ActivityTool, "Bash")

	at := began.Add(LongToolAfter + 5*time.Minute)
	for i := 0; i < 3; i++ {
		if err := d.watchWorkers(at); err != nil {
			t.Fatal(err)
		}
	}
	msgs := pendingFrom(t, d, launcher.ID)
	if len(msgs) != 1 || !strings.Contains(msgs[0].Text, "Bash") {
		t.Fatalf("launcher has %v, want one long-tool notice", msgs)
	}
	x := d.esc.get(worker.ID)
	if x == nil || x.Source != NoticeLongTool || x.Count != escalationStep(5*time.Minute) {
		t.Fatalf("escalation %+v", x)
	}
}

// A card nobody launched is not watched.
func TestTheWatchdogLeavesHumanCardsAlone(t *testing.T) {
	d := testDaemon(t)
	mine := peerCard(t, d, "mine")
	prompt(t, d, mine.ID)
	d.turnEnded(mine.ID)
	if err := d.watchWorkers(time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if x := d.esc.get(mine.ID); x != nil {
		t.Fatalf("a human card was escalated: %+v", x)
	}
}

// F15. A message to a card with no way to drain its queue says so.
func TestAMessageNothingWillDeliverSaysSo(t *testing.T) {
	d := testDaemon(t)
	gem, _, err := d.st.Register(store.Observed{WireName: "gem", Worktree: "/tmp/gem", Runner: "gemini"})
	if err != nil {
		t.Fatal(err)
	}
	claude := peerCard(t, d, "quiet")
	hooked := peerCard(t, d, "hooked")
	if err := d.st.SawHook(hooked.ID, store.HookStop); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		id, delivered string
		warned        bool
	}{
		{gem.ID, "undeliverable", true},
		{claude.ID, "queued-unconfirmed", true},
		{hooked.ID, "queued", false},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/tasks/"+c.id+"/message",
			strings.NewReader(`{"text":"hello","from":"someone"}`))
		req.SetPathValue("id", c.id)
		d.handleMessage(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		if out["delivered"] != c.delivered {
			t.Errorf("%s: delivered %v, want %s", c.id, out["delivered"], c.delivered)
		}
		if w, _ := out["warning"].(string); (w != "") != c.warned {
			t.Errorf("%s: warning %q", c.id, w)
		}
		if n := len(pendingFrom(t, d, c.id)); n != 1 {
			t.Errorf("%s: %d queued, want the message kept", c.id, n)
		}
	}
}

// F15 on the tell door too.
func TestATellNothingWillDeliverSaysSo(t *testing.T) {
	d := testDaemon(t)
	peerCard(t, d, "alice")
	if _, _, err := d.st.Register(store.Observed{WireName: "gem", Worktree: "/tmp/gem", Runner: "gemini"}); err != nil {
		t.Fatal(err)
	}
	out, code := tell(t, d, "alice", "gem", "hello")
	if code != http.StatusOK || out["reachable"] != ReachNo || !strings.Contains(out["note"].(string), "no way to receive") {
		t.Fatalf("tell answered %d: %v", code, out)
	}
}

// An agent-launched claude session gets the Stop hook in front of its
// arguments. A human's session and a runner that is not claude do not.
func TestOnlyAnAgentLaunchedClaudeGetsTheStopHook(t *testing.T) {
	was := stopHookCommand
	stopHookCommand = func() string { return "C:/bin/atrium.exe turn --event end" }
	t.Cleanup(func() { stopHookCommand = was })
	claude := &store.Harness{ID: "claude", Cmd: "claude"}
	args := []string{"--mcp-config", "x.json"}

	got := withStopHook(claude, args, true)
	if len(got) != 4 || got[0] != "--settings" || !strings.Contains(got[1], `"Stop"`) ||
		!strings.Contains(got[1], "turn --event end") || got[2] != "--mcp-config" {
		t.Fatalf("agent claude launch got %q", got)
	}
	if got := withStopHook(claude, args, false); len(got) != 2 {
		t.Fatalf("a human's launch got %q", got)
	}
	if got := withStopHook(&store.Harness{ID: "gemini", Cmd: "gemini"}, args, true); len(got) != 2 {
		t.Fatalf("a gemini launch got %q", got)
	}
	stopHookCommand = func() string { return "" }
	if got := withStopHook(claude, args, true); len(got) != 2 {
		t.Fatalf("with the operator's own Stop hook installed it still added one: %q", got)
	}
}

// The board's own dialog records the human as the launcher. An agent launch
// that lost its sender stays unnamed.
func TestTheBoardDialogIsTheHuman(t *testing.T) {
	for _, c := range []struct {
		req  LaunchRequest
		want string
	}{
		{LaunchRequest{}, store.HumanLauncher},
		{LaunchRequest{Tags: []string{OriginAgentTag}}, ""},
		{LaunchRequest{Tags: []string{OriginAgentTag}, SpawnedBy: "orchestrator"}, "orchestrator"},
	} {
		if got := launcherFor(c.req); got != c.want {
			t.Errorf("%+v: launcher %q, want %q", c.req, got, c.want)
		}
	}
}

// F8. A launch records who asked for it, and resolves the parent's card.
func TestALaunchRecordsItsLauncher(t *testing.T) {
	d := testDaemon(t)
	parent := peerCard(t, d, "orchestrator")
	h := slowHarness(t, d)
	task, err := d.Launch(LaunchRequest{Harness: h, Cwd: t.TempDir(),
		Tags: []string{OriginAgentTag}, SpawnedBy: "orchestrator"})
	if err != nil {
		t.Skipf("could not spawn a test runner on this machine: %v", err)
	}
	t.Cleanup(func() { _ = d.StopRunner(task.ID) })
	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SpawnedBy != d.st.Qualify("orchestrator") || got.SpawnedByID != parent.ID {
		t.Fatalf("lineage %q / %q, want the orchestrator's handle and card", got.SpawnedBy, got.SpawnedByID)
	}
	if !agentLaunched(got) || d.launcherOf(got) == nil || d.launcherOf(got).ID != parent.ID {
		t.Fatal("the worker cannot find its launcher")
	}
}
