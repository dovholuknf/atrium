//go:build integration

package daemon

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

func TestParseKindDefaultsToNeedsAndNeverErrors(t *testing.T) {
	for in, want := range map[string]string{
		"fyi": KindFYI, " FYI ": KindFYI, "Fyi": KindFYI,
		"": KindNeeds, "needs": KindNeeds, "urgent": KindNeeds, "fyi!": KindNeeds, "f y i": KindNeeds,
	} {
		if got := parseKind(in); got != want {
			t.Errorf("parseKind(%q) = %q, want %q", in, got, want)
		}
	}
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

func tellKind(t *testing.T, d *Daemon, to *store.Task, from, kind string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	d.handleTell(rec, httptest.NewRequest(http.MethodPost, "/tell", bytes.NewReader([]byte(
		`{"from":"`+from+`","to":"`+to.WireName+`","text":"all quiet","kind":"`+kind+`"}`))))
	return rec
}

func TestAnFYIReportIsHeldNotTypedForBothTags(t *testing.T) {
	for _, tag := range []string{HoldNoticesTag, OrchestratorTag} {
		d := testDaemon(t)
		launcher, worker := holdingPair(t, d, tag)

		rec, out := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportProgress, Kind: "fyi",
			Recap: "the matrix is written up"})
		if rec.Code != http.StatusOK || out["launcher_told"] != true {
			t.Fatalf("%s: report answered %d: %s", tag, rec.Code, rec.Body)
		}
		if msgs := pendingFrom(t, d, launcher.ID); len(msgs) != 0 {
			t.Fatalf("%s: %d queued for an fyi, want none: %v", tag, len(msgs), msgs)
		}
		held := heldOn(t, d, launcher.ID)
		if len(held) != 1 || held[0]["source"] != NoticeFYI || held[0]["about_card"] != worker.ID ||
			held[0]["about"] != worker.WireName || !strings.Contains(held[0]["text"].(string), "the matrix") {
			t.Fatalf("%s: held = %v", tag, held)
		}
	}
}

func TestANeedsReportIsTypedAsToday(t *testing.T) {
	for _, kind := range []string{"", "needs", "gibberish"} {
		d := testDaemon(t)
		launcher, _ := holdingPair(t, d, OrchestratorTag)
		rec, _ := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, Kind: kind,
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
	for name, in := range map[string]FinishRequest{
		"done": {Agent: "worker", Status: ReportDone, Kind: "fyi", NoCommit: "research only",
			Recap: "the matrix is written up"},
		"progress with an ask": {Agent: "worker", Status: ReportProgress, Kind: "fyi",
			Recap: "the matrix is written up", Ask: "which table, from clint"},
	} {
		for _, tag := range []string{HoldNoticesTag, OrchestratorTag} {
			d := testDaemon(t)
			launcher, _ := holdingPair(t, d, tag)
			rec, _ := finishWith(t, d, in)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s %s: report answered %d: %s", name, tag, rec.Code, rec.Body)
			}
			for _, h := range heldOn(t, d, launcher.ID) {
				if h["source"] == NoticeFYI {
					t.Fatalf("%s %s: held as an fyi: %v", name, tag, h)
				}
			}
			// Typed as today. A hold-notices launcher already keeps every report, as a report.
			want := 1
			if tag == HoldNoticesTag {
				want = 0
			}
			if n := len(pendingFrom(t, d, launcher.ID)); n != want {
				t.Fatalf("%s %s: %d queued, want %d", name, tag, n, want)
			}
		}
	}
}

func TestAnFYIReportToALauncherThatDoesNotHoldIsTypedAsToday(t *testing.T) {
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

func TestAnFYISayIsHeldNotTypedNotQueuedForBothTags(t *testing.T) {
	for _, tag := range []string{HoldNoticesTag, OrchestratorTag} {
		d := testDaemon(t)
		launcher, worker := holdingPair(t, d, tag)

		rec := sayKind(t, d, launcher, worker.WireName, "fyi")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: say answered %d: %s", tag, rec.Code, rec.Body)
		}
		if msgs := pendingFrom(t, d, launcher.ID); len(msgs) != 0 {
			t.Fatalf("%s: %d queued for an fyi say: %v", tag, len(msgs), msgs)
		}
		held := heldOn(t, d, launcher.ID)
		if len(held) != 1 || held[0]["source"] != NoticeFYI || held[0]["about"] != worker.WireName ||
			held[0]["text"] != "all quiet" {
			t.Fatalf("%s: held = %v", tag, held)
		}
		// A say to the launcher is the worker's report.
		if got, _ := d.st.Get(worker.ID); got.ReportedAt == nil {
			t.Fatalf("%s: the fyi say did not count as the worker's report", tag)
		}
	}
}

func TestAnFYITellIsHeldAndANeedsTellIsQueued(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := holdingPair(t, d, OrchestratorTag)

	if rec := tellKind(t, d, launcher, worker.WireName, "fyi"); rec.Code != http.StatusOK {
		t.Fatalf("tell answered %d: %s", rec.Code, rec.Body)
	}
	if n := len(pendingFrom(t, d, launcher.ID)); n != 0 {
		t.Fatalf("%d queued for an fyi tell", n)
	}
	if n := len(heldOn(t, d, launcher.ID)); n != 1 {
		t.Fatalf("%d held, want one", n)
	}

	if rec := tellKind(t, d, launcher, worker.WireName, "needs"); rec.Code != http.StatusOK {
		t.Fatalf("tell answered %d: %s", rec.Code, rec.Body)
	}
	if n := len(pendingFrom(t, d, launcher.ID)); n != 1 {
		t.Fatalf("%d queued for a needs tell, want one", n)
	}
	if n := len(heldOn(t, d, launcher.ID)); n != 1 {
		t.Fatalf("%d held after a needs tell, want still one", n)
	}
}

func TestANeedsSayAndAnFYIToANonHolderAreQueuedAsToday(t *testing.T) {
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
	d := testDaemon(t)
	launcher, worker := holdingPair(t, d, OrchestratorTag)
	// holdingPair hands back the launcher as it was before it was tagged.
	launcher, _ = d.st.Get(launcher.ID)

	if n, at := d.heldNoticesFor(launcher); n != 0 || at != "" {
		t.Fatalf("a fresh card has %d held at %q", n, at)
	}
	sayKind(t, d, launcher, worker.WireName, "fyi")
	n1, at1 := d.heldNoticesFor(launcher)
	if n1 != 1 || at1 == "" {
		t.Fatalf("after one fyi: %d at %q", n1, at1)
	}
	time.Sleep(5 * time.Millisecond)
	sayKind(t, d, launcher, worker.WireName, "fyi")
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
	sayKind(t, d, launcher, worker.WireName, "fyi")
	if n, _ := d.heldNoticesFor(launcher); n != 1 {
		t.Fatalf("after a new fyi: %d, want 1", n)
	}
}

// A notice held between the read and the stamp is newer than what was handed back, so it
// still counts. The marker never moves back either.
func TestANoticeHeldBetweenTheReadAndTheStampStillCountsUnread(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := holdingPair(t, d, OrchestratorTag)
	launcher, _ = d.st.Get(launcher.ID)

	sayKind(t, d, launcher, worker.WireName, "fyi")
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
	sayKind(t, d, launcher, worker.WireName, "fyi") // held after the read, before the stamp

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
	d := testDaemon(t)
	launcher, worker := holdingPair(t, d, OrchestratorTag)
	d.turnEnded(worker.ID)
	nudgeThenStopAgain(t, d, worker.ID)
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
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	d.turnEnded(worker.ID)
	nudgeThenStopAgain(t, d, worker.ID)
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
