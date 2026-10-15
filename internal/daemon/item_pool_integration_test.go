//go:build integration

package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// poolCard is a resident card in a pool, with a directory of its own for LEARNINGS.md.
func poolCard(t *testing.T, d *Daemon, name, pool string) *store.Task {
	t.Helper()
	task, _, err := d.st.Register(store.Observed{WireName: name, Worktree: t.TempDir(), Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetTags(task.ID, []string{ResidentTagName, PoolTag + pool}); err != nil {
		t.Fatal(err)
	}
	prompt(t, d, task.ID)
	got, _ := d.st.Get(task.ID)
	return got
}

// ResidentTagName is link.ResidentTag, which the daemon does not import.
const ResidentTagName = "atrium:resident"

func submit(t *testing.T, d *Daemon, pool, brief, owner, by string) *store.ItemRow {
	t.Helper()
	it, code, err := d.submitPoolItem(pool, brief, owner, by)
	if err != nil {
		t.Fatalf("submit answered %d: %v", code, err)
	}
	return it
}

func endPoolItem(t *testing.T, d *Daemon, agent, learned string) {
	t.Helper()
	finishWith(t, d, FinishRequest{Agent: agent, Status: ReportDone, SHA: "abc1234", Recap: "done abc1234", Ended: true, Learned: learned})
}

func itemState(t *testing.T, d *Daemon, id string) *store.ItemRow {
	t.Helper()
	it, err := d.st.ItemGet(id)
	if err != nil {
		t.Fatal(err)
	}
	return it
}

func typedBriefs(t *testing.T, d *Daemon, id string) []string {
	return noticesFromAtrium(t, d, id)
}

// A POOL FEEDS ONE ITEM AT A TIME PER CARD: the second waits until the first is finished.
func TestAPoolIsFedOneItemAtATimePerCard(t *testing.T) {
	d := testDaemon(t)
	stubCommits(t, d, true, true)
	peerCard(t, d, "orchestrator")
	card := poolCard(t, d, "rev1", "reviewer")

	a := submit(t, d, "reviewer", "review internal/link", "", "orchestrator")
	b := submit(t, d, "reviewer", "review internal/store", "", "orchestrator")

	if got := itemState(t, d, a.ID); got.State != store.ItemRunning || got.TaskID != card.ID || got.Revision != 2 {
		t.Fatalf("first item: %+v", got)
	}
	if got := itemState(t, d, b.ID); got.State != store.ItemQueued || got.TaskID != "" {
		t.Fatalf("second item must wait: %+v", got)
	}
	if msgs := typedBriefs(t, d, card.ID); len(msgs) != 1 || !strings.Contains(msgs[0], "review internal/link") {
		t.Fatalf("the card was handed %q, want only the first brief", msgs)
	}
	// A reaper tick changes nothing while the card is busy.
	d.feedPools()
	if got := itemState(t, d, b.ID); got.State != store.ItemQueued {
		t.Fatalf("a tick fed a busy card: %+v", got)
	}

	endPoolItem(t, d, "rev1", "")
	d.feedPool("reviewer")
	if got := itemState(t, d, a.ID); got.State != store.ItemFinished {
		t.Fatalf("first item: %+v", got)
	}
	if got := itemState(t, d, b.ID); got.State != store.ItemRunning || got.TaskID != card.ID {
		t.Fatalf("second item after the first finished: %+v", got)
	}
}

// A BLOCKED ITEM IS BUSY: the card takes nothing new until it is finished.
func TestABlockedItemKeepsItsPoolCardBusy(t *testing.T) {
	d := testDaemon(t)
	stubCommits(t, d, true, true)
	peerCard(t, d, "orchestrator")
	poolCard(t, d, "rev1", "reviewer")
	a := submit(t, d, "reviewer", "first", "", "orchestrator")
	b := submit(t, d, "reviewer", "second", "", "orchestrator")

	finishWith(t, d, FinishRequest{Agent: "rev1", Status: ReportBlocked, Ask: "which branch", Recap: "which branch", Ended: true})
	d.feedPool("reviewer")
	if got := itemState(t, d, a.ID); got.State != store.ItemBlocked {
		t.Fatalf("first item: %+v", got)
	}
	if got := itemState(t, d, b.ID); got.State != store.ItemQueued {
		t.Fatalf("a blocked card was handed a second item: %+v", got)
	}
}

// MORE CARDS IS MORE THROUGHPUT: each idle card takes one, in submission order, and the rest wait.
func TestEachIdleCardInAPoolTakesOneItem(t *testing.T) {
	d := testDaemon(t)
	peerCard(t, d, "orchestrator")
	c1 := poolCard(t, d, "rev1", "reviewer")
	c2 := poolCard(t, d, "rev2", "reviewer")
	poolCard(t, d, "other", "elsewhere")

	a := submit(t, d, "reviewer", "one", "", "orchestrator")
	b := submit(t, d, "reviewer", "two", "", "orchestrator")
	c := submit(t, d, "reviewer", "three", "", "orchestrator")

	ga, gb, gc := itemState(t, d, a.ID), itemState(t, d, b.ID), itemState(t, d, c.ID)
	if ga.State != store.ItemRunning || gb.State != store.ItemRunning || gc.State != store.ItemQueued {
		t.Fatalf("states %s %s %s", ga.State, gb.State, gc.State)
	}
	if ga.TaskID == gb.TaskID || (ga.TaskID != c1.ID && ga.TaskID != c2.ID) {
		t.Fatalf("two items on one card: %s %s", ga.TaskID, gb.TaskID)
	}
}

// A POOL WITH NO CARD REFUSES, and an item for another pool is never typed here.
func TestSubmittingToAPoolWithNoCardIsRefused(t *testing.T) {
	d := testDaemon(t)
	peerCard(t, d, "orchestrator")
	poolCard(t, d, "rev1", "reviewer")
	if _, code, err := d.submitPoolItem("nobody", "x", "", "orchestrator"); err == nil || code != 409 {
		t.Fatalf("code %d err %v", code, err)
	}
	if _, code, err := d.submitPoolItem("Bad Name", "x", "", "orchestrator"); err == nil || code != 400 {
		t.Fatalf("code %d err %v", code, err)
	}
	if _, _, err := d.submitPoolItem("reviewer", "x", "tester", "orchestrator"); err == nil {
		t.Fatal("a tester owner was accepted")
	}
}

// THE OWNER OF A POOL ITEM IS TOLD, not the card's launcher.
func TestAPoolItemsOwnerIsTheSubmitterNotTheLauncher(t *testing.T) {
	d := testDaemon(t)
	stubCommits(t, d, true, true)
	peerCard(t, d, "orchestrator")
	submitter := peerCard(t, d, "submitter")
	poolCard(t, d, "rev1", "reviewer")
	it := submit(t, d, "reviewer", "go", "", "submitter@here")
	if it.Owner != "submitter" || it.OwnerKind != store.ItemOwnerSession {
		t.Fatalf("owner: %+v", it)
	}
	endPoolItem(t, d, "rev1", "")
	if n := atrium(t, d, submitter.ID); n != 1 {
		t.Fatalf("the submitter has %d notices, want 1", n)
	}
}

// CANCELLING A QUEUED ITEM finishes it with nobody to ask, and the pool is not stuck behind it.
func TestCancellingAQueuedPoolItem(t *testing.T) {
	d := testDaemon(t)
	peerCard(t, d, "orchestrator")
	poolCard(t, d, "rev1", "reviewer")
	submit(t, d, "reviewer", "one", "", "orchestrator")
	b := submit(t, d, "reviewer", "two", "", "orchestrator")
	if _, code, err := d.itemCancel(b.ID, "orchestrator"); err != nil {
		t.Fatalf("cancel answered %d: %v", code, err)
	}
	if got := itemState(t, d, b.ID); got.State != store.ItemFinished || got.Note != "cancelled by owner" {
		t.Fatalf("%+v", got)
	}
	if owed, _ := d.st.ItemsOwed(50); len(owed) != 1 {
		t.Fatalf("%d pushes owed, want only the first item's running push, and nothing for the one that never had a card", len(owed))
	}
}

func readLearnings(t *testing.T, card *store.Task) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(card.Worktree, LearningsFile))
	if err != nil {
		return ""
	}
	return string(b)
}

