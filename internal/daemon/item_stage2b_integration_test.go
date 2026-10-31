//go:build integration

package daemon

import (
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// stuckEvents counts the stuck alerts written on a card.
func stuckEvents(t *testing.T, d *Daemon, taskID string) int {
	t.Helper()
	evs, err := d.st.Events(taskID, 200)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range evs {
		if e.Kind == store.EventNotified && strings.Contains(string(e.Payload), ItemStuckSource) {
			n++
		}
	}
	return n
}

func atrium(t *testing.T, d *Daemon, id string) int { return len(noticesFromAtrium(t, d, id)) }

// AN OWNER SILENT FOR 15 MINUTES IS ALERTED ONCE PER STALL.
func TestAnOwnerSilentFor15MinutesIsAlertedOnce(t *testing.T) {
	d := testDaemon(t)
	_, worker, it := itemRunning(t, d, "")
	blockWith(t, d, "need the staging key")
	now := time.Now()

	d.sweepItems(now.Add(14 * time.Minute))
	if n := stuckEvents(t, d, worker.ID); n != 0 {
		t.Fatalf("alerted at 14 minutes: %d", n)
	}
	d.sweepItems(now.Add(16 * time.Minute))
	d.sweepItems(now.Add(40 * time.Minute))
	if n := stuckEvents(t, d, worker.ID); n != 1 {
		t.Fatalf("%d alerts, want exactly 1", n)
	}
	if got, _ := d.st.ItemGet(it.ID); got.StuckAlerted != ItemStuckOwnerSilent {
		t.Fatalf("item: %+v", got)
	}
}

// A NEW STALL (blocked again after running) IS A NEW ALERT.
func TestABlockedAgainItemCanBeAlertedAgain(t *testing.T) {
	d := testDaemon(t)
	_, worker, it := itemRunning(t, d, "")
	blockWith(t, d, "first")
	now := time.Now()
	d.sweepItems(now.Add(16 * time.Minute))
	if _, _, err := d.st.ItemTransition(it.ID, store.ItemRunning, ""); err != nil {
		t.Fatal(err)
	}
	blockWith(t, d, "second")
	d.sweepItems(now.Add(40 * time.Minute))
	if n := stuckEvents(t, d, worker.ID); n != 2 {
		t.Fatalf("%d alerts, want 2", n)
	}
}

// AN OWNER WHOSE SESSION HAS ENDED IS ALERTED AT ONCE.
func TestAnOwnerWhoIsGoneIsAlertedAtOnce(t *testing.T) {
	d := testDaemon(t)
	launcher, worker, it := itemRunning(t, d, "")
	blockWith(t, d, "need the staging key")
	if err := d.st.SetStatus(launcher.ID, store.StatusDead); err != nil {
		t.Fatal(err)
	}
	d.sweepItems(time.Now())
	d.sweepItems(time.Now())
	if n := stuckEvents(t, d, worker.ID); n != 1 {
		t.Fatalf("%d alerts, want 1", n)
	}
	if got, _ := d.st.ItemGet(it.ID); got.StuckAlerted != ItemStuckOwnerGone {
		t.Fatalf("item: %+v", got)
	}
}

// THE HUMAN'S OWN ITEM HAS NO 15 MINUTE TIMER: THE BOARD ITEM IS THE ALERT.
func TestAHumanOwnedItemHasNoOwnerTimer(t *testing.T) {
	d := testDaemon(t)
	_, worker, _ := itemRunning(t, d, "human")
	blockWith(t, d, "need a decision")
	d.sweepItems(time.Now().Add(3 * time.Hour))
	if n := stuckEvents(t, d, worker.ID); n != 0 {
		t.Fatalf("%d alerts for a human's item", n)
	}
}

// A RUNNING OR FINISHED ITEM IS NEVER STUCK.
func TestARunningOrFinishedItemIsNeverStuck(t *testing.T) {
	d := testDaemon(t)
	_, worker, _ := itemRunning(t, d, "")
	d.sweepItems(time.Now().Add(3 * time.Hour))
	finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, SHA: "abc1234", Recap: "done", Ended: true})
	d.sweepItems(time.Now().Add(6 * time.Hour))
	if n := stuckEvents(t, d, worker.ID); n != 0 {
		t.Fatalf("%d alerts", n)
	}
}

