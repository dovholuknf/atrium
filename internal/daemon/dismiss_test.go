package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// Taking a question off a card without telling the session anything.
//
// The three ways that came first all deliver text: saying something to a card
// types it into the terminal, sending its note does the same, and a session
// finishing is the session's own decision. None of them is available to
// somebody who already answered by TYPING IN THE TERMINAL, which atrium cannot
// see and which is how anybody sitting in front of a session replies.

func dismiss(t *testing.T, d *Daemon, taskID string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/v1/tasks/"+taskID+"/asks", nil)
	req.SetPathValue("id", taskID)
	d.handleDismissAsks(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func dismissedCount(out map[string]any) float64 {
	n, _ := out["dismissed"].(float64)
	return n
}

// THE SHAPE THAT PRODUCED THIS. Two questions sat open on one card for two
// days because both were answered in the terminal. Nothing expires an ask, and
// the board falls back to the ask field when no waiting reason is set, so
// every later turn-end on that card announced itself as a question.
func TestDismissingTakesEveryQuestionOffWithoutSayingAnything(t *testing.T) {
	d := testDaemon(t)
	card := cardFor(t, d, "dotfiles")

	questions := []string{
		"which of these two schemas is authoritative",
		"which branch is base",
	}
	for _, q := range questions {
		rec, _ := askOf(t, d, HelpRequest{Agent: "dotfiles", Ask: q, Blocked: true})
		if rec.Code != http.StatusOK {
			t.Fatalf("could not ask %q: %d", q, rec.Code)
		}
	}
	open, err := d.st.OpenAsks(card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 2 {
		t.Fatalf("the fixture has %d open questions, not 2", len(open))
	}

	rec, out := dismiss(t, d, card.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("answered %d: %s", rec.Code, rec.Body.String())
	}
	if dismissedCount(out) != 2 {
		t.Fatalf("said it dismissed %v of 2", out["dismissed"])
	}

	open, err = d.st.OpenAsks(card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 0 {
		t.Fatalf("%d question(s) still outstanding", len(open))
	}

	// AND THE MIRROR COLUMN THE BOARD READS. A card whose questions are all
	// settled while this still holds one goes on announcing turn-ends as
	// questions, which is the defect rather than a detail of it.
	after, err := d.st.Get(card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Ask != "" {
		t.Fatalf("the card still draws %q after every question was dismissed", after.Ask)
	}

	// Recorded as dismissed rather than as answered, because that is what
	// happened and the event log is the only place it can be said.
	events, err := d.st.Events(card.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, e := range events {
		if strings.Contains(string(e.Payload), askDismissed) {
			found++
		}
	}
	if found != 2 {
		t.Fatalf("recorded %d dismissals of 2", found)
	}
}

// A card that was never asked anything is not an error. The menu entry only
// appears when there is something to dismiss, and a second click on a menu
// drawn a moment ago must not fail.
func TestDismissingNothingIsFine(t *testing.T) {
	d := testDaemon(t)
	card := cardFor(t, d, "quiet")

	rec, out := dismiss(t, d, card.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("answered %d for a card with no questions", rec.Code)
	}
	if dismissedCount(out) != 0 {
		t.Fatalf("claimed to dismiss %v", out["dismissed"])
	}
}

// A QUESTION PUT TO A PEER COMES OFF TOO.
//
// It is still a question nobody is going to answer, and the card is still
// stopped on it. The operator deciding it is finished with is the same
// decision either way, and leaving peer questions behind would mean the menu
// said it cleared them and did not.
func TestDismissingAlsoTakesOffAQuestionPutToAPeer(t *testing.T) {
	d := testDaemon(t)
	card := cardFor(t, d, "asker")
	// The peer has to be a session atrium knows, or the ask is refused before
	// it is ever recorded.
	//
	// Registered here rather than through `cardFor`, which gives every card
	// the same worktree and the same pid. `Register` falls back to that pair
	// when the wire name does not match, so a second card made that way
	// ADOPTS the first one and the test ends up asking a card about itself.
	peer, _, err := d.st.Register(store.Observed{
		WireName: "reviewer", Worktree: "d:/git/other", Runner: "claude", PID: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	rec, _ := askOf(t, d, HelpRequest{
		Agent: "asker", Ask: "is the migration safe to run",
		Peer: peer.WireName, Blocked: true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("could not ask: %d", rec.Code)
	}

	_, out := dismiss(t, d, card.ID)
	if dismissedCount(out) != 1 {
		t.Fatalf("dismissed %v of 1", out["dismissed"])
	}
	open, err := d.st.OpenAsks(card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 0 {
		t.Fatalf("a question put to a peer survived: %+v", open)
	}
}