// LEARNED IS APPENDED, once per item, and the next item is pointed at the file.
func TestLearnedIsAppendedToLearningsMd(t *testing.T) {
	d := testDaemon(t)
	stubCommits(t, d, true, true)
	peerCard(t, d, "orchestrator")
	card := poolCard(t, d, "rev1", "reviewer")
	a := submit(t, d, "reviewer", "one", "", "orchestrator")

	endPoolItem(t, d, "rev1", "the store tests\n  need the tag   integration")
	endPoolItem(t, d, "rev1", "a replay with another learning is not appended again")
	body := readLearnings(t, card)
	if n := countEntries(body); n != 1 || !strings.Contains(body, "## "+a.ID) ||
		!strings.Contains(body, "the store tests need the tag integration") || strings.Contains(body, "replay") {
		t.Fatalf("LEARNINGS.md:\n%s", body)
	}
	submit(t, d, "reviewer", "two", "", "orchestrator")
	msgs := typedBriefs(t, d, card.ID)
	if last := msgs[len(msgs)-1]; !strings.Contains(last, "Read LEARNINGS.md (1 entries)") {
		t.Fatalf("the second item was handed %q", last)
	}
}

// A card that is not in a pool leaves nothing, whatever it says.
func TestLearnedFromACardOutsideAPoolIsIgnored(t *testing.T) {
	d := testDaemon(t)
	stubCommits(t, d, true, true)
	_, worker, _ := itemRunning(t, d, "")
	finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, SHA: "abc1234", Recap: "done", Ended: true, Learned: "x"})
	if readLearnings(t, worker) != "" {
		t.Fatal("a non-pool card wrote LEARNINGS.md")
	}
}

