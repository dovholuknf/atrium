//go:build integration

package daemon

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

func TestParseKindDefaultsToNeedsAndNeverErrors(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"fyi": KindFYI, " FYI ": KindFYI, "Fyi": KindFYI,
		"": KindNeeds, "needs": KindNeeds, "urgent": KindNeeds, "fyi!": KindNeeds, "f y i": KindNeeds,
	} {
		if got := parseKind(in); got != want {
			t.Errorf("parseKind(%q) = %q, want %q", in, got, want)
		}
	}
}

func holdingPairAs(t *testing.T, d *Daemon, tag, suffix string) (launcher, worker *store.Task) {
	t.Helper()
	launcher = peerCard(t, d, "orchestrator-"+suffix)
	worker = peerCard(t, d, "worker-"+suffix)
	if err := d.st.SetTags(worker.ID, []string{"sdk", OriginAgentTag}); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetLineage(worker.ID, launcher.WireName, launcher.ID); err != nil {
		t.Fatal(err)
	}
	prompt(t, d, worker.ID)
	if err := d.st.SetTags(launcher.ID, []string{tag}); err != nil {
		t.Fatal(err)
	}
	worker, err := d.st.Get(worker.ID)
	if err != nil {
		t.Fatal(err)
	}
	if made, err := d.st.CreateWorkItem(worker, store.NewWorkItem{Brief: "hold test"}); err != nil || !made {
		t.Fatalf("work item: %v %v", made, err)
	}
	return launcher, worker
}

func sayKind(t *testing.T, d *Daemon, to *store.Task, from, kind string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/tasks/"+to.ID+"/message",
		strings.NewReader(`{"text":"all quiet","from":"`+from+`","kind":"`+kind+`"}`))
	req.SetPathValue("id", to.ID)
	d.handleMessage(rec, req)
	return rec
}

// A notice a worker on ANOTHER ROOM sends its launcher arrives as an fyi from `handle@room`, a
// sender with no card here. It is never parked: it is queued for the end of the orchestrator's turn.
func TestACrossRoomNoticeToAnOrchestratorIsQueuedForTurnEnd(t *testing.T) {
	t.Parallel()
	for _, tag := range []string{HoldNoticesTag, OrchestratorTag} {
		d := testDaemon(t)
		launcher, _ := holdingPair(t, d, tag)
		rec := sayKind(t, d, launcher, "sa1@claude-sg4", KindFYI)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"delivered":"queued`) ||
			!strings.Contains(rec.Body.String(), `"done"`) || strings.Contains(rec.Body.String(), `"held"`) {
			t.Fatalf("%s: answered %d, want queued with when done: %s", tag, rec.Code, rec.Body)
		}
		msgs := pendingFrom(t, d, launcher.ID)
		if len(msgs) != 1 || !msgs[0].WaitTurn || msgs[0].FromPeer != "sa1@claude-sg4" {
			t.Fatalf("%s: queued = %+v, want the fyi waiting for the turn end", tag, msgs)
		}
		if n := len(heldOn(t, d, launcher.ID)); n != 0 {
			t.Fatalf("%s: %d held, want none", tag, n)
		}
	}
}

func tellKind(t *testing.T, d *Daemon, to *store.Task, from, kind string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	d.handleTell(rec, httptest.NewRequest(http.MethodPost, "/tell", bytes.NewReader([]byte(
		`{"from":"`+from+`","to":"`+to.WireName+`","text":"all quiet","kind":"`+kind+`"}`))))
	return rec
}

