package daemon

import (
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// Owed answers: see owed.go and docs/rnd/long-turn-checkin-design.md section 11.

func endWorker(t *testing.T, d *Daemon, worker *store.Task) {
	t.Helper()
	if err := d.st.SetStatus(worker.ID, store.StatusDone); err != nil {
		t.Fatal(err)
	}
}

// orphanWorker is an agent-launched worker whose launcher is `by`, which names no card.
func orphanWorker(t *testing.T, d *Daemon, name, by string) *store.Task {
	t.Helper()
	w := peerCard(t, d, name)
	if err := d.st.SetTags(w.ID, []string{"sdk", OriginAgentTag}); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetLineage(w.ID, by, ""); err != nil {
		t.Fatal(err)
	}
	prompt(t, d, w.ID)
	w, _ = d.st.Get(w.ID)
	return w
}

func orchCard(t *testing.T, d *Daemon, name string) *store.Task {
	t.Helper()
	o := peerCard(t, d, name)
	if err := d.st.SetTags(o.ID, []string{OrchestratorTag}); err != nil {
		t.Fatal(err)
	}
	return o
}

func owedHeld(t *testing.T, d *Daemon, id, contains string) int {
	t.Helper()
	n := 0
	for _, h := range heldOn(t, d, id) {
		if h["source"] == NoticeOwed && strings.Contains(h["text"].(string), contains) {
			n++
		}
	}
	return n
}

func TestAWorkerThatEndsWithNoReportOpensAnItemOnItsLauncher(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	endWorker(t, d, worker)
	d.owedPass(time.Now())

	n, _, orphan := d.owedFor(launcher)
	if n != 1 || orphan {
		t.Fatalf("the launcher keeps %d items (orphan %v), want one", n, orphan)
	}
	// The launcher holds no notices, and the item is on its card all the same.
	if owedHeld(t, d, launcher.ID, "no report") != 1 {
		t.Fatalf("held = %v", heldOn(t, d, launcher.ID))
	}
	// Again changes nothing: one item per worker.
	d.owedPass(time.Now())
	if n, _, _ := d.owedFor(launcher); n != 1 {
		t.Fatalf("a second pass made %d", n)
	}
}

func TestTheOrchestratorHearsOnceAtTenMinutesAndNothingIsTyped(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	orch := orchCard(t, d, "chief")
	endWorker(t, d, worker)
	now := time.Now()
	d.owedPass(now)
	d.owedPass(now.Add(owedPushAfter - time.Minute))
	if n := owedHeld(t, d, orch.ID, "has not answered"); n != 0 {
		t.Fatalf("pushed at nine minutes: %d", n)
	}
	d.owedPass(now.Add(owedPushAfter + time.Second))
	d.owedPass(now.Add(3 * owedPushAfter))
	if n := owedHeld(t, d, orch.ID, launcher.WireName+" has not answered "+worker.WireName); n != 1 {
		t.Fatalf("the orchestrator got %d pushes, want one: %v", n, heldOn(t, d, orch.ID))
	}
	if msgs := pendingFrom(t, d, orch.ID); len(msgs) != 0 {
		t.Fatalf("something was queued or typed for the orchestrator: %v", msgs)
	}
	if msgs := pendingFrom(t, d, worker.ID); len(msgs) != 0 {
		t.Fatalf("something was written to the worker: %v", msgs)
	}
}

func TestTheOrchestratorIsNeverToldAboutItself(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	if err := d.st.SetTags(launcher.ID, []string{OrchestratorTag}); err != nil {
		t.Fatal(err)
	}
	endWorker(t, d, worker)
	now := time.Now()
	d.owedPass(now)
	d.owedPass(now.Add(2 * owedPushAfter))
	if n := owedHeld(t, d, launcher.ID, "has not answered"); n != 0 {
		t.Fatalf("the orchestrator was pushed about its own worker: %d", n)
	}
	if n, _, _ := d.owedFor(launcher); n != 1 {
		t.Fatalf("its row shows %d, want the chip's one", n)
	}
}

func TestAQuestionOpensAnItemAndTheLaunchersReplyClosesIt(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	d.peerSaid(worker.WireName, launcher, "which table?", KindNeeds)
	if err := d.st.SetStatus(worker.ID, store.StatusNeedsInput); err != nil {
		t.Fatal(err)
	}
	d.owedPass(time.Now())
	if n, _, _ := d.owedFor(launcher); n != 1 {
		t.Fatalf("a worker waiting on an answer opened %d", n)
	}
	if it, _ := d.st.OwedItemOf(worker.ID); it == nil || it.Reason != owedAsked {
		t.Fatalf("item = %+v", it)
	}
	worker, _ = d.st.Get(worker.ID)
	d.peerSaid(launcher.WireName, worker, "table three", KindNeeds)
	if n, _, _ := d.owedFor(launcher); n != 0 {
		t.Fatalf("the reply left %d open", n)
	}
	d.owedPass(time.Now().Add(time.Hour))
	if n, _, _ := d.owedFor(launcher); n != 0 {
		t.Fatalf("an answered question reopened: %d", n)
	}
}

func TestAnFYIOpensNothing(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	d.peerSaid(worker.WireName, launcher, "fyi: tests pass", KindFYI)
	if err := d.st.SetStatus(worker.ID, store.StatusNeedsInput); err != nil {
		t.Fatal(err)
	}
	d.owedPass(time.Now())
	if n, _, _ := d.owedFor(launcher); n != 0 {
		t.Fatalf("an fyi opened %d", n)
	}
}

func TestADoneReportOwesOnlyWhenItCarriesAnAsk(t *testing.T) {
	stopAfter := stopAfterReport
	stopAfterReport = func(*Daemon, string) error { return nil }
	t.Cleanup(func() { stopAfterReport = stopAfter })
	for name, c := range map[string]struct {
		ask  string
		want int
	}{"no ask": {"", 0}, "an ask": {"which table, from the launcher", 1}} {
		d := testDaemon(t)
		launcher, _ := launchedPair(t, d)
		rec, _ := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, NoCommit: "research",
			Recap: "written up", Ask: c.ask})
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", name, rec.Code, rec.Body)
		}
		d.owedPass(time.Now())
		if n, _, _ := d.owedFor(launcher); n != c.want {
			t.Fatalf("%s: %d items, want %d", name, n, c.want)
		}
	}
}

