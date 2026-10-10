//go:build integration

package daemon

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func resumeCard(t *testing.T, d *Daemon, name, resume string) *store.Task {
	t.Helper()
	task, _, err := d.st.Register(store.Observed{WireName: name, Worktree: "D:/git/atrium", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if resume != "" {
		if err := d.st.SetResumeID(task.ID, resume); err != nil {
			t.Fatal(err)
		}
	}
	return task
}

func resumeIDOf(t *testing.T, d *Daemon, id string) string {
	t.Helper()
	got, err := d.st.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return got.ResumeID
}

func stopBody(t *testing.T, d *Daemon, body map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	d.handleStop(rec, httptest.NewRequest("POST", "/stop", strings.NewReader(string(raw))))
}

func eventReason(t *testing.T, d *Daemon, id, kind string) string {
	t.Helper()
	evs, err := d.st.Events(id, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range evs {
		if e.Kind != store.EventNotified {
			continue
		}
		var p struct{ By, Reason string }
		_ = json.Unmarshal(e.Payload, &p)
		if p.By == kind {
			var p struct{ Reason string }
			_ = json.Unmarshal(e.Payload, &p)
			return p.Reason
		}
	}
	return ""
}

// r-021 item 1: a session bound by task id takes its conversation from a card
// with no live session, and is refused against a live one, with a reason on both.
func TestATaskBoundResumeMovesOrIsRefusedByLiveness(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer func() { cancel(); <-errCh }()

	old := resumeCard(t, d, "atriumx", "conv-1")
	mine := resumeCard(t, d, "merge", "")
	if err := d.st.SetStatus(old.ID, store.StatusDone); err != nil {
		t.Fatal(err)
	}

	start := SessionEvent{Agent: "merge", Event: "start", TaskID: mine.ID, Resume: "conv-1", Cwd: "D:/git/atrium"}
	if err := d.onSession(start); err != nil {
		t.Fatal(err)
	}
	if got := resumeIDOf(t, d, mine.ID); got != "conv-1" {
		t.Fatalf("the conversation did not move to the card that claimed it by id: %q", got)
	}
	if got := resumeIDOf(t, d, old.ID); got != "" {
		t.Fatalf("the old card kept it: %q", got)
	}
	if eventReason(t, d, old.ID, store.ResumeMoved) == "" || eventReason(t, d, mine.ID, store.ResumeMoved) == "" {
		t.Fatal("a move left no reason on both cards")
	}

	// Now the holder is live: a third card claiming by id is turned away.
	other := resumeCard(t, d, "orch", "")
	d.sup.runners[mine.ID] = &runner{taskID: mine.ID}
	if err := d.onSession(SessionEvent{Agent: "orch", Event: "start", TaskID: other.ID, Resume: "conv-1"}); err != nil {
		t.Fatal(err)
	}
	if got := resumeIDOf(t, d, mine.ID); got != "conv-1" {
		t.Fatalf("a live holder lost its conversation: %q", got)
	}
	if got := resumeIDOf(t, d, other.ID); got != "" {
		t.Fatalf("the claimant took a live card's conversation: %q", got)
	}
	for _, id := range []string{other.ID, mine.ID} {
		if eventReason(t, d, id, store.ResumeRefused) == "" {
			t.Fatalf("no reason on card %s", id)
		}
	}
}

// r-021 item 2: a claude nested in an agent's shell posts with the parent's task
// id and name. Its Stop and its SessionStart change nothing on the parent.
func TestANestedClaudeCannotChangeTheParentsResumeID(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer func() { cancel(); <-errCh }()

	parent := resumeCard(t, d, "parent", "")
	d.sup.runners[parent.ID] = &runner{taskID: parent.ID, pid: 100}
	yes := true

	if err := d.onSession(SessionEvent{Agent: "parent", Event: "start", TaskID: parent.ID, PID: 100,
		Resume: "parent-1", Resumable: &yes}); err != nil {
		t.Fatal(err)
	}
	if got := resumeIDOf(t, d, parent.ID); got != "parent-1" {
		t.Fatalf("the runner's own start was not taken: %q", got)
	}

	// The nested session's Stop: same name, same task id, an id nobody announced.
	stopBody(t, d, map[string]any{
		"agent": "parent", "task_id": parent.ID, "resume": "nested-9", "resumable": true,
	})
	if got := resumeIDOf(t, d, parent.ID); got != "parent-1" {
		t.Fatalf("a nested Stop changed the resume id to %q", got)
	}

	// And its SessionStart, from a different pid.
	if err := d.onSession(SessionEvent{Agent: "parent", Event: "start", TaskID: parent.ID, PID: 200,
		Resume: "nested-9", Resumable: &yes}); err != nil {
		t.Fatal(err)
	}
	got, _ := d.st.Get(parent.ID)
	if got.ResumeID != "parent-1" || got.PID == 200 {
		t.Fatalf("a nested start reached the card: resume %q pid %d", got.ResumeID, got.PID)
	}

	// The runner's own /clear still moves it: announced by its SessionStart.
	if err := d.onSession(SessionEvent{Agent: "parent", Event: "start", TaskID: parent.ID, PID: 100,
		Resume: "parent-2", Resumable: &yes}); err != nil {
		t.Fatal(err)
	}
	stopBody(t, d, map[string]any{"agent": "parent", "task_id": parent.ID, "resume": "parent-2", "resumable": true})
	if got := resumeIDOf(t, d, parent.ID); got != "parent-2" {
		t.Fatalf("a real /clear did not follow: %q", got)
	}
}

// r-021 item 3: a Stop whose name came from the directory does not land on a
// finished card.
func TestADirectoryNameNeverReachesADoneCard(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer func() { cancel(); <-errCh }()

	old := resumeCard(t, d, "atriumx", "")
	if err := d.st.SetStatus(old.ID, store.StatusDone); err != nil {
		t.Fatal(err)
	}
	stopBody(t, d, map[string]any{
		"agent": "atriumx", "name_source": "dir", "resume": "stray-1", "resumable": true,
		"cwd": "D:/git/atrium",
	})
	if got := resumeIDOf(t, d, old.ID); got != "" {
		t.Fatalf("a directory-named Stop stored %q on a done card", got)
	}
	got, _ := d.st.Get(old.ID)
	if got.Status != store.StatusDone {
		t.Fatalf("the done card was revived: %s", got.Status)
	}
}

// r-021 item 4: the live database's state (an old done card called atrium holds
// the merge conversation) repairs itself when the merge card's own session
// reports its id with its task id. No one-off clear.
func TestTheStrandedConversationMovesWhenItsOwnerStarts(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer func() { cancel(); <-errCh }()

	stranded := resumeCard(t, d, "atriumx", "18fa6feb")
	if err := d.st.SetStatus(stranded.ID, store.StatusDone); err != nil {
		t.Fatal(err)
	}
	merge := resumeCard(t, d, "merge", "")
	d.sup.runners[merge.ID] = &runner{taskID: merge.ID, pid: 55}
	yes := true
	if err := d.onSession(SessionEvent{Agent: "atriumx", NameSource: "dir", Event: "start", TaskID: merge.ID,
		PID: 55, Resume: "18fa6feb", Resumable: &yes}); err != nil {
		t.Fatal(err)
	}
	if got := resumeIDOf(t, d, merge.ID); got != "18fa6feb" {
		t.Fatalf("merge did not get its conversation back: %q", got)
	}
	if got := resumeIDOf(t, d, stranded.ID); got != "" {
		t.Fatalf("the stranded card kept it: %q", got)
	}
	// And the card was not renamed by a name it did not choose.
	if got, _ := d.st.Get(merge.ID); got.WireName == "" || strings.HasSuffix(got.WireName, "/atriumx") || got.WireName == "atriumx" {
		t.Fatalf("merge's wire name was overwritten: %q", got.WireName)
	}
}