func TestAnFYIReportIsQueuedForTurnEndForBothTags(t *testing.T) {
	t.Parallel()
	for _, tag := range []string{HoldNoticesTag, OrchestratorTag} {
		d := testDaemon(t)
		launcher, worker := holdingPair(t, d, tag)

		rec, out := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportProgress, Kind: "fyi",
			Recap: "the matrix is written up"})
		if rec.Code != http.StatusOK || out["launcher_told"] != "queued" {
			t.Fatalf("%s: report answered %d, want launcher_told queued: %s", tag, rec.Code, rec.Body)
		}
		msgs := pendingFrom(t, d, launcher.ID)
		if len(msgs) != 1 || !strings.Contains(msgs[0].Text, "the matrix") || !msgs[0].WaitTurn {
			t.Fatalf("%s: queued = %+v, want the report for the turn end", tag, msgs)
		}
		if n := len(heldOn(t, d, launcher.ID)); n != 0 {
			t.Fatalf("%s: %d held, want none (worker %s)", tag, n, worker.ID)
		}
	}
}

func TestANeedsReportIsTypedAsToday(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	for i, kind := range []string{"", "needs", "gibberish"} {
		launcher, worker := holdingPairAs(t, d, OrchestratorTag, strconv.Itoa(i))
		rec, _ := finishWith(t, d, FinishRequest{Agent: worker.WireName, Status: ReportDone, Kind: kind,
			NoCommit: "research only", Recap: "the matrix is written up"})
		if rec.Code != http.StatusOK {
			t.Fatalf("report answered %d: %s", rec.Code, rec.Body)
		}
		msgs := pendingFrom(t, d, launcher.ID)
		if len(msgs) != 1 || !strings.Contains(msgs[0].Text, "the matrix is written up") {
			t.Fatalf("kind %q: %d queued, want the report", kind, len(msgs))
		}
		if n := len(heldOn(t, d, launcher.ID)); n != 0 {
			t.Fatalf("kind %q: %d held, want none", kind, n)
		}
	}
}

func TestABlockedFYIReportIsStillNeeds(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	launcher, _ := holdingPair(t, d, OrchestratorTag)
	rec, _ := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportBlocked, Kind: "fyi",
		Ask: "the token, from clint"})
	if rec.Code != http.StatusOK {
		t.Fatalf("report answered %d: %s", rec.Code, rec.Body)
	}
	if n := len(pendingFrom(t, d, launcher.ID)); n != 1 {
		t.Fatalf("%d queued, want the blocked report", n)
	}
	if n := len(heldOn(t, d, launcher.ID)); n != 0 {
		t.Fatalf("%d held, want none", n)
	}
}

// A done report waits on acceptance and a merge, and one carrying an ask wants an answer.
// Neither may sit unseen as a held notice, whatever it was labelled.
func TestADoneOrAskingFYIReportIsForcedToNeeds(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	n := 0
	for name, in := range map[string]FinishRequest{
		"done": {Status: ReportDone, Kind: "fyi", NoCommit: "research only",
			Recap: "the matrix is written up"},
		"progress with an ask": {Status: ReportProgress, Kind: "fyi",
			Recap: "the matrix is written up", Ask: "which table, from clint"},
	} {
		for _, tag := range []string{HoldNoticesTag, OrchestratorTag} {
			n++
			launcher, worker := holdingPairAs(t, d, tag, strconv.Itoa(n))
			in.Agent = worker.WireName
			rec, _ := finishWith(t, d, in)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s %s: report answered %d: %s", name, tag, rec.Code, rec.Body)
			}
			for _, h := range heldOn(t, d, launcher.ID) {
				if h["source"] == NoticeFYI {
					t.Fatalf("%s %s: held as an fyi: %v", name, tag, h)
				}
			}
			if n := len(pendingFrom(t, d, launcher.ID)); n != 1 {
				t.Fatalf("%s %s: %d queued, want 1", name, tag, n)
			}
		}
	}
}

func TestAnFYIReportToALauncherThatDoesNotHoldIsTypedAsToday(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	launcher, _ := holdingPair(t, d, "orchestrators")
	finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, Kind: "fyi",
		NoCommit: "research only", Recap: "the matrix is written up"})
	if n := len(pendingFrom(t, d, launcher.ID)); n != 1 {
		t.Fatalf("%d queued, want the report", n)
	}
	if n := len(heldOn(t, d, launcher.ID)); n != 0 {
		t.Fatalf("%d held for a launcher that does not hold", n)
	}
}