func TestReadingTheNoticesDoesNotCloseAnItem(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	if err := d.st.SetTags(launcher.ID, []string{HoldNoticesTag}); err != nil {
		t.Fatal(err)
	}
	endWorker(t, d, worker)
	d.owedPass(time.Now())
	evs := heldOn(t, d, launcher.ID)
	if len(evs) == 0 {
		t.Fatal("no held notice")
	}
	if _, err := d.st.MarkNoticesRead(launcher.ID, time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	d.owedPass(time.Now())
	if n, _, _ := d.owedFor(launcher); n != 1 {
		t.Fatalf("reading closed the item: %d", n)
	}
}

func TestADismissClosesAndAnExitClosesTheItem(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	endWorker(t, d, worker)
	d.owedPass(time.Now())
	d.closeOwed(worker.ID, "dismissed")
	if n, _, _ := d.owedFor(launcher); n != 0 {
		t.Fatalf("a dismiss left %d", n)
	}
	// A dismissed item is not reopened by the same ending.
	d.owedPass(time.Now())
	if n, _, _ := d.owedFor(launcher); n != 0 {
		t.Fatalf("the same ending reopened it: %d", n)
	}
}

func TestExitingTheWorkerClosesItsItem(t *testing.T) {
	d := testDaemon(t)
	parent := peerCard(t, d, "orchestrator")
	h := slowHarness(t, d)
	task, err := d.Launch(LaunchRequest{Harness: h, Cwd: t.TempDir(), Tags: []string{OriginAgentTag},
		SpawnedBy: "orchestrator"})
	if err != nil {
		t.Skipf("could not spawn a test runner on this machine: %v", err)
	}
	t.Cleanup(func() { _ = d.StopRunner(task.ID) })
	if err := d.st.PutOwedItem(store.OwedItem{Worker: task.ID, WorkerWire: task.WireName, Host: parent.ID,
		Launcher: parent.ID, Reason: owedEnded, Since: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := d.StopRunner(task.ID); err != nil {
		t.Fatal(err)
	}
	if it, _ := d.st.OwedItemOf(task.ID); it != nil {
		t.Fatalf("the exit left the item open: %+v", it)
	}
}

func TestTheListingLineAfterAClearAndAfterACompaction(t *testing.T) {
	for _, ev := range []SessionEvent{
		{Event: "start", Source: "clear"},
		{Event: "compact", Trigger: "auto"},
	} {
		d := testDaemon(t)
		launcher, worker := launchedPair(t, d)
		endWorker(t, d, worker)
		d.owedPass(time.Now())
		if msgs := pendingFrom(t, d, launcher.ID); len(msgs) != 0 {
			t.Fatalf("%s: queued before a clear: %v", ev.Event, msgs)
		}
		ev.Agent = launcher.WireName
		if err := d.onSession(ev); err != nil {
			t.Fatal(err)
		}
		msgs := pendingFrom(t, d, launcher.ID)
		if len(msgs) != 1 || msgs[0].FromPeer != "atrium" ||
			!strings.Contains(msgs[0].Text, "1 open item from your workers: "+worker.WireName) ||
			!strings.Contains(msgs[0].Text, "atrium_task notices") {
			t.Fatalf("%s: line = %+v", ev.Event, msgs)
		}
	}
}

func TestAnOrphanKeepsItsItemOnTheOrchestratorOrOnItsOwnRow(t *testing.T) {
	d := testDaemon(t)
	worker := orphanWorker(t, d, "worker", "ghost")
	endWorker(t, d, worker)
	d.owedPass(time.Now())
	if n, _, orphan := d.owedFor(worker); n != 1 || !orphan {
		t.Fatalf("with no orchestrator the worker's row shows %d (orphan %v)", n, orphan)
	}

	d2 := testDaemon(t)
	w2 := orphanWorker(t, d2, "worker", "ghost")
	orch := orchCard(t, d2, "chief")
	endWorker(t, d2, w2)
	d2.owedPass(time.Now())
	if n, _, _ := d2.owedFor(orch); n != 1 {
		t.Fatalf("the orchestrator's card shows %d", n)
	}
	if n, _, _ := d2.owedFor(w2); n != 0 {
		t.Fatalf("the worker's row shows %d too", n)
	}
}

func TestAnOrphanOnARoomWithNoOrchestratorTellsTheHubsOne(t *testing.T) {
	d := testDaemon(t)
	rl := &fakeRelay{peers: []RemotePeer{{Handle: "chief", Room: "sg4-control", Tags: []string{OrchestratorTag}}}}
	d.SetRelay(rl)
	worker := orphanWorker(t, d, "worker", "ghost")
	endWorker(t, d, worker)
	now := time.Now()
	d.owedPass(now)
	d.owedPass(now.Add(owedPushAfter + time.Second))
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		rl.mu.Lock()
		n := len(rl.got)
		rl.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if len(rl.got) != 1 || rl.got[0].To != "chief" || rl.got[0].Room != "sg4-control" ||
		!strings.Contains(rl.got[0].Text, "has not answered") {
		t.Fatalf("the hub's orchestrator was told %+v", rl.got)
	}
}

func TestAPermissionItemClosesWithoutAPushWhenApprovedInTime(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	orch := orchCard(t, d, "chief")
	if err := d.st.SetStatus(worker.ID, store.StatusNeedsPermission); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	d.owedPass(now.Add(owedPermAfter - time.Minute))
	if n, _, _ := d.owedFor(launcher); n != 0 {
		t.Fatalf("an item opened before five minutes: %d", n)
	}
	d.owedPass(now.Add(owedPermAfter + time.Second))
	if n, _, _ := d.owedFor(launcher); n != 1 {
		t.Fatalf("a five minute wait opened %d", n)
	}
	// The operator approves; the worker runs again.
	if err := d.st.SetStatus(worker.ID, store.StatusRunning); err != nil {
		t.Fatal(err)
	}
	d.owedPass(now.Add(owedPermAfter + owedPushAfter))
	if n, _, _ := d.owedFor(launcher); n != 0 {
		t.Fatalf("the item outlived its reason: %d", n)
	}
	if n := owedHeld(t, d, orch.ID, "has not answered"); n != 0 {
		t.Fatalf("a closed item was pushed: %d", n)
	}
}

func TestAReportThatReachesNobodyIsNotStampedAndTheOrchestratorHearsOfIt(t *testing.T) {
	stopAfter := stopAfterReport
	stopAfterReport = func(*Daemon, string) error { return nil }
	t.Cleanup(func() { stopAfterReport = stopAfter })
	d := testDaemon(t)
	worker := orphanWorker(t, d, "worker", store.HumanLauncher)
	orch := orchCard(t, d, "chief")
	rec, out := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, NoCommit: "research",
		Recap: "the clone is done"})
	if rec.Code != 200 || out["launcher_told"] != false {
		t.Fatalf("report: %d %v", rec.Code, out)
	}
	got, _ := d.st.Get(worker.ID)
	if got.ReportedAt != nil {
		t.Fatal("reported_at was stamped for a report nobody was queued to hear")
	}
	if n := func() int {
		c := 0
		for _, h := range heldOn(t, d, orch.ID) {
			if h["source"] == NoticeNoLauncher && strings.Contains(h["text"].(string), "the clone is done") {
				c++
			}
		}
		return c
	}(); n != 1 {
		t.Fatalf("the orchestrator heard of it %d times: %v", n, heldOn(t, d, orch.ID))
	}
}

func TestAnAnsweredQuestionBeforeTheWorkerStopsOwesNothing(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	d.peerSaid(worker.WireName, launcher, "which table?", KindNeeds)
	worker, _ = d.st.Get(worker.ID)
	d.peerSaid(launcher.WireName, worker, "table three", KindNeeds)
	if err := d.st.SetStatus(worker.ID, store.StatusNeedsInput); err != nil {
		t.Fatal(err)
	}
	d.owedPass(time.Now())
	if n, _, _ := d.owedFor(launcher); n != 0 {
		t.Fatalf("an answered question owed %d", n)
	}
}

func TestAReportWithNoAskWithdrawsAnEarlierQuestion(t *testing.T) {
	stopAfter := stopAfterReport
	stopAfterReport = func(*Daemon, string) error { return nil }
	t.Cleanup(func() { stopAfterReport = stopAfter })
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	d.peerSaid(worker.WireName, launcher, "which table?", KindNeeds)
	finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, NoCommit: "research", Recap: "done anyway"})
	d.owedPass(time.Now())
	if n, _, _ := d.owedFor(launcher); n != 0 {
		t.Fatalf("a done report with no ask left %d open", n)
	}
}

