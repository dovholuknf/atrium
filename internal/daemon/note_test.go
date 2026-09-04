package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// A note is for you. Sending it is a second, separate act.
//
// The tests are about the seam between those two: what is written down does
// not go anywhere, and what is sent leaves nothing behind, and a send that
// fails leaves what you wrote where you can still see it.

func noteCard(t *testing.T, d *Daemon, name string) *store.Task {
	t.Helper()
	task, _, err := d.st.Register(store.Observed{
		WireName: name, Worktree: "/tmp/atrium-test", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func sendNote(t *testing.T, d *Daemon, id string) (map[string]any, int) {
	t.Helper()
	r := httptest.NewRequest("POST", "/v1/tasks/"+id+"/note/send", nil)
	r.SetPathValue("id", id)
	w := httptest.NewRecorder()
	d.handleSendNote(w, r)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return out, w.Code
}

// Writing one down sends nothing. That is the entire distinction from the box
// underneath it, which fires as soon as there is anything in it.
func TestWritingANoteSendsNothing(t *testing.T) {
	d := testDaemon(t)
	task := noteCard(t, d, "note-1")

	if err := d.st.SetNote(task.ID, "ask it about the retry loop"); err != nil {
		t.Fatal(err)
	}

	pending, err := d.st.PendingMessages(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("writing a note queued %d messages", len(pending))
	}
	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.HasNote() {
		t.Fatal("the note was not kept")
	}
}

// Three things written during a long turn arrive as ONE instruction. That is
// the whole reason a note exists rather than three messages.
func TestSendingANoteMakesOneMessage(t *testing.T) {
	d := testDaemon(t)
	task := noteCard(t, d, "note-2")

	if err := d.st.SetNote(task.ID,
		"one: check the retry loop\ntwo: the timeout is wrong\nthree: write it up"); err != nil {
		t.Fatal(err)
	}
	out, code := sendNote(t, d, task.ID)
	if code != http.StatusOK {
		t.Fatalf("sending answered %d", code)
	}
	if out["delivered"] != "queued" {
		t.Fatalf("delivered %v, wanted queued for a card with no terminal", out["delivered"])
	}

	pending, err := d.st.PendingMessages(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("three lines became %d messages", len(pending))
	}
	for _, want := range []string{"retry loop", "timeout", "write it up"} {
		if !strings.Contains(pending[0].Text, want) {
			t.Fatalf("the message lost %q: %q", want, pending[0].Text)
		}
	}
}

// Sending empties the pad, or the next glance at the card shows something that
// has already gone.
func TestSendingClearsTheNote(t *testing.T) {
	d := testDaemon(t)
	task := noteCard(t, d, "note-3")

	if err := d.st.SetNote(task.ID, "something"); err != nil {
		t.Fatal(err)
	}
	if _, code := sendNote(t, d, task.ID); code != http.StatusOK {
		t.Fatalf("sending answered %d", code)
	}
	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.HasNote() {
		t.Fatalf("the note survived being sent: %q", got.Note)
	}
}

// Nothing written down is a refusal rather than an empty message, because an
// empty message reaches a model and costs it a turn to read nothing.
func TestSendingAnEmptyNoteIsRefused(t *testing.T) {
	d := testDaemon(t)
	task := noteCard(t, d, "note-4")

	if _, code := sendNote(t, d, task.ID); code != http.StatusBadRequest {
		t.Fatalf("sending nothing answered %d", code)
	}
	if _, err := d.st.Get(task.ID); err != nil {
		t.Fatal(err)
	}
	pending, err := d.st.PendingMessages(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatal("an empty note queued a message")
	}
}

// Whitespace is not something to say.
func TestWhitespaceIsNotANote(t *testing.T) {
	d := testDaemon(t)
	task := noteCard(t, d, "note-5")
	if err := d.st.SetNote(task.ID, "   \n\t "); err != nil {
		t.Fatal(err)
	}
	if _, code := sendNote(t, d, task.ID); code != http.StatusBadRequest {
		t.Fatalf("whitespace was sent, answering %d", code)
	}
}

// Writing a note is you thinking, not the session doing anything. Bumping the
// activity clock would move a silent card to the top of an activity-sorted
// list, which is the same reason tagging does not.
func TestWritingANoteIsNotActivity(t *testing.T) {
	d := testDaemon(t)
	task := noteCard(t, d, "note-6")

	before, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetNote(task.ID, "thinking out loud"); err != nil {
		t.Fatal(err)
	}
	after, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !after.LastActivityAt.Equal(before.LastActivityAt) {
		t.Fatal("writing a note counted as the session doing something")
	}
}

// A card that does not exist is a refusal, not a panic.
func TestSendingANoteOnAMissingCardIsRefused(t *testing.T) {
	d := testDaemon(t)
	if _, code := sendNote(t, d, "no-such-card"); code != http.StatusNotFound {
		t.Fatalf("answered %d", code)
	}
}
