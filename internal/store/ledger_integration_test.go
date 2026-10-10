//go:build integration

package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ledgerPair is a launcher card and a worker it launched, with the worker's
// work item made the way a launch makes it.
func ledgerPair(t *testing.T, s *Store) (launcher, worker *Task, w *WorkItem) {
	t.Helper()
	launcher = mustRegister(t, s, "orchestrator")
	worker = mustRegister(t, s, "worker")
	if err := s.SetTags(worker.ID, []string{"origin:agent"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLineage(worker.ID, "orchestrator", launcher.ID); err != nil {
		t.Fatal(err)
	}
	worker = mustGet(t, s, worker.ID)
	made, err := s.CreateWorkItem(worker, NewWorkItem{Brief: "build stage 1", BriefPath: "/w/BRIEF.md"})
	if err != nil || !made {
		t.Fatalf("CreateWorkItem made=%v err=%v", made, err)
	}
	return launcher, worker, mustItem(t, s, worker.ID)
}

func mustRegister(t *testing.T, s *Store, name string) *Task {
	t.Helper()
	task, _, err := s.Register(Observed{WireName: name, Worktree: "/tmp/" + name, Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func mustGet(t *testing.T, s *Store, id string) *Task {
	t.Helper()
	task, err := s.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func mustItem(t *testing.T, s *Store, id string) *WorkItem {
	t.Helper()
	w, err := s.WorkItem(id)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func logOf(t *testing.T, s *Store, id string) []*WorkLogEntry {
	t.Helper()
	got, err := s.WorkLog(id, 0)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func countKind(entries []*WorkLogEntry, kind string) int {
	n := 0
	for _, e := range entries {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

func report(t *testing.T, s *Store, taskID, status, summary string) *ReportResult {
	t.Helper()
	res, err := s.RecordReport(ReportWrite{
		TaskID: taskID, Recap: summary, ReportStatus: status,
		Event: map[string]any{"by": "agent", "kind": "report", "report": status},
	})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func exit(t *testing.T, s *Store, taskID string, extra map[string]any) {
	t.Helper()
	payload := map[string]any{"by": "reaper", "detected": "process is gone"}
	for k, v := range extra {
		payload[k] = v
	}
	if err := s.AppendEvent(taskID, EventExited, payload); err != nil {
		t.Fatal(err)
	}
}

func launchedWith(t *testing.T, s *Store, taskID string, payload map[string]any) {
	t.Helper()
	if err := s.AppendEvent(taskID, EventLaunched, payload); err != nil {
		t.Fatal(err)
	}
}

func launched(t *testing.T, s *Store, taskID string) {
	t.Helper()
	if err := s.AppendEvent(taskID, EventLaunched, map[string]any{"by": "session hook"}); err != nil {
		t.Fatal(err)
	}
}

// Only a card another session launched gets an item, and only one.
func TestOnlyALaunchedCardGetsAWorkItem(t *testing.T) {
	s := openTestStore(t)
	mine := mustRegister(t, s, "mine")
	if made, err := s.CreateWorkItem(mine, NewWorkItem{}); err != nil || made {
		t.Fatalf("a session nobody launched got an item: made=%v err=%v", made, err)
	}
	board := mustRegister(t, s, "board")
	if err := s.SetLineage(board.ID, HumanLauncher, ""); err != nil {
		t.Fatal(err)
	}
	if made, _ := s.CreateWorkItem(mustGet(t, s, board.ID), NewWorkItem{}); made {
		t.Fatal("a card the board's dialog launched got an item nobody asked for")
	}

	_, worker, w := ledgerPair(t, s)
	if w.State != WorkOpen || w.Generation != 1 || w.EndedGeneration != 0 || w.Revision != 1 {
		t.Fatalf("new item = %+v", w)
	}
	if w.LauncherHandle == "" || w.ArbiterID != w.LauncherID || w.Brief != "build stage 1" {
		t.Fatalf("new item did not copy the launch: %+v", w)
	}
	if made, _ := s.CreateWorkItem(worker, NewWorkItem{Brief: "again"}); made {
		t.Fatal("a second launch onto the card replaced its item")
	}
}

// A done report hands the work over. It never closes it.
func TestADoneReportMovesWorkToReportedAndOnlyDoneDoes(t *testing.T) {
	s := openTestStore(t)
	_, worker, _ := ledgerPair(t, s)

	for _, st := range []string{"progress", "blocked", "question"} {
		report(t, s, worker.ID, st, "still going: "+st)
		if w := mustItem(t, s, worker.ID); w.State != WorkOpen {
			t.Fatalf("a %s report moved the work to %s", st, w.State)
		}
	}
	res := report(t, s, worker.ID, "done", "stage 1 landed")
	if res.Item == nil || res.Item.State != WorkReported {
		t.Fatalf("done report left the item at %+v", res.Item)
	}
	w := mustItem(t, s, worker.ID)
	if w.Revision != 2 || w.LatestReportID == "" || w.StateBy != ByWorker {
		t.Fatalf("reported item = %+v", w)
	}
	first := w.LatestReportID

	// A second done replaces the first and moves the revision, so a verdict
	// read at the old one would be stale.
	report(t, s, worker.ID, "done", "stage 1 landed, and the test")
	w = mustItem(t, s, worker.ID)
	if w.State != WorkReported || w.Revision != 3 || w.LatestReportID == first {
		t.Fatalf("second done did not replace the first: %+v", w)
	}
	if n := countKind(logOf(t, s, worker.ID), LogReport); n != 5 {
		t.Fatalf("log holds %d reports, want every one of the 5", n)
	}
}

// A retried report is one row.
func TestARetriedReportIsOneRow(t *testing.T) {
	s := openTestStore(t)
	_, worker, _ := ledgerPair(t, s)
	report(t, s, worker.ID, "done", "the same words")
	res := report(t, s, worker.ID, "done", "the same words")
	if !res.Duplicate {
		t.Fatal("the retry was not recognised")
	}
	if n := countKind(logOf(t, s, worker.ID), LogReport); n != 1 {
		t.Fatalf("a retried report wrote %d rows", n)
	}
	if w := mustItem(t, s, worker.ID); w.Revision != 2 {
		t.Fatalf("a retried report moved the revision to %d", w.Revision)
	}
}

// The sa19 case: the session dies mid-task. The item is flagged in the exit's
// transaction and the launcher gets one durable notice naming the last report.
func TestASessionEndingWithoutAReportIsFlaggedOnce(t *testing.T) {
	s := openTestStore(t)
	launcher, worker, _ := ledgerPair(t, s)
	var notices []LedgerNotice
	s.OnLedgerNotice = func(n LedgerNotice) { notices = append(notices, n) }

	report(t, s, worker.ID, "progress", "feature list committed, starting go-vs-c")
	exit(t, s, worker.ID, nil)

	w := mustItem(t, s, worker.ID)
	if w.State != WorkEnded || w.EndedGeneration != 1 || w.StateBy != ByAtrium {
		t.Fatalf("after the exit the item is %+v", w)
	}
	msgs, err := s.PendingMessages(launcher.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || len(notices) != 1 {
		t.Fatalf("launcher has %d queued, %d handed to the daemon, want one each", len(msgs), len(notices))
	}
	for _, want := range []string{"ended without a final report", "process is gone",
		"progress", "feature list committed", "outputs: none", worker.ID} {
		if !strings.Contains(msgs[0].Text, want) {
			t.Fatalf("notice %q does not say %q", msgs[0].Text, want)
		}
	}
	if msgs[0].FromPeer != worker.WireName || notices[0].MessageID != msgs[0].ID {
		t.Fatalf("notice from %q, handed over as %+v", msgs[0].FromPeer, notices[0])
	}

	// The supervisor, the session hook and the reaper all notice the same
	// death. One row, one notice.
	exit(t, s, worker.ID, map[string]any{"by": "supervisor"})
	exit(t, s, worker.ID, map[string]any{"by": "session hook"})
	if msgs, _ := s.PendingMessages(launcher.ID); len(msgs) != 1 {
		t.Fatalf("one death sent %d notices", len(msgs))
	}
	if n := countKind(logOf(t, s, worker.ID), LogAtrium); n != 1 {
		t.Fatalf("one death wrote %d atrium rows", n)
	}
}

// Resumed by somebody, the work is open again. A late exit from the old run
// changes nothing, and nothing resumes by itself.
func TestRunningAgainReopensAndALateExitIsIgnored(t *testing.T) {
	s := openTestStore(t)
	_, worker, _ := ledgerPair(t, s)

	// A second launched event in the same run is the run announcing itself
	// twice, not a new generation.
	launched(t, s, worker.ID)
	if w := mustItem(t, s, worker.ID); w.Generation != 1 {
		t.Fatalf("a repeat start moved the generation to %d", w.Generation)
	}
	exit(t, s, worker.ID, nil)
	// A bare restart is not new work: the item stays ended. Only a prompted launch reopens it.
	launched(t, s, worker.ID)
	w := mustItem(t, s, worker.ID)
	if w.State != WorkEnded || w.Generation != 2 || w.EndedGeneration != 1 {
		t.Fatalf("running again left %+v", w)
	}
	exit(t, s, worker.ID, map[string]any{"generation": 2})
	launchedWith(t, s, worker.ID, map[string]any{"by": "launch", "prompted": true})
	w = mustItem(t, s, worker.ID)
	if w.State != WorkOpen || w.Generation != 3 || w.EndedGeneration != 2 {
		t.Fatalf("a prompted launch left %+v", w)
	}
	exit(t, s, worker.ID, map[string]any{"generation": 1})
	w = mustItem(t, s, worker.ID)
	if w.State != WorkOpen || w.EndedGeneration != 2 {
		t.Fatalf("a late exit from generation 1 moved the item: %+v", w)
	}
	last := logOf(t, s, worker.ID)
	if !strings.Contains(last[len(last)-1].Text, "late exit") {
		t.Fatalf("the late exit was not logged: %+v", last[len(last)-1])
	}
	// The current run ending does move it.
	exit(t, s, worker.ID, map[string]any{"generation": 3})
	if w := mustItem(t, s, worker.ID); w.State != WorkEnded || w.EndedGeneration != 3 {
		t.Fatalf("the current run ending left %+v", w)
	}
}

// A session that ends after its done report leaves the work reported: the
// report is on record and the arbiter can still judge it.
func TestAnExitAfterADoneReportLeavesItReported(t *testing.T) {
	s := openTestStore(t)
	launcher, worker, _ := ledgerPair(t, s)
	report(t, s, worker.ID, "done", "finished")
	before := mustItem(t, s, worker.ID)
	exit(t, s, worker.ID, nil)
	w := mustItem(t, s, worker.ID)
	if w.State != WorkReported || w.EndedGeneration != 1 {
		t.Fatalf("an exit after done left %+v", w)
	}
	if w.Revision != before.Revision {
		t.Fatalf("an exit after done moved the revision %d to %d, a verdict on the report would be refused",
			before.Revision, w.Revision)
	}
	if n := countKind(logOf(t, s, worker.ID), LogAtrium); n != 1 {
		t.Fatalf("want one row saying the session ended, got %d", n)
	}
	if msgs, _ := s.PendingMessages(launcher.ID); len(msgs) != 0 {
		t.Fatalf("an exit after done sent an ended notice: %+v", msgs)
	}
}

// A done report that arrives after the exit is recorded is still the stronger
// fact. A report on closed work changes nothing.
func TestALateDoneWinsAndClosedIsClosed(t *testing.T) {
	s := openTestStore(t)
	_, worker, _ := ledgerPair(t, s)
	exit(t, s, worker.ID, nil)
	report(t, s, worker.ID, "done", "it was in flight")
	w := mustItem(t, s, worker.ID)
	if w.State != WorkReported {
		t.Fatalf("a late done left %s", w.State)
	}
	entries := logOf(t, s, worker.ID)
	if got := entries[len(entries)-1]; got.Note != "after the session ended" {
		t.Fatalf("late done note = %q", got.Note)
	}

	// Stage 2 writes verdicts. Closing by hand here stands in for one.
	if _, err := s.db.Exec(`UPDATE work_item SET state = ? WHERE task_id = ?`, WorkAccepted, worker.ID); err != nil {
		t.Fatal(err)
	}
	report(t, s, worker.ID, "done", "one more thing")
	if w := mustItem(t, s, worker.ID); w.State != WorkAccepted {
		t.Fatalf("a report reopened closed work: %s", w.State)
	}
	entries = logOf(t, s, worker.ID)
	if got := entries[len(entries)-1]; got.Note != "after close" {
		t.Fatalf("report on closed work note = %q", got.Note)
	}
	exit(t, s, worker.ID, map[string]any{"generation": 1})
	if w := mustItem(t, s, worker.ID); w.State != WorkAccepted {
		t.Fatalf("an exit moved closed work to %s", w.State)
	}
}

// Messages between a worker and its launcher are logged verbatim on the
// worker's item. Anybody else's are not.
func TestMessagesBetweenWorkerAndLauncherAreLogged(t *testing.T) {
	s := openTestStore(t)
	launcher, worker, _ := ledgerPair(t, s)
	other := mustRegister(t, s, "bystander")

	if ok, err := s.LogWorkMessage(worker.ID, launcher.ID, worker.WireName, "halfway there"); err != nil || !ok {
		t.Fatalf("worker to launcher: ok=%v err=%v", ok, err)
	}
	if ok, _ := s.LogWorkMessage(launcher.ID, worker.ID, launcher.WireName, "also do the docs"); !ok {
		t.Fatal("launcher to worker was not logged")
	}
	if ok, _ := s.LogWorkMessage(other.ID, worker.ID, other.WireName, "hello"); ok {
		t.Fatal("a bystander's message went on the worker's item")
	}
	entries := logOf(t, s, worker.ID)
	if countKind(entries, LogSay) != 1 || countKind(entries, LogInstruction) != 1 {
		t.Fatalf("log = %+v", entries)
	}
	if w := mustItem(t, s, worker.ID); w.State != WorkOpen {
		t.Fatalf("a message moved the work to %s", w.State)
	}
}

// The log is capped. Chatter goes first, and the latest done report is never
// taken, however much is said after it.
func TestTheLogIsBoundedAndKeepsTheLatestReport(t *testing.T) {
	s := openTestStore(t)
	launcher, worker, _ := ledgerPair(t, s)
	report(t, s, worker.ID, "done", "the report that matters")
	latest := mustItem(t, s, worker.ID).LatestReportID
	for i := 0; i < 20; i++ {
		report(t, s, worker.ID, "progress", "progress "+string(rune('a'+i)))
	}
	for i := 0; i < 500; i++ {
		if _, err := s.LogWorkMessage(worker.ID, launcher.ID, worker.WireName, "chatter"); err != nil {
			t.Fatal(err)
		}
	}
	entries := logOf(t, s, worker.ID)
	if len(entries) > MaxWorkLogRows {
		t.Fatalf("log holds %d rows, cap is %d", len(entries), MaxWorkLogRows)
	}
	if countKind(entries, LogReport) != 21 {
		t.Fatalf("reports were trimmed before chatter: %d left", countKind(entries, LogReport))
	}
	found := false
	for _, e := range entries {
		found = found || e.ID == latest
	}
	if !found {
		t.Fatal("the latest done report was trimmed")
	}

	// And by bytes: long progress reports push the log over a megabyte, and
	// the done report still stays.
	// Unique at the front, since the text is cut at MaxWorkText and identical
	// reports are one row.
	long := strings.Repeat("x", MaxWorkText)
	for i := 0; i < 200; i++ {
		report(t, s, worker.ID, "progress", time.Duration(i).String()+long)
	}
	var reports int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM work_log WHERE task_id = ? AND kind = 'report'`,
		worker.ID).Scan(&reports); err != nil {
		t.Fatal(err)
	}
	if reports >= 200 || reports < 100 {
		t.Fatalf("%d reports left, want the byte cap to have trimmed the oldest", reports)
	}
	var bytes int
	if err := s.db.QueryRow(`SELECT SUM(LENGTH(text) + LENGTH(outputs)) FROM work_log WHERE task_id = ?`,
		worker.ID).Scan(&bytes); err != nil {
		t.Fatal(err)
	}
	if bytes > MaxWorkLogBytes {
		t.Fatalf("log holds %d bytes, cap is %d", bytes, MaxWorkLogBytes)
	}
	entries = logOf(t, s, worker.ID)
	found = false
	for _, e := range entries {
		found = found || e.ID == latest
		if len(e.Text) > MaxWorkText {
			t.Fatalf("a row holds %d bytes of text", len(e.Text))
		}
	}
	if !found {
		t.Fatal("the byte cap trimmed the latest done report")
	}
}

// A failure between the item moving and its notice rolls both back, with the
// exit event. The next exit seen for that death does the whole move.
func TestACrashBetweenTheMoveAndTheNoticeLosesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "atrium.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	launcher, worker, _ := ledgerPair(t, s)
	failpointHook = func(step string) error {
		if step == "ended-notice" {
			return errors.New("disk fell off")
		}
		return nil
	}
	defer func() { failpointHook = nil }()
	if err := s.AppendEvent(worker.ID, EventExited, map[string]any{"by": "reaper"}); err == nil {
		t.Fatal("the failed change reported success")
	}
	s.Close()
	failpointHook = nil

	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if w := mustItem(t, s, worker.ID); w.State != WorkOpen || w.EndedGeneration != 0 {
		t.Fatalf("half a change survived: %+v", w)
	}
	var exits int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM event WHERE task_id = ? AND kind = ?`,
		worker.ID, EventExited).Scan(&exits); err != nil {
		t.Fatal(err)
	}
	if exits != 0 {
		t.Fatal("the exit event survived a change that rolled back")
	}
	if msgs, _ := s.PendingMessages(launcher.ID); len(msgs) != 0 {
		t.Fatal("the notice survived a change that rolled back")
	}
	exit(t, s, worker.ID, nil)
	if w := mustItem(t, s, worker.ID); w.State != WorkEnded {
		t.Fatalf("the retry left %s", w.State)
	}
	if msgs, _ := s.PendingMessages(launcher.ID); len(msgs) != 1 {
		t.Fatalf("the retry queued %d notices", len(msgs))
	}
}

// A report is one transaction: a failure between any two of its writes leaves
// none of them.
func TestAReportFailingBetweenWritesLeavesNothing(t *testing.T) {
	for _, step := range []string{"recap", "sha", "event", "status", "reported", "ledger"} {
		t.Run(step, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "atrium.db")
			s, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			launcher, worker, _ := ledgerPair(t, s)
			failpointHook = func(at string) error {
				if at == step {
					return errors.New("failed at " + at)
				}
				return nil
			}
			_, err = s.RecordReport(ReportWrite{
				TaskID: worker.ID, Recap: "all done", SetSHA: true, SHA: "abc1234",
				Event:      map[string]any{"by": "agent", "kind": "finished", "status": "done"},
				MoveStatus: true, Status: StatusDone, ReportStatus: "done",
				Notice: &NoticeSpec{ToID: launcher.ID, From: worker.WireName, Source: "report",
					Key: "k", Text: "report from worker"},
			})
			failpointHook = nil
			if err == nil {
				t.Fatal("the failed report reported success")
			}
			s.Close()

			s, err = Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			got := mustGet(t, s, worker.ID)
			if got.Recap != "" || got.ReportSHA != "" || got.Status == StatusDone || got.ReportedAt != nil {
				t.Fatalf("half a report survived: recap=%q sha=%q status=%s reported=%v",
					got.Recap, got.ReportSHA, got.Status, got.ReportedAt)
			}
			if w := mustItem(t, s, worker.ID); w.State != WorkOpen || len(logOf(t, s, worker.ID)) != 0 {
				t.Fatalf("the ledger kept half a report: %+v", w)
			}
			if msgs, _ := s.PendingMessages(launcher.ID); len(msgs) != 0 {
				t.Fatal("the notice survived")
			}
		})
	}
}

// The snapshot file lists what is not closed, the crash first, and the
// read-only reader prints the same list.
func TestTheSnapshotFileAndTheReadOnlyReaderAgree(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "atrium.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_, worker, _ := ledgerPair(t, s)
	second := mustRegister(t, s, "second")
	if err := s.SetTags(second.ID, []string{"origin:agent"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLineage(second.ID, "orchestrator", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateWorkItem(mustGet(t, s, second.ID), NewWorkItem{}); err != nil {
		t.Fatal(err)
	}
	report(t, s, second.ID, "done", "second is done\nwith detail")
	exit(t, s, worker.ID, nil)

	file := LedgerFilePath(path)
	if err := s.WriteLedgerFile(file); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	ended := strings.Index(text, "Ended without a report")
	reported := strings.Index(text, "Reported, waiting on a verdict")
	if ended < 0 || reported < 0 || ended > reported {
		t.Fatalf("the crash case is not listed first:\n%s", text)
	}
	if !strings.Contains(text, `"second is done"`) || !strings.Contains(text, worker.ID) {
		t.Fatalf("the file is missing an item:\n%s", text)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, ".work-ledger-*"))
	if len(matches) != 0 {
		t.Fatalf("temporary files left behind: %v", matches)
	}

	// The daemon down: the reader opens the database read only.
	s.Close()
	v, err := ReadLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Open) != 2 || v.Open[0].State != WorkEnded || v.Open[1].LastReport == nil {
		t.Fatalf("read-only ledger = %+v", v.Open)
	}
	if strings.Contains(RenderLedger(v), worker.ID) != true {
		t.Fatal("the reader rendered a different list")
	}
}

// Reading a database with no ledger yet is an empty list, not an error.
func TestReadLedgerOnADatabaseWithNoLedger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "atrium.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`DROP TABLE work_item`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	v, err := ReadLedger(path)
	if err != nil || len(v.Open) != 0 {
		t.Fatalf("v=%+v err=%v", v, err)
	}
}

// A card whose item is open is not pruned. A card whose item is closed is,
// and the item outlives it.
func TestPruneKeepsCardsWithOpenWorkAndItemsOutliveCards(t *testing.T) {
	s := openTestStore(t)
	_, worker, _ := ledgerPair(t, s)
	report(t, s, worker.ID, "done", "done")
	if err := s.SetStatus(worker.ID, StatusDone); err != nil {
		t.Fatal(err)
	}
	if n, err := s.Prune(0); err != nil || n != 0 {
		t.Fatalf("pruned %d cards with reported work, err=%v", n, err)
	}
	if _, err := s.db.Exec(`UPDATE work_item SET state = ? WHERE task_id = ?`, WorkAccepted, worker.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := s.Prune(0); err != nil || n != 1 {
		t.Fatalf("pruned %d, err=%v, want the card with accepted work gone", n, err)
	}
	w := mustItem(t, s, worker.ID)
	if w.State != WorkAccepted || w.Handle != worker.WireName || len(logOf(t, s, worker.ID)) == 0 {
		t.Fatalf("the item did not outlive its card: %+v", w)
	}
}

// The backfill makes inferred items for the last two weeks of launched cards,
// once, and reads each card's own reports.
func TestTheBackfillIsInferredBoundedAndRunsOnce(t *testing.T) {
	s := openTestStore(t)
	launcher := mustRegister(t, s, "orchestrator")
	mk := func(name string, age time.Duration) *Task {
		c := mustRegister(t, s, name)
		if err := s.SetTags(c.ID, []string{"origin:agent"}); err != nil {
			t.Fatal(err)
		}
		if err := s.SetLineage(c.ID, "orchestrator", launcher.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.Exec(`UPDATE task SET created_at = ? WHERE id = ?`, ts(now().Add(-age)), c.ID); err != nil {
			t.Fatal(err)
		}
		return c
	}
	reported := mk("reported", time.Hour)
	if err := s.AppendEvent(reported.ID, EventSubmitted, map[string]any{
		"by": "agent", "kind": "finished", "status": "done", "sha": "abc1234"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetReportSHA(reported.ID, "abc1234", false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRecap(reported.ID, "did the thing"); err != nil {
		t.Fatal(err)
	}
	crashed := mk("crashed", 2*time.Hour)
	if err := s.AppendEvent(crashed.ID, EventSubmitted, map[string]any{
		"by": "agent", "kind": "report", "report": "progress"}); err != nil {
		t.Fatal(err)
	}
	live := mk("live", 3*time.Hour)
	unknown := mk("unknown", 4*time.Hour)
	old := mk("old", 20*24*time.Hour)
	human := mustRegister(t, s, "human")
	if err := s.SetLineage(human.ID, HumanLauncher, ""); err != nil {
		t.Fatal(err)
	}

	liveness := func(c *Task) Liveness {
		switch c.ID {
		case crashed.ID, reported.ID:
			return Gone
		case live.ID:
			return Live
		}
		return LiveUnknown
	}
	res, err := s.BackfillWorkLedger(liveness)
	if err != nil {
		t.Fatal(err)
	}
	if res.Items != 4 || res.Reported != 1 || res.Ended != 1 || res.Open != 2 || res.Unknown != 1 {
		t.Fatalf("backfill = %+v", res)
	}
	w := mustItem(t, s, reported.ID)
	if w.State != WorkReported || !w.Inferred || len(w.Outputs.Commits) != 1 || w.LatestReportID == "" {
		t.Fatalf("reported card = %+v", w)
	}
	entries := logOf(t, s, reported.ID)
	recapRow := false
	for _, e := range entries {
		recapRow = recapRow || (e.Note == "recap at backfill" && e.Kind == LogAtrium)
	}
	if !recapRow {
		t.Fatalf("the recap was not logged as a recap: %+v", entries)
	}
	if w := mustItem(t, s, crashed.ID); w.State != WorkEnded || w.Running() {
		t.Fatalf("crashed card = %+v", w)
	}
	if w := mustItem(t, s, live.ID); w.State != WorkOpen || !w.Running() {
		t.Fatalf("live card = %+v", w)
	}
	if w := mustItem(t, s, unknown.ID); w.State != WorkOpen {
		t.Fatalf("unknown card = %+v", w)
	}
	for _, id := range []string{old.ID, human.ID, launcher.ID} {
		if _, err := s.WorkItem(id); err == nil {
			t.Fatalf("card %s should not be on the ledger", id)
		}
	}
	again, err := s.BackfillWorkLedger(liveness)
	if err != nil || !again.Skipped {
		t.Fatalf("second backfill = %+v err=%v", again, err)
	}
}

// say is a worker's message to its launcher, or the launcher's to the worker, the way the doors log it.
func say(t *testing.T, s *Store, from, to *Task, text string) {
	t.Helper()
	if _, err := s.LogWorkMessage(from.ID, to.ID, from.WireName, text); err != nil {
		t.Fatal(err)
	}
}

func queued(t *testing.T, s *Store, id string) int {
	t.Helper()
	msgs, err := s.PendingMessages(id)
	if err != nil {
		t.Fatal(err)
	}
	return len(msgs)
}

// A done or blocked say to the launcher is the report, and an end after it sends no notice.
func TestADoneOrBlockedSayIsTheReport(t *testing.T) {
	cases := []struct {
		name, text, state string
	}{
		{"done with a sha", "done abc1234", WorkReported},
		{"blocked", "blocked: no network", WorkReported},
		{"capitalised", "Done 9f3c", WorkReported},
		{"not at the start", "I am done with the first half", WorkOpen},
		{"a word that starts with done", "doneness check", WorkOpen},
		{"chatter", "working on it", WorkOpen},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := openTestStore(t)
			launcher, worker, _ := ledgerPair(t, s)
			say(t, s, worker, launcher, c.text)
			if w := mustItem(t, s, worker.ID); w.State != c.state {
				t.Fatalf("after %q the item is %q, want %q", c.text, w.State, c.state)
			}
			exit(t, s, worker.ID, map[string]any{"cause": CauseAsked, "cause_by": launcher.WireName})
			n := queued(t, s, launcher.ID)
			if c.state == WorkReported && n != 0 {
				t.Fatalf("a reported worker's end queued %d notices", n)
			}
			if c.state == WorkReported {
				last, err := lastReportOn(s.db, worker.ID)
				if err != nil || last == nil || last.Text != c.text {
					t.Fatalf("the say is not the report: %+v %v", last, err)
				}
			}
		})
	}
}

// An end atrium caused is logged and queues nothing; the work stays open.
func TestAnEndAtriumCausedSendsNoNotice(t *testing.T) {
	cases := []struct {
		name    string
		payload map[string]any
		want    string
	}{
		{"asked by the launcher", map[string]any{"cause": CauseAsked, "cause_by": "orchestrator"}, "asked to exit by orchestrator"},
		{"asked by the operator", map[string]any{"cause": CauseAsked}, "asked to exit by the operator"},
		{"room shutdown", map[string]any{"cause": CauseShutdown}, "the room shut down"},
		{"idle park", map[string]any{"cause": CauseIdlePark}, "parked after sitting idle"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := openTestStore(t)
			launcher, worker, _ := ledgerPair(t, s)
			exit(t, s, worker.ID, c.payload)
			if n := queued(t, s, launcher.ID); n != 0 {
				t.Fatalf("an expected end queued %d notices", n)
			}
			if w := mustItem(t, s, worker.ID); w.State != WorkOpen || w.EndedGeneration != 1 {
				t.Fatalf("an expected end left %+v", w)
			}
			log := logOf(t, s, worker.ID)
			if got := log[len(log)-1].Text; !strings.Contains(got, "the session ended ("+c.want+")") {
				t.Fatalf("log line %q does not say %q", got, c.want)
			}
		})
	}
}

// The room reopening a card leaves the item as it was, whether reported or ended.
func TestAReopenDoesNotResetTheItem(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, s *Store, launcher, worker *Task)
		want  string
	}{
		{"reported", func(t *testing.T, s *Store, l, w *Task) { report(t, s, w.ID, "done", "shipped") }, WorkReported},
		{"ended", func(t *testing.T, s *Store, l, w *Task) {
			exit(t, s, w.ID, nil)
		}, WorkEnded},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := openTestStore(t)
			launcher, worker, _ := ledgerPair(t, s)
			c.setup(t, s, launcher, worker)
			before := queued(t, s, launcher.ID)
			if c.want == WorkReported {
				exit(t, s, worker.ID, map[string]any{"cause": CauseShutdown})
			}
			launchedWith(t, s, worker.ID, map[string]any{"by": "supervisor", "via": "reopen"})
			if w := mustItem(t, s, worker.ID); w.State != c.want {
				t.Fatalf("a reopen turned %s into %s", c.want, w.State)
			}
			exit(t, s, worker.ID, map[string]any{"cause": CauseIdlePark})
			if n := queued(t, s, launcher.ID); n != before {
				t.Fatalf("the park after a reopen queued %d notices", n-before)
			}
		})
	}
}

// A launcher's say with new work reopens an item that ended without a report.
func TestALauncherSayReopensAnEndedItem(t *testing.T) {
	s := openTestStore(t)
	launcher, worker, _ := ledgerPair(t, s)
	exit(t, s, worker.ID, nil)
	say(t, s, launcher, worker, "one more thing: also fix the docs")
	if w := mustItem(t, s, worker.ID); w.State != WorkOpen {
		t.Fatalf("new work left the item %s", w.State)
	}
}

// A session that dies or quits with no report and nobody asking is still worth a turn.
func TestAnUnexpectedEndStillSendsANotice(t *testing.T) {
	s := openTestStore(t)
	launcher, worker, _ := ledgerPair(t, s)
	say(t, s, worker, launcher, "still working on the parser")
	exit(t, s, worker.ID, nil)
	msgs, err := s.PendingMessages(launcher.ID)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("queued %d notices, err %v", len(msgs), err)
	}
	if !strings.Contains(msgs[0].Text, "unexpected") {
		t.Fatalf("the notice does not say why it is unexpected: %q", msgs[0].Text)
	}
	if w := mustItem(t, s, worker.ID); w.State != WorkEnded {
		t.Fatalf("item is %s", w.State)
	}
}
