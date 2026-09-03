package daemon

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// A request nobody is parked on can never be answered: the reply channel lived
// in the connection that has gone. Left pending it keeps offering a question
// on behalf of a session that no longer exists.
func TestOrphanedRequestIsClosed(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task, _, err := d.st.Register(store.Observed{
		WireName: "orphan", Worktree: "/tmp/atrium-test", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Recorded directly, so there is no agent parked on it. This is the state
	// a session leaves behind when it dies between asking and being answered.
	p, _, err := d.st.RecordPermission(task.ID, "Bash", "rm -rf /", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetStatus(task.ID, store.StatusNeedsPermission); err != nil {
		t.Fatal(err)
	}

	// Inside the grace period nothing happens: the two facts are read at
	// different moments and a request can be younger than the hub's map.
	if err := d.reapOrphans(); err != nil {
		t.Fatal(err)
	}
	got, err := d.st.GetPermission(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DecidedAt != nil {
		t.Fatal("a request was closed inside the grace period")
	}

	// Past it, the request is answered so the queue stops offering it.
	shrinkGrace(t)
	if err := d.reapOrphans(); err != nil {
		t.Fatal(err)
	}
	got, err = d.st.GetPermission(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DecidedAt == nil {
		t.Fatal("an orphaned request is still pending")
	}
	if got.Decision != "block" {
		t.Fatalf("an orphan was answered %q, wanted block", got.Decision)
	}
	if got.DecidedBy != "orphaned" {
		t.Fatalf("recorded as decided by %q, wanted orphaned", got.DecidedBy)
	}
}

// The card moves too. Left in needs-permission it claims to be asking
// something, and nothing is.
func TestOrphanedCardGoesDead(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task, _, err := d.st.Register(store.Observed{
		WireName: "orphan-card", Worktree: "/tmp/atrium-test", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.st.RecordPermission(task.ID, "Bash", "ls", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetStatus(task.ID, store.StatusNeedsPermission); err != nil {
		t.Fatal(err)
	}
	shrinkGrace(t)

	if err := d.reapOrphans(); err != nil {
		t.Fatal(err)
	}
	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.StatusDead {
		t.Fatalf("the card is %q, wanted dead", got.Status)
	}
}

// A request somebody is parked on is the ordinary case and must survive. This
// is the test that stops the whole mechanism becoming a way to lose questions.
func TestALiveRequestIsNeverOrphaned(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task, _, err := d.st.Register(store.Observed{
		WireName: "live-req", Worktree: "/tmp/atrium-test", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Through the HTTP endpoint an agent actually posts to, not the hook
	// underneath it. The hook records the request; the endpoint is what parks
	// on the reply and registers it as live. Calling the hook directly was
	// the first version of this test and it failed, which is correct: without
	// the endpoint there really is nobody waiting.
	done := make(chan struct{})
	go func() {
		defer close(done)
		body := `{"agent":"live-req","tool":"Bash","command":"sleep 1"}`
		resp, err := http.Post("http://"+d.opts.AgentAddr+"/permission",
			"application/json", strings.NewReader(body))
		if err == nil {
			resp.Body.Close()
		}
	}()

	// Wait for it to reach the store, then take the grace period away.
	var id string
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		pend, err := d.st.PendingForTask(task.ID)
		if err == nil && len(pend) > 0 {
			id = pend[0].ID
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if id == "" {
		t.Fatal("the request never reached the store")
	}
	shrinkGrace(t)

	if err := d.reapOrphans(); err != nil {
		t.Fatal(err)
	}
	got, err := d.st.GetPermission(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.DecidedAt != nil {
		t.Fatalf("a request an agent is waiting on was closed as an orphan: %q", got.Reason)
	}

	// Let the parked goroutine go.
	if _, err := d.decide(id, "approve", "", ""); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the agent was never answered")
	}
}

// A card with a pid is the reaper's to judge, and its answer is a fact where
// this one is an inference.
func TestACardWithAPidIsLeftToTheReaper(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task, _, err := d.st.Register(store.Observed{
		WireName: "has-pid", Worktree: "/tmp/atrium-test", Runner: "claude",
		PID: 4242,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.st.RecordPermission(task.ID, "Bash", "ls", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetStatus(task.ID, store.StatusNeedsPermission); err != nil {
		t.Fatal(err)
	}
	shrinkGrace(t)

	if err := d.reapOrphans(); err != nil {
		t.Fatal(err)
	}
	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status == store.StatusDead {
		t.Fatal("a card with a pid was killed by inference rather than by the pid check")
	}
}

// shrinkGrace puts the test on the other side of the grace period, restored
// afterwards so one test cannot change what another is measuring.
//
// The grace period rather than the timestamps, because it is one value and
// the alternative is a store method whose only caller is a test.
func shrinkGrace(t *testing.T) {
	t.Helper()
	was := OrphanGrace
	OrphanGrace = 0
	t.Cleanup(func() { OrphanGrace = was })
}
