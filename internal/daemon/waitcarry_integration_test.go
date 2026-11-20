//go:build integration

package daemon

import (
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

func waitingSince(t *testing.T, d *Daemon, id string) time.Time {
	t.Helper()
	g, err := d.st.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if g.WaitingSince == nil {
		t.Fatalf("no waiting_since in %s", g.Status)
	}
	return *g.WaitingSince
}

func nextMilli(t *testing.T, after time.Time) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !time.Now().Truncate(time.Millisecond).After(after) {
		if time.Now().After(deadline) {
			t.Fatal("the clock did not move")
		}
	}
}

func carriedWaitCard(t *testing.T, d *Daemon, name string) (*store.Task, *runner, time.Time) {
	t.Helper()
	card := peerCard(t, d, name)
	r, _ := typedRunner(t, d, card.ID)
	for _, s := range []string{store.StatusNeedsInput, store.StatusDead, store.StatusNeedsInput} {
		if err := d.st.SetStatus(card.ID, s); err != nil {
			t.Fatal(err)
		}
	}
	return card, r, waitingSince(t, d, card.ID)
}

func peerTyped(r *runner, cause string) {
	r.typeMu.Lock()
	r.peerSent, r.peerCause = time.Now(), cause
	r.typeMu.Unlock()
}

// A card that came back from a deploy and is then driven by says must not keep the wait it had before the deploy.
func TestSayTurnEndsTheCarriedWait(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	card, r, before := carriedWaitCard(t, d, "orch-say")
	nextMilli(t, before)
	peerTyped(r, store.UsageSay)
	activityPrompt(t, d, card)
	d.turnEnded(card.ID)
	if got := waitingSince(t, d, card.ID); !got.After(before) {
		t.Fatalf("a say turn kept the carried wait: %v, want after %v", got, before)
	}
}

func TestRestartWakeTurnKeepsTheCarriedWait(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	card, r, before := carriedWaitCard(t, d, "orch-wake")
	nextMilli(t, before)
	peerTyped(r, store.UsageRestartWake)
	activityPrompt(t, d, card)
	d.turnEnded(card.ID)
	if got := waitingSince(t, d, card.ID); !got.Equal(before) {
		t.Fatalf("a restart wake reset the carried wait: %v, want %v", got, before)
	}
}
