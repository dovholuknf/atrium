package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// A session saying it is stuck, and what it needs.
//
// The behaviour worth pinning is the difference between STOPPED and WORKING.
// Filing a session that is still going as waiting makes the count that drives
// every alert lie, and that count is what decides whether somebody gets
// interrupted.

func askOf(t *testing.T, d *Daemon, in HelpRequest) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	d.handleHelp(rec, httptest.NewRequest(http.MethodPost, "/help", bytes.NewReader(raw)))
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func cardFor(t *testing.T, d *Daemon, name string) *store.Task {
	t.Helper()
	task, _, err := d.st.Register(store.Observed{
		WireName: name, Worktree: "d:/git/atrium", Runner: "claude", PID: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

// The whole point: the ask lands on the card, where the board already draws it.
func TestAnAskLandsOnTheCard(t *testing.T) {
	d := testDaemon(t)
	task := cardFor(t, d, "stuck-one")

	rec, out := askOf(t, d, HelpRequest{
		Agent: "stuck-one", Blocked: true,
		Ask: "which of these two schemas is authoritative",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("answered %d: %s", rec.Code, rec.Body)
	}
	if out["recorded"] != true {
		t.Fatalf("not recorded: %v", out)
	}
	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Ask, "authoritative") {
		t.Fatalf("the ask is not on the card: %q", got.Ask)
	}
	if got.AskAt == nil {
		t.Fatal("the card does not say when it asked")
	}
	if got.Status != store.StatusNeedsInput {
		t.Fatalf("a blocked session is filed as %q", got.Status)
	}
}

// AN ASK DOES NOT EAT THE OPERATOR'S `WHY`.
//
// It used to. `why` is the field somebody writes once and reads in a week, and
// an ask landing there destroyed the standing answer to "what was I even
// doing" with a question that would be stale by lunchtime, with nothing to put
// it back from. The two facts also read identically once they share a line,
// which is the other half of the same bug.
func TestAnAskDoesNotOverwriteWhatTheCardIsFor(t *testing.T) {
	d := testDaemon(t)
	task := cardFor(t, d, "has-a-why")
	if err := d.st.SetWhy(task.ID, "port the vcpkg dependency and open a pull request"); err != nil {
		t.Fatal(err)
	}

	askOf(t, d, HelpRequest{Agent: "has-a-why", Blocked: true, Ask: "which branch is base"})

	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Why, "vcpkg") {
		t.Fatalf("asking a question destroyed what the card is for: %q", got.Why)
	}
	if !strings.Contains(got.Ask, "base") {
		t.Fatalf("the ask did not land: %q", got.Ask)
	}
}

// A session that is still working must NOT be filed as waiting. The status
// column is a bucket of human attention, and putting a working session in it
// makes the alerting count wrong.
func TestAWorkingSessionIsNotFiledAsWaiting(t *testing.T) {
	d := testDaemon(t)
	task := cardFor(t, d, "still-going")
	if err := d.st.SetStatus(task.ID, store.StatusRunning); err != nil {
		t.Fatal(err)
	}

	_, out := askOf(t, d, HelpRequest{
		Agent: "still-going", Ask: "is the staging database safe to drop", Blocked: false,
	})
	if out["waiting"] == true {
		t.Fatal("a session that said it was still working was filed as waiting")
	}
	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.StatusRunning {
		t.Fatalf("a working session's card moved to %q", got.Status)
	}
	// And the question is still recorded, or --working is a way to say nothing.
	if !strings.Contains(got.Ask, "staging database") {
		t.Fatalf("the ask was dropped: %q", got.Ask)
	}
}

// A card put down by hand stays put. Shelving is an answer, and a session
// inside a shelved worktree does not overrule it.
func TestAShelvedCardIsNotDraggedBack(t *testing.T) {
	d := testDaemon(t)
	task := cardFor(t, d, "shelved-one")
	if err := d.st.SetStatus(task.ID, store.StatusShelved); err != nil {
		t.Fatal(err)
	}
	askOf(t, d, HelpRequest{Agent: "shelved-one", Ask: "anybody there", Blocked: true})

	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.StatusShelved {
		t.Fatalf("asking for help unshelved a card: %q", got.Status)
	}
}

// An empty ask is refused, because it is what needs-input already means and
// recording it would overwrite whatever the card said before.
func TestAnEmptyAskIsRefused(t *testing.T) {
	d := testDaemon(t)
	cardFor(t, d, "silent")
	rec, _ := askOf(t, d, HelpRequest{Agent: "silent", Ask: "   ", Blocked: true})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("an empty ask answered %d", rec.Code)
	}
}

// Long asks are truncated rather than refused. Refusing makes an agent retry,
// and the retry is longer.
func TestALongAskIsTruncatedNotRefused(t *testing.T) {
	d := testDaemon(t)
	task := cardFor(t, d, "wordy")
	rec, _ := askOf(t, d, HelpRequest{
		Agent: "wordy", Blocked: true, Ask: strings.Repeat("x", MaxAsk*3),
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("a long ask answered %d", rec.Code)
	}
	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Ask) > MaxAsk+8 {
		t.Fatalf("the ask was stored at %d characters", len(got.Ask))
	}
}

// THE HOOK POSTURE. An agent atrium has never heard of gets `ok` and nothing
// recorded, because failing here would mean a session could fail at the moment
// it tried to ask for help.
func TestAnUnknownSessionIsNotAnError(t *testing.T) {
	d := testDaemon(t)
	rec, out := askOf(t, d, HelpRequest{Agent: "nobody-here", Ask: "hello", Blocked: true})
	if rec.Code != http.StatusOK {
		t.Fatalf("an unknown session answered %d", rec.Code)
	}
	if out["recorded"] != false {
		t.Fatalf("an unknown session was recorded somewhere: %v", out)
	}
}