// A WORKER NOT RESUMED 2 MINUTES AFTER AN UNBLOCK GETS THE ANSWER RE-TYPED ONCE, THEN ONE ALERT.
func TestAWorkerNotResumedIsRetypedOnceThenAlertedOnce(t *testing.T) {
	d := testDaemon(t)
	_, worker, it := itemRunning(t, d, "")
	blockWith(t, d, "need the staging key")
	if _, _, err := d.itemUnblock(it.ID, "use KEY-9", "orchestrator"); err != nil {
		t.Fatal(err)
	}
	msgs := pendingFrom(t, d, worker.ID)
	if err := d.markDelivered(worker.ID, "stop", messageIDs(msgs)); err != nil {
		t.Fatal(err)
	}
	now := time.Now()

	d.sweepItems(now.Add(time.Minute))
	if n := atrium(t, d, worker.ID); n != 0 {
		t.Fatalf("re-typed before 2 minutes: %d", n)
	}
	d.sweepItems(now.Add(3 * time.Minute))
	d.sweepItems(now.Add(3*time.Minute + 30*time.Second))
	again := noticesFromAtrium(t, d, worker.ID)
	if len(again) != 1 || !strings.Contains(again[0], "use KEY-9") || !strings.Contains(again[0], "in case you missed it") {
		t.Fatalf("the re-typed answer: %v", again)
	}
	if n := stuckEvents(t, d, worker.ID); n != 0 {
		t.Fatalf("alerted with the retry still out: %d", n)
	}
	d.sweepItems(now.Add(6 * time.Minute))
	d.sweepItems(now.Add(9 * time.Minute))
	if n := stuckEvents(t, d, worker.ID); n != 1 {
		t.Fatalf("%d alerts, want 1", n)
	}
	if n := atrium(t, d, worker.ID); n != 1 {
		t.Fatalf("re-typed more than once: %d", n)
	}
	if got, _ := d.st.ItemGet(it.ID); got.StuckAlerted != ItemStuckWorkerStalled || got.State != store.ItemBlocked {
		t.Fatalf("item: %+v", got)
	}
}

// A WORKER THAT RESUMES IN TIME IS NEITHER RE-TYPED NOR ALERTED.
func TestAWorkerThatResumesIsLeftAlone(t *testing.T) {
	d := testDaemon(t)
	_, worker, it := itemRunning(t, d, "")
	blockWith(t, d, "need the staging key")
	if _, _, err := d.itemUnblock(it.ID, "use KEY-9", "orchestrator"); err != nil {
		t.Fatal(err)
	}
	if err := d.markDelivered(worker.ID, "stop", messageIDs(pendingFrom(t, d, worker.ID))); err != nil {
		t.Fatal(err)
	}
	promptEvent(d, worker.ID)
	d.sweepItems(time.Now().Add(30 * time.Minute))
	if n := stuckEvents(t, d, worker.ID) + atrium(t, d, worker.ID); n != 0 {
		t.Fatalf("%d things for a worker that resumed", n)
	}
}

