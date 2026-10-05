package daemon

import (
	"github.com/dovholuknf/atrium/internal/store"
	"strings"
	"testing"
	"time"
)

// A say to a card that moved goes on to the new card and the sender is told where.
func TestASayToAMovedCardIsForwarded(t *testing.T) {
	d, f := roomDaemon(t)
	peerCard(t, d, "sa1")
	old := peerCard(t, d, "sa2")
	if _, err := d.st.Freeze(old.ID, "mv1", time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetMovedTo(old.ID, "mv1", "sg3~NEW1"); err != nil {
		t.Fatal(err)
	}
	for _, to := range []string{"sa2", old.ID} {
		code, out := say(t, d, map[string]string{"from": "sa1", "to": to, "text": "hi"})
		if code != 200 || out["forwarded"] != "moved to sg3~NEW1" {
			t.Fatalf("%s: %d %+v", to, code, out)
		}
	}
	got := f.says()
	if len(got) != 2 || got[0].Room != "sg3" || got[0].To != "NEW1" {
		t.Fatalf("relay got %+v", got)
	}
}

// A frozen card is held: its says are queued for the move, not delivered.
func TestAFrozenCardHoldsItsSays(t *testing.T) {
	d, _ := roomDaemon(t)
	peerCard(t, d, "sa1")
	c := peerCard(t, d, "sa2")
	if _, err := d.st.Freeze(c.ID, "mv1", time.Minute); err != nil {
		t.Fatal(err)
	}
	if !d.holdingMessages(c.ID) {
		t.Fatal("a frozen card is not held")
	}
	if code, out := say(t, d, map[string]string{"from": "sa1", "to": "sa2", "text": "hold"}); code != 200 {
		t.Fatalf("%d %+v", code, out)
	}
	if msgs, _ := d.takeMessages(c.ID, "hook"); len(msgs) != 0 {
		t.Fatalf("a frozen card took %+v", msgs)
	}
	q, _ := d.st.FreezeQueue(c.ID)
	if len(q) != 1 {
		t.Fatalf("freeze queue %+v", q)
	}
	if _, err := d.st.Unfreeze(c.ID, "mv1", true); err != nil {
		t.Fatal(err)
	}
	if d.holdingMessages(c.ID) {
		t.Fatal("still held after the undo")
	}
	if msgs, _ := d.takeMessages(c.ID, "hook"); len(msgs) != 1 {
		t.Fatalf("after the undo %+v", msgs)
	}
}

// The chain is followed up to eight hops and a loop is named.
func TestFollowMovedChainsAndLoops(t *testing.T) {
	d, _ := roomDaemon(t)
	a, b := peerCard(t, d, "sa1"), peerCard(t, d, "sa2")
	if _, err := d.st.Freeze(a.ID, "m", time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetMovedTo(a.ID, "m", "m1mini~"+b.ID); err != nil {
		t.Fatal(err)
	}
	a, _ = d.st.Get(a.ID)
	end, err := d.followMoved(a)
	if err != nil || end.Live == nil || end.Live.ID != b.ID {
		t.Fatalf("%+v %v", end, err)
	}
	if _, err := d.st.Freeze(b.ID, "m2", time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetMovedTo(b.ID, "m2", "m1mini~"+a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.followMoved(a); err == nil || !strings.Contains(err.Error(), "loops at") {
		t.Fatalf("no loop named: %v", err)
	}
}

// A worker whose launcher moved to another room reports there.
func TestALauncherThatMovedIsFollowed(t *testing.T) {
	d, _ := roomDaemon(t)
	l := peerCard(t, d, "sa1")
	w := peerCard(t, d, "sa2")
	if err := d.st.SetLauncher(w.ID, l.WireName, l.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.st.Freeze(l.ID, "m", time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetMovedTo(l.ID, "m", "sg3~NEW1"); err != nil {
		t.Fatal(err)
	}
	w, _ = d.st.Get(w.ID)
	if got := d.launcherAfterMove(w, mustGet(t, d, l.ID)); got != nil {
		t.Fatalf("launcher %+v", got)
	}
	w, _ = d.st.Get(w.ID)
	if w.SpawnedByID != "sg3~NEW1" || !strings.HasSuffix(w.SpawnedBy, "@sg3") {
		t.Fatalf("not re-pointed: %q %q", w.SpawnedBy, w.SpawnedByID)
	}
}

func mustGet(t *testing.T, d *Daemon, id string) *store.Task {
	t.Helper()
	x, err := d.st.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return x
}

// A launch for one move is made once: a repeat answers the same card, which is
// moved_from the old one and sits in its pinned slot.
func TestARepeatLaunchForOneMoveAnswersTheSameCard(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer func() {
		cancel()
		<-errCh
	}()
	h := slowHarness(t, d)
	req := LaunchRequest{Harness: h, Cwd: t.TempDir(), Title: "moved", MovedFrom: "sg3~OLD1", Pinned: true, PinOrder: 3}
	a, err := d.Launch(req)
	if err != nil {
		t.Skipf("could not spawn a slow test runner on this machine: %v", err)
	}
	defer func() { _ = d.StopRunner(a.ID) }()
	b, err := d.Launch(req)
	if err != nil || b.ID != a.ID {
		t.Fatalf("repeat: %v %v", b, err)
	}
	got := mustGet(t, d, a.ID)
	if got.MovedFrom != "sg3~OLD1" || !got.Pinned || got.PinOrder != 3 {
		t.Fatalf("%+v", got)
	}
}