func TestAnFYIReportStillCountsAsTheWorkersReport(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	launcher, worker := holdingPair(t, d, OrchestratorTag)
	finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportProgress, Kind: "fyi",
		Recap: "the matrix is written up"})
	got, err := d.st.Get(worker.ID)
	if err != nil || got.ReportedAt == nil {
		t.Fatalf("the worker's report was not recorded: %v %+v", err, got)
	}
	// And a turn that ends after it is not a silent stop.
	stopTurn(t, d, "worker")
	for _, h := range heldOn(t, d, launcher.ID) {
		if h["source"] == NoticeSilentStop {
			t.Fatalf("an fyi report was followed by a silent stop notice: %v", h)
		}
	}
}

func TestAnFYISayIsQueuedForTurnEndForBothTags(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	for i, tag := range []string{HoldNoticesTag, OrchestratorTag} {
		launcher, worker := holdingPairAs(t, d, tag, strconv.Itoa(i))

		rec := sayKind(t, d, launcher, worker.WireName, "fyi")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: say answered %d: %s", tag, rec.Code, rec.Body)
		}
		if !strings.Contains(rec.Body.String(), `"delivered":"queued`) || strings.Contains(rec.Body.String(), `"held"`) {
			t.Fatalf("%s: answered %s, want queued", tag, rec.Body)
		}
		if msgs := pendingFrom(t, d, launcher.ID); len(msgs) != 1 || msgs[0].Text != "all quiet" || !msgs[0].WaitTurn {
			t.Fatalf("%s: queued = %+v, want the fyi for the turn end", tag, msgs)
		}
		if n := len(heldOn(t, d, launcher.ID)); n != 0 {
			t.Fatalf("%s: %d held, want none", tag, n)
		}
		// A say to the launcher is the worker's report.
		if got, _ := d.st.Get(worker.ID); got.ReportedAt == nil {
			t.Fatalf("%s: the fyi say did not count as the worker's report", tag)
		}
	}
}

func TestAnFYITellAndANeedsTellAreBothQueued(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	launcher, worker := holdingPair(t, d, OrchestratorTag)

	if rec := tellKind(t, d, launcher, worker.WireName, "fyi"); rec.Code != http.StatusOK {
		t.Fatalf("tell answered %d: %s", rec.Code, rec.Body)
	}
	if n := len(pendingFrom(t, d, launcher.ID)); n != 1 {
		t.Fatalf("%d queued for an fyi tell, want 1", n)
	}
	if n := len(heldOn(t, d, launcher.ID)); n != 0 {
		t.Fatalf("%d held, want none", n)
	}

	if rec := tellKind(t, d, launcher, worker.WireName, "needs"); rec.Code != http.StatusOK {
		t.Fatalf("tell answered %d: %s", rec.Code, rec.Body)
	}
	if n := len(pendingFrom(t, d, launcher.ID)); n != 2 {
		t.Fatalf("%d queued after a needs tell, want 2", n)
	}
}

func TestANeedsSayAndAnFYIToANonHolderAreQueuedAsToday(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	launcher, worker := holdingPair(t, d, OrchestratorTag)
	sayKind(t, d, launcher, worker.WireName, "needs")
	sayKind(t, d, launcher, worker.WireName, "")
	if n := len(pendingFrom(t, d, launcher.ID)); n != 2 {
		t.Fatalf("%d queued for two needs says, want 2", n)
	}

	// The orchestrator saying fyi DOWN to a worker: the worker holds nothing.
	rec := sayKind(t, d, worker, launcher.WireName, "fyi")
	if rec.Code != http.StatusOK {
		t.Fatalf("say answered %d: %s", rec.Code, rec.Body)
	}
	if n := len(pendingFrom(t, d, worker.ID)); n != 1 {
		t.Fatalf("%d queued for the worker, want the fyi queued as today", n)
	}
	if n := len(heldOn(t, d, worker.ID)); n != 0 {
		t.Fatalf("%d held on a card that does not hold", n)
	}
}

