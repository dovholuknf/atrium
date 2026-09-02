package daemon

import (
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

func quietTask(t *testing.T, d *Daemon, name string, pid int) *store.Task {
	t.Helper()
	task, _, err := d.st.Register(store.Observed{
		WireName: name, Worktree: "/tmp/atrium-test", Runner: "claude", PID: pid,
	})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

// A `running` column full of sessions that stopped hours ago is what makes the
// board untrustworthy. With no pid there is nothing to ask the operating
// system, so silence is the only signal left.
func TestSilentCardWithNoPIDIsAssumedGone(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task := quietTask(t, d, "silent", 0)
	if err := d.st.BackdateActivity(task.ID, QuietAfter+time.Hour); err != nil {
		t.Fatal(err)
	}

	if err := d.reapOnce(); err != nil {
		t.Fatalf("reap: %v", err)
	}
	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.StatusDead {
		t.Fatalf("a card silent for %s is still %s", QuietAfter+time.Hour, got.Status)
	}
}

// Being wrong has to cost one status change, not a lost session. Anything the
// session does brings the card back.
func TestAnAssumedGoneCardRevives(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task := quietTask(t, d, "revives", 0)
	if err := d.st.SetStatus(task.ID, store.StatusDead); err != nil {
		t.Fatal(err)
	}

	if err := d.onSession(SessionEvent{Agent: "revives", Event: "start"}); err != nil {
		t.Fatalf("session start: %v", err)
	}
	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.StatusRunning {
		t.Fatalf("card is %s after the session reported in, wanted running", got.Status)
	}
}

// A card waiting on a human is quiet because nobody answered it. Marking that
// dead would throw the question away and leave the agent frozen with nothing
// coming.
func TestWaitingCardsAreNeverAssumedGone(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	for _, status := range []string{store.StatusNeedsInput, store.StatusNeedsPermission} {
		task := quietTask(t, d, "waiting-"+status, 0)
		if err := d.st.SetStatus(task.ID, status); err != nil {
			t.Fatal(err)
		}
		if err := d.st.BackdateActivity(task.ID, QuietAfter+24*time.Hour); err != nil {
			t.Fatal(err)
		}

		if err := d.reapOnce(); err != nil {
			t.Fatalf("reap: %v", err)
		}
		got, err := d.st.Get(task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != status {
			t.Fatalf("a card in %s was moved to %s for being quiet", status, got.Status)
		}
	}
}

// A session that spoke recently is left alone, whatever else is true of it.
func TestRecentlyActiveCardsAreLeftAlone(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task := quietTask(t, d, "busy", 0)
	if err := d.reapOnce(); err != nil {
		t.Fatalf("reap: %v", err)
	}
	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.StatusRunning {
		t.Fatalf("a card active seconds ago was moved to %s", got.Status)
	}
}
