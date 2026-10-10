//go:build integration

package daemon

import (
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// waitingReasonAfter fires one Notification at a running card and returns what
// the card says about why it waits. prior, when set, is a question noted
// while the card was still running (NoteAsked).
func waitingReasonAfter(t *testing.T, kind string, prior bool) string {
	t.Helper()
	d := testDaemon(t)
	task := fleetCard(t, d, "noted")
	if err := d.st.SetStatus(task.ID, store.StatusRunning); err != nil {
		t.Fatal(err)
	}
	if prior {
		if err := d.st.NoteAsked(task.ID); err != nil {
			t.Fatal(err)
		}
	}
	d.onActivity(ActivityEvent{Agent: "noted", Event: "waiting", Notification: kind})
	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.StatusNeedsInput {
		t.Fatalf("%s: status %q, want needs-input", kind, got.Status)
	}
	return got.WaitingReason
}

// A real prompt asked: the hub's input notice reads `asked` for a card that
// has no Stop yet. See link/notify.go.
func TestARealNotificationRecordsThatTheCardAsked(t *testing.T) {
	for _, kind := range []string{"permission_prompt", "elicitation_dialog", ""} {
		if got := waitingReasonAfter(t, kind, false); got != store.WaitingAsked {
			t.Errorf("%q: waiting reason %q, want asked", kind, got)
		}
	}
}

// The prompt sitting idle for a minute asks nothing. Writing `asked` here let
// a card that never did anything raise an `input` notice (atrium-87300).
func TestAnIdlePromptDoesNotRecordAnAsk(t *testing.T) {
	if got := waitingReasonAfter(t, "idle_prompt", false); got != "" {
		t.Fatalf("an idle prompt wrote waiting reason %q", got)
	}
}

// ...and it does not erase a question the card really asked earlier.
func TestAnIdlePromptKeepsARealEarlierAsk(t *testing.T) {
	if got := waitingReasonAfter(t, "idle_prompt", true); got != store.WaitingAsked {
		t.Fatalf("an idle prompt lost the earlier ask: %q", got)
	}
}
