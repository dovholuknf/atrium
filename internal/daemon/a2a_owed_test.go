package daemon

import (
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// Item 41, docs/rnd/owed-report-design.md. A card owes its launcher a report only
// for a prompt the launcher sent.

// reportedWorker is a launched worker that has paid what its opening prompt
// made owed, and is waiting.
func reportedWorker(t *testing.T) (*Daemon, *store.Task, *store.Task) {
	t.Helper()
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	time.Sleep(5 * time.Millisecond)
	if err := d.st.MarkReported(worker.ID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	return d, launcher, worker
}

// AB2. The saorch case: a message from a third session, then a silent stop,
// gives the launcher no notice, and the board no STUCK.
func TestAMessageFromAThirdSessionOwesNothing(t *testing.T) {
	d, launcher, worker := reportedWorker(t)
	third := peerCard(t, d, "third")

	if _, err := d.deliverPeer(worker, third.WireName, "a report from one of your workers"); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.st.Get(worker.ID); got.OwesReport() {
		t.Fatal("a message from a session that is not the launcher made the card owe a report")
	}
	if stopTurn(t, d, "worker"); len(pendingFrom(t, d, launcher.ID)) != 0 {
		t.Fatalf("the launcher was told of a silent stop: %v", pendingFrom(t, d, launcher.ID))
	}
	if err := d.watchWorkers(time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	// A SILENT STOP is the debt this is about. The worker is left mid-turn an hour
	// on, which is a long turn (R3), a report about time and not about debt.
	if x := d.esc.get(worker.ID); x != nil && x.Source != NoticeLongTurn {
		t.Fatalf("the board rang STUCK for a debt nobody was owed: %+v", x)
	}
}

// The launcher's own message is a prompt it sent, so it makes the card owe.
func TestAMessageFromTheLauncherOwesAReport(t *testing.T) {
	d, launcher, worker := reportedWorker(t)

	if _, err := d.deliverPeer(worker, launcher.WireName, "now do the other thing"); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.st.Get(worker.ID); !got.OwesReport() {
		t.Fatal("a message from the launcher did not make the card owe a report")
	}
	// The first Stop carries the launcher's message in. The turn it starts ends silently.
	stopTurn(t, d, "worker")
	stopTurn(t, d, "worker")
	if n := len(pendingFrom(t, d, launcher.ID)); n != 1 {
		t.Fatalf("the launcher has %d notices, want one", n)
	}
}

// The operator's own prompt is not the launcher's.
func TestTheOperatorsPromptOwesNothingToTheLauncher(t *testing.T) {
	d, launcher, worker := reportedWorker(t)
	if err := d.st.AppendEvent(worker.ID, store.EventPrompted,
		map[string]any{"text": "hold on", "via": "terminal"}); err != nil {
		t.Fatal(err)
	}
	if stopTurn(t, d, "worker"); len(pendingFrom(t, d, launcher.ID)) != 0 {
		t.Fatal("the operator's prompt made the card owe its launcher")
	}
}

// A turn a monitor woke has no atrium prompt behind it. After a report it owes
// nothing, and before one it shares the debt of the prompt it never paid, so
// the launcher hears once rather than once per wake.
func TestAMonitorWakeOwesNothingNew(t *testing.T) {
	d, launcher, worker := reportedWorker(t)
	for i := 0; i < 3; i++ {
		d.turnResumed(worker.ID)
		d.turnEnded(worker.ID)
		stopTurn(t, d, "worker")
	}
	if len(pendingFrom(t, d, launcher.ID)) != 0 {
		t.Fatal("a woken turn after a report told the launcher")
	}

	d2 := testDaemon(t)
	launcher2, worker2 := launchedPair(t, d2)
	for i := 0; i < 3; i++ {
		d2.turnResumed(worker2.ID)
		stopTurn(t, d2, "worker")
	}
	if n := len(pendingFrom(t, d2, launcher2.ID)); n != 1 {
		t.Fatalf("%d notices across three woken turns, want one", n)
	}
}
