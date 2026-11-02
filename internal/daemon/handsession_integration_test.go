//go:build integration

package daemon

import (
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// b-cards-auto-revive part 4: `claude --resume` of a dead card's conversation, typed by hand in its folder, lands back
// on that card rather than on no card at all.
func TestAHandResumeOfADeadCardsConversationLandsOnThatCard(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d, _, cancel, errCh := startDaemon(t)
	defer func() { cancel(); <-errCh }()

	card := resumeCard(t, d, "worker", "conv-abcdef12")
	if err := d.st.SetStatus(card.ID, store.StatusDead); err != nil {
		t.Fatal(err)
	}
	if err := d.onSession(SessionEvent{Agent: "atriumx", NameSource: store.NameFromDir, Event: "start",
		Cwd: "D:/git/atrium", PID: 777, Resume: "conv-abcdef12"}); err != nil {
		t.Fatal(err)
	}
	tasks, err := d.st.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("the hand resume made a card of its own: %d cards", len(tasks))
	}
	got, _ := d.st.Get(card.ID)
	if got.Status != store.StatusNeedsInput || got.PID != 777 {
		t.Fatalf("the card did not take the session: status %s, pid %d", got.Status, got.PID)
	}
	if !strings.HasSuffix(got.WireName, "worker") {
		t.Fatalf("a directory name renamed the card: %q", got.WireName)
	}
}

// A card whose own runner is live keeps its session: a hand resume of the same conversation is a second copy.
func TestAHandResumeDoesNotTakeACardWithALiveRunner(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d, _, cancel, errCh := startDaemon(t)
	defer func() { cancel(); <-errCh }()

	card := resumeCard(t, d, "worker", "conv-abcdef12")
	d.sup.runners[card.ID] = &runner{taskID: card.ID, pid: 100}
	if err := d.onSession(SessionEvent{Agent: "atriumx", NameSource: store.NameFromDir, Event: "start",
		Cwd: "D:/git/atrium", PID: 777, Resume: "conv-abcdef12"}); err != nil {
		t.Fatal(err)
	}
	got, _ := d.st.Get(card.ID)
	if got.PID == 777 {
		t.Fatal("the hand session took the pid of a card with a live runner")
	}
	if got.ResumeID != "conv-abcdef12" {
		t.Fatalf("the live card lost its conversation: %q", got.ResumeID)
	}
}

// A start whose directory name only a finished card holds gets a card of its own, and its later hooks find that card
// by pid and folder. The finished card is untouched.
func TestAStartUnderAFinishedCardsNameGetsItsOwnCard(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d, _, cancel, errCh := startDaemon(t)
	defer func() { cancel(); <-errCh }()

	done := resumeCard(t, d, "atriumx", "")
	if err := d.st.SetStatus(done.ID, store.StatusDone); err != nil {
		t.Fatal(err)
	}
	ev := SessionEvent{Agent: "atriumx", NameSource: store.NameFromDir, Event: "start", Cwd: "D:/git/atrium",
		PID: 778, Resume: "abcdef1234"}
	if err := d.onSession(ev); err != nil {
		t.Fatal(err)
	}
	tasks, err := d.st.List()
	if err != nil {
		t.Fatal(err)
	}
	var own *store.Task
	for _, task := range tasks {
		if task.ID != done.ID {
			own = task
		}
	}
	if len(tasks) != 2 || own == nil || !strings.HasSuffix(own.WireName, "atriumx-abcdef12") {
		t.Fatalf("the start did not get a card of its own: %d cards, %+v", len(tasks), own)
	}
	ev.Event = "end"
	if err := d.onSession(ev); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.st.Get(own.ID); got.Status != store.StatusDead {
		t.Fatalf("the end did not reach the session's own card: %s", got.Status)
	}
	got, _ := d.st.Get(done.ID)
	if got.Status != store.StatusDone || got.PID == 778 {
		t.Fatalf("the finished card was touched: status %s, pid %d", got.Status, got.PID)
	}
}