func TestTheHeldCountAndOldestRiseWithHeldAndFallWithRead(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	launcher, worker := holdingPair(t, d, OrchestratorTag)
	// holdingPair hands back the launcher as it was before it was tagged.
	launcher, _ = d.st.Get(launcher.ID)

	if n, at := d.heldNoticesFor(launcher); n != 0 || at != "" {
		t.Fatalf("a fresh card has %d held at %q", n, at)
	}
	d.holdNotice(launcher, worker, NoticeSilentStop, "a")
	n1, at1 := d.heldNoticesFor(launcher)
	if n1 != 1 || at1 == "" {
		t.Fatalf("after one fyi: %d at %q", n1, at1)
	}
	time.Sleep(5 * time.Millisecond)
	d.holdNotice(launcher, worker, NoticeSilentStop, "b")
	n2, at2 := d.heldNoticesFor(launcher)
	if n2 != 2 || at2 != at1 {
		t.Fatalf("after two: %d at %q, want 2 and the oldest unchanged %q", n2, at2, at1)
	}
	// A card that does not hold shows nothing.
	if n, at := d.heldNoticesFor(worker); n != 0 || at != "" {
		t.Fatalf("a worker shows %d at %q", n, at)
	}

	time.Sleep(5 * time.Millisecond)
	through := time.Now().UTC().Format(store.TimeFormat)
	changed, err := d.st.MarkNoticesRead(launcher.ID, through)
	if err != nil || !changed {
		t.Fatalf("read: %v %v", changed, err)
	}
	if n, at := d.heldNoticesFor(launcher); n != 0 || at != "" {
		t.Fatalf("after read: %d at %q", n, at)
	}
	if changed, _ := d.st.MarkNoticesRead(launcher.ID, through); changed {
		t.Fatal("a second read with nothing new reported a change")
	}
	time.Sleep(5 * time.Millisecond)
	d.holdNotice(launcher, worker, NoticeSilentStop, "c")
	if n, _ := d.heldNoticesFor(launcher); n != 1 {
		t.Fatalf("after a new fyi: %d, want 1", n)
	}
}