// AN ANSWER THAT NEVER REACHED THE WORKER IS NOT TYPED A SECOND TIME: IT IS STILL QUEUED. THE ALERT STILL COMES.
func TestAnUndeliveredAnswerIsNotDuplicated(t *testing.T) {
	d := testDaemon(t)
	_, worker, it := itemRunning(t, d, "")
	blockWith(t, d, "need the staging key")
	if _, _, err := d.itemUnblock(it.ID, "use KEY-9", "orchestrator"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	d.sweepItems(now.Add(3 * time.Minute))
	d.sweepItems(now.Add(6 * time.Minute))
	if n := len(pendingFrom(t, d, worker.ID)); n != 1 {
		t.Fatalf("%d messages queued, want the one", n)
	}
	if n := stuckEvents(t, d, worker.ID); n != 1 {
		t.Fatalf("%d alerts, want 1", n)
	}
}

// AN OWNER NOTICE THAT NOBODY COULD RECEIVE IS ALERTED AT DELIVERY.
func TestAnUnreachedOwnerNoticeAlertsAtOnce(t *testing.T) {
	d := testDaemon(t)
	_, worker, it := itemRunning(t, d, "director")
	blockWith(t, d, "need the staging key")
	if got, _ := d.st.ItemGet(it.ID); got.StuckAlerted != ItemStuckOwnerGone {
		t.Fatalf("item: %+v", got)
	}
	if n := stuckEvents(t, d, worker.ID); n != 1 {
		t.Fatalf("%d alerts, want 1", n)
	}
}

// RE-DELIVERY AFTER A CONTEXT CLEAR: ONCE, MARKED, AND ONLY WHILE IT IS STILL PENDING.
func TestAPendingOwnerNoticeIsRedeliveredOnceAfterAClear(t *testing.T) {
	d := testDaemon(t)
	launcher, _, _ := itemRunning(t, d, "")
	blockWith(t, d, "need the staging key")
	notice := noticesFromAtrium(t, d, launcher.ID)[0]
	if err := d.markDelivered(launcher.ID, "stop", messageIDs(pendingFrom(t, d, launcher.ID))); err != nil {
		t.Fatal(err)
	}
	carried := []deliveredSay{{From: store.ReservedHandle, Text: notice}}

	got := d.itemMissed(launcher.ID, carried)
	if len(got) != 1 || !strings.Contains(got[0].Text, "in case you missed it") || !strings.Contains(got[0].Text, notice) {
		t.Fatalf("the redelivery: %+v", got)
	}
	if again := d.itemMissed(launcher.ID, carried); len(again) != 0 {
		t.Fatalf("redelivered twice: %+v", again)
	}
}

func TestAnOwnerNoticeTheOwnerActedOnIsNotRedelivered(t *testing.T) {
	d := testDaemon(t)
	launcher, _, it := itemRunning(t, d, "")
	blockWith(t, d, "need the staging key")
	notice := noticesFromAtrium(t, d, launcher.ID)[0]
	if _, _, err := d.itemUnblock(it.ID, "use KEY-9", "orchestrator"); err != nil {
		t.Fatal(err)
	}
	if got := d.itemMissed(launcher.ID, []deliveredSay{{From: store.ReservedHandle, Text: notice}}); len(got) != 0 {
		t.Fatalf("redelivered a notice already answered: %+v", got)
	}
}

func TestAPendingAnswerIsRedeliveredToTheWorkerOnceAfterAClear(t *testing.T) {
	d := testDaemon(t)
	_, worker, it := itemRunning(t, d, "")
	blockWith(t, d, "need the staging key")
	if _, _, err := d.itemUnblock(it.ID, "use KEY-9", "orchestrator"); err != nil {
		t.Fatal(err)
	}
	answer := pendingFrom(t, d, worker.ID)[0].Text
	carried := []deliveredSay{{From: store.ReservedHandle, Text: answer}}

	got := d.itemMissed(worker.ID, carried)
	if len(got) != 1 || !strings.Contains(got[0].Text, "in case you missed it") || !strings.Contains(got[0].Text, "use KEY-9") {
		t.Fatalf("the redelivery: %+v", got)
	}
	if again := d.itemMissed(worker.ID, carried); len(again) != 0 {
		t.Fatalf("redelivered twice: %+v", again)
	}
}

// SAYS THAT ARE NOT AN ITEM'S PASS THROUGH UNTOUCHED.
func TestAClearLeavesOrdinarySaysAlone(t *testing.T) {
	d := testDaemon(t)
	launcher, _, _ := itemRunning(t, d, "")
	in := []deliveredSay{{From: "someone", Text: "hello"}, {From: store.ReservedHandle, Text: "not an item notice"}}
	if got := d.itemMissed(launcher.ID, in); len(got) != 2 {
		t.Fatalf("%+v", got)
	}
}
