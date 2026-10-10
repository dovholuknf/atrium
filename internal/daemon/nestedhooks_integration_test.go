//go:build integration

package daemon

import (
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func nestedParent(t *testing.T, d *Daemon) *store.Task {
	t.Helper()
	task, _, err := d.st.Register(store.Observed{WireName: "parent", Worktree: "D:/git/atrium",
		Runner: "claude", PID: 100})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetResumeID(task.ID, "parent-1"); err != nil {
		t.Fatal(err)
	}
	d.sup.runners[task.ID] = &runner{taskID: task.ID, pid: 100}
	// The runner's own SessionStart comes first, as it does for a real card.
	if err := d.onSession(SessionEvent{Agent: "parent", Event: "start", TaskID: task.ID, PID: 100,
		Resume: "parent-1"}); err != nil {
		t.Fatal(err)
	}
	got, _ := d.st.Get(task.ID)
	return got
}

func TestANestedEndAndCompactLeaveTheParentAlone(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer func() { cancel(); <-errCh }()
	parent := nestedParent(t, d)
	yes := true
	if _, err := d.st.QueueMessage(parent.ID, "hello"); err != nil {
		t.Fatal(err)
	}

	for _, ev := range []string{"end", "compact", "join", "leave"} {
		if err := d.onSession(SessionEvent{Agent: "parent", Event: ev, TaskID: parent.ID, PID: 200,
			Resume: "nested-9", Resumable: &yes}); err != nil {
			t.Fatal(err)
		}
		got, _ := d.st.Get(parent.ID)
		if got.Status != parent.Status || got.ResumeID != "parent-1" || got.PID != 100 || got.Gated != parent.Gated {
			t.Fatalf("a nested %s reached the parent: status %s resume %q pid %d gated %v",
				ev, got.Status, got.ResumeID, got.PID, got.Gated)
		}
		if msgs, _ := d.st.PendingMessages(parent.ID); len(msgs) != 1 {
			t.Fatalf("a nested %s changed the parent's queue: %d", ev, len(msgs))
		}
	}
}

func TestTheParentsOwnEndAndAnOldHooksEndStillWork(t *testing.T) {
	for _, pid := range []int{100, 0} {
		d, _, cancel, errCh := startDaemon(t)
		parent := nestedParent(t, d)
		if err := d.onSession(SessionEvent{Agent: "parent", Event: "end", TaskID: parent.ID, PID: pid}); err != nil {
			t.Fatal(err)
		}
		got, _ := d.st.Get(parent.ID)
		if got.Status == parent.Status {
			t.Fatalf("pid %d: the card's own end did nothing (%s)", pid, got.Status)
		}
		cancel()
		<-errCh
	}
}

func TestANestedStopDoesNotEndTheParentsTurnOrTakeItsMessages(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer func() { cancel(); <-errCh }()
	parent := nestedParent(t, d)
	if _, err := d.st.QueueMessage(parent.ID, "for the parent"); err != nil {
		t.Fatal(err)
	}

	stopBody(t, d, map[string]any{"agent": "parent", "task_id": parent.ID, "pid": 200})
	got, _ := d.st.Get(parent.ID)
	if got.PID != 100 {
		t.Fatalf("a nested Stop moved the parent's pid to %d", got.PID)
	}
	if got.Status != parent.Status {
		t.Fatalf("a nested Stop ended the parent's turn: %s", got.Status)
	}
	if msgs, _ := d.st.PendingMessages(parent.ID); len(msgs) != 1 {
		t.Fatalf("a nested Stop took the parent's message: %d left", len(msgs))
	}

	// The parent's own Stop, and an older hook's with no pid, still deliver.
	for _, pid := range []int{100, 0} {
		stopBody(t, d, map[string]any{"agent": "parent", "task_id": parent.ID, "pid": pid})
		if msgs, _ := d.st.PendingMessages(parent.ID); len(msgs) != 0 {
			t.Fatalf("pid %d: the parent's own Stop did not take its message", pid)
		}
		if _, err := d.st.QueueMessage(parent.ID, "again"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestANestedPermissionIsGatedButDoesNotMoveTheParentsPid(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer func() { cancel(); <-errCh }()
	parent := nestedParent(t, d)
	if err := d.st.SetStatus(parent.ID, store.StatusShelved); err != nil {
		t.Fatal(err)
	}

	for _, pid := range []int{200, 100, 0} {
		_, auto, err := d.onPermRequest(PermissionRequest{Agent: "parent", Tool: "Bash",
			Command: "go build ./...", PID: pid, Cwd: "D:/git/atrium"})
		if err != nil {
			t.Fatal(err)
		}
		// The chain still runs whole: a shelved card is a standing no.
		if auto == nil || auto.Decision != "block" || strings.TrimSpace(auto.Reason) == "" {
			t.Fatalf("pid %d: the request was not answered through the chain: %+v", pid, auto)
		}
		got, _ := d.st.Get(parent.ID)
		if got.PID != 100 {
			t.Fatalf("pid %d: the parent's pid became %d", pid, got.PID)
		}
	}
}