// A notice held between the read and the stamp is newer than what was handed back, so it
// still counts. The marker never moves back either.
func TestANoticeHeldBetweenTheReadAndTheStampStillCountsUnread(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	launcher, worker := holdingPair(t, d, OrchestratorTag)
	launcher, _ = d.st.Get(launcher.ID)

	d.holdNotice(launcher, worker, NoticeSilentStop, "a")
	// What the read handed back: the newest notice's own `at`.
	events, err := d.st.Events(launcher.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	var newest time.Time
	for _, e := range events {
		if e.Kind == store.EventNotified && e.At.After(newest) {
			newest = e.At
		}
	}
	through := newest.UTC().Format(store.TimeFormat)
	time.Sleep(5 * time.Millisecond)
	d.holdNotice(launcher, worker, NoticeSilentStop, "b") // held after the read, before the stamp

	if changed, err := d.st.MarkNoticesRead(launcher.ID, through); err != nil || !changed {
		t.Fatalf("stamp: %v %v", changed, err)
	}
	if n, _ := d.heldNoticesFor(launcher); n != 1 {
		t.Fatalf("%d unread after the stamp, want the one held after the read", n)
	}
	// An older stamp arriving late unreads nothing and moves nothing.
	if changed, _ := d.st.MarkNoticesRead(launcher.ID, "2000-01-01T00:00:00.000Z"); changed {
		t.Fatal("an older stamp reported a change")
	}
	if n, _ := d.heldNoticesFor(launcher); n != 1 {
		t.Fatalf("%d unread after an older stamp, want still 1", n)
	}
}

func TestAStuckCardWakesAnOrchestratorOnlyPastTheSecondStep(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	launcher, worker := holdingPair(t, d, OrchestratorTag)
	d.turnEnded(worker.ID)
	stopSilentlyByID(t, d, worker.ID)
	got, _ := d.st.Get(worker.ID)
	stopped := got.WaitingSinceOr(time.Now())

	// A silent stop is told to the launcher at two minutes. That is not a wake: the
	// notice is held, and nothing is typed or queued until ten.
	for _, after := range []time.Duration{90 * time.Second, 3 * time.Minute, 9 * time.Minute} {
		if err := d.watchWorkers(stopped.Add(after)); err != nil {
			t.Fatal(err)
		}
		if n := len(pendingFrom(t, d, launcher.ID)); n != 0 {
			t.Fatalf("%d queued at %v, want none", n, after)
		}
	}

	// Past the fourth step, ten minutes: one wake, once.
	for _, after := range []time.Duration{10 * time.Minute, 11 * time.Minute} {
		if err := d.watchWorkers(stopped.Add(after)); err != nil {
			t.Fatal(err)
		}
	}
	msgs := pendingFrom(t, d, launcher.ID)
	if len(msgs) != 1 || !strings.Contains(msgs[0].Text, "stuck") || !strings.Contains(msgs[0].Text, worker.ID) {
		t.Fatalf("%d queued, want one stuck wake: %v", len(msgs), msgs)
	}
	if n := len(heldOn(t, d, launcher.ID)); n != 1 {
		t.Fatalf("%d held, want the wake kept out of the held list", n)
	}
}

func TestAStuckCardDoesNotWakeALauncherThatDoesNotHold(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	d.turnEnded(worker.ID)
	stopSilentlyByID(t, d, worker.ID)
	got, _ := d.st.Get(worker.ID)
	stopped := got.WaitingSinceOr(time.Now())
	if err := d.watchWorkers(stopped.Add(11 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	// Today's one silent-stop notice, and no second one for a wake.
	if n := len(pendingFrom(t, d, launcher.ID)); n != 1 {
		t.Fatalf("%d queued, want only the silent stop", n)
	}
}

// Fyis parked on a holding card before fyis stopped being held are delivered once, at its next turn end.
func TestParkedFyisAreRedeliveredOnce(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	launcher, worker := holdingPair(t, d, OrchestratorTag)
	launcher, _ = d.st.Get(launcher.ID)
	d.holdNotice(launcher, worker, NoticeFYI, "parked before the upgrade")
	d.holdNotice(launcher, worker, NoticeSilentStop, "an automatic notice")

	d.redeliverHeldFyis()
	d.redeliverHeldFyis()
	msgs := pendingFrom(t, d, launcher.ID)
	if len(msgs) != 1 || msgs[0].Text != "parked before the upgrade" || !msgs[0].WaitTurn {
		t.Fatalf("queued = %+v, want the one parked fyi for the turn end", msgs)
	}
}

func TestAProgressReportOnADoneCardDoesNotAnswerDone(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	_, worker := holdingPair(t, d, OrchestratorTag)
	if err := d.st.SetStatus(worker.ID, store.StatusDone); err != nil {
		t.Fatal(err)
	}
	rec, out := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportProgress, Kind: "fyi",
		Recap: "on to the next task"})
	if rec.Code != http.StatusOK || out["status"] == store.StatusDone {
		t.Fatalf("a progress report answered %d %v, want the card working: %s", rec.Code, out["status"], rec.Body)
	}
	if got, err := d.st.Get(worker.ID); err != nil || got.Status == store.StatusDone {
		t.Fatalf("a progress report left the card done: %v %+v", err, got)
	}
}