// THE CAP IS SAID, NEVER SILENT: at 50 entries the next learning is NOT written and the card carries an alert, and
// nothing already there is touched.
func TestLearningsCapIsAlertedAndNothingIsDropped(t *testing.T) {
	d := testDaemon(t)
	stubCommits(t, d, true, true)
	peerCard(t, d, "orchestrator")
	card := poolCard(t, d, "rev1", "reviewer")
	var seed strings.Builder
	seed.WriteString(learningsHeader)
	for i := 0; i < LearningsMaxEntries-1; i++ {
		seed.WriteString("## old\n\nentry\n\n")
	}
	path := filepath.Join(card.Worktree, LearningsFile)
	if err := os.WriteFile(path, []byte(seed.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	submit(t, d, "reviewer", "one", "", "orchestrator")
	submit(t, d, "reviewer", "two", "", "orchestrator")

	endPoolItem(t, d, "rev1", "the fiftieth")
	if n := countEntries(readLearnings(t, card)); n != LearningsMaxEntries {
		t.Fatalf("%d entries, want %d", n, LearningsMaxEntries)
	}
	if n := learningsAlerts(t, d, card.ID); n != 1 {
		t.Fatalf("%d alerts at the cap, want 1 saying it was reached", n)
	}
	before := readLearnings(t, card)
	d.feedPool("reviewer")
	endPoolItem(t, d, "rev1", "the fifty-first")
	if got := readLearnings(t, card); got != before {
		t.Fatal("the file changed past the cap")
	}
	if n := learningsAlerts(t, d, card.ID); n != 2 {
		t.Fatalf("%d alerts, want a second saying the learning was not written", n)
	}
}

func TestLearningsByteCap(t *testing.T) {
	d := testDaemon(t)
	stubCommits(t, d, true, true)
	peerCard(t, d, "orchestrator")
	card := poolCard(t, d, "rev1", "reviewer")
	path := filepath.Join(card.Worktree, LearningsFile)
	if err := os.WriteFile(path, []byte(learningsHeader+"## big\n\n"+strings.Repeat("x", LearningsMaxBytes-len(learningsHeader)-20)+"\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := readLearnings(t, card)
	submit(t, d, "reviewer", "one", "", "orchestrator")
	endPoolItem(t, d, "rev1", strings.Repeat("word ", 100))
	if readLearnings(t, card) != before || learningsAlerts(t, d, card.ID) != 1 {
		t.Fatal("an entry that passes 20 KB was written, or not alerted for")
	}
}

func learningsAlerts(t *testing.T, d *Daemon, taskID string) int {
	t.Helper()
	evs, err := d.st.Events(taskID, 200)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range evs {
		if e.Kind == store.EventNotified && strings.Contains(string(e.Payload), LearningsSource) {
			n++
		}
	}
	return n
}