func TestOnlyAPermissionWaitNudgesALauncherThatHoldsNothing(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	if err := d.st.SetStatus(worker.ID, store.StatusNeedsPermission); err != nil {
		t.Fatal(err)
	}
	d.owedPass(time.Now().Add(owedPermAfter + time.Second))
	if msgs := pendingFrom(t, d, launcher.ID); len(msgs) != 1 || !strings.Contains(msgs[0].Text, "needs-permission") {
		t.Fatalf("the typed nudge = %v", msgs)
	}
	// A holder is never typed at.
	d2 := testDaemon(t)
	l2, w2 := launchedPair(t, d2)
	if err := d2.st.SetTags(l2.ID, []string{HoldNoticesTag}); err != nil {
		t.Fatal(err)
	}
	if err := d2.st.SetStatus(w2.ID, store.StatusNeedsPermission); err != nil {
		t.Fatal(err)
	}
	d2.owedPass(time.Now().Add(owedPermAfter + time.Second))
	if msgs := pendingFrom(t, d2, l2.ID); len(msgs) != 0 {
		t.Fatalf("a holder was typed at: %v", msgs)
	}
	if n, _, _ := d2.owedFor(l2); n != 1 {
		t.Fatalf("a holder's item is missing: %d", n)
	}
}

func TestNoListingLineWhenNothingIsOwedOrWhenTheItemIsTheWorkersOwn(t *testing.T) {
	d := testDaemon(t)
	launcher, _ := launchedPair(t, d)
	if err := d.onSession(SessionEvent{Agent: launcher.WireName, Event: "start", Source: "clear"}); err != nil {
		t.Fatal(err)
	}
	if err := d.onSession(SessionEvent{Agent: launcher.WireName, Event: "compact"}); err != nil {
		t.Fatal(err)
	}
	if msgs := pendingFrom(t, d, launcher.ID); len(msgs) != 0 {
		t.Fatalf("a launcher owed nothing got %v", msgs)
	}
	// An orphan's item is on its own row, which is not the worker's to be told about.
	o := orphanWorker(t, d, "stray", "ghost")
	endWorker(t, d, o)
	d.owedPass(time.Now())
	if err := d.onSession(SessionEvent{Agent: o.WireName, Event: "start", Source: "clear"}); err != nil {
		t.Fatal(err)
	}
	if msgs := pendingFrom(t, d, o.ID); len(msgs) != 0 {
		t.Fatalf("the orphan was told about itself: %v", msgs)
	}
}

func TestAnOrchestratorsOwnWorkNeverReachesTheOrchestratorsRow(t *testing.T) {
	stopAfter := stopAfterReport
	stopAfterReport = func(*Daemon, string) error { return nil }
	t.Cleanup(func() { stopAfterReport = stopAfter })
	d := testDaemon(t)
	other := orchCard(t, d, "chief")
	w := orphanWorker(t, d, "second-chief", "ghost")
	if err := d.st.SetTags(w.ID, []string{OriginAgentTag, OrchestratorTag}); err != nil {
		t.Fatal(err)
	}
	endWorker(t, d, w)
	now := time.Now()
	d.owedPass(now)
	d.owedPass(now.Add(2 * owedPushAfter))
	if n := owedHeld(t, d, other.ID, "has not answered"); n != 0 {
		t.Fatalf("an orchestrator was the subject of a push: %d", n)
	}
	finishWith(t, d, FinishRequest{Agent: "second-chief", Status: ReportDone, NoCommit: "x", Recap: "all done"})
	for _, h := range heldOn(t, d, other.ID) {
		if h["source"] == NoticeNoLauncher {
			t.Fatalf("an orchestrator's report was held for another: %v", h)
		}
	}
}
