//go:build integration

package daemon

import (
	"net/http"
	"strings"
	"testing"
)

// wake=true goes on to the hub with the message, so the room that holds the card resumes it.
func TestAWakeSayToAnotherRoomAsksTheHubToResumeIt(t *testing.T) {
	d, f := roomDaemon(t)
	peerCard(t, d, "sa1")

	raw := map[string]any{"from": "sa1", "to": "orch@claude-sg4", "text": "wake up", "wake": true}
	code, out := sayAny(t, d, raw)
	if code != http.StatusOK || out["delivered"] != "queued" {
		t.Fatalf("%d %+v", code, out)
	}
	if got := f.says(); len(got) != 1 || !got[0].Wake {
		t.Fatalf("relay got %+v, want one say with Wake", got)
	}

	if _, _ = sayAny(t, d, map[string]any{"from": "sa1", "to": "orch@claude-sg4", "text": "plain"}); f.says()[1].Wake {
		t.Fatal("a say without wake asked for one")
	}
}

// A held message carries no wake, so a say that asks for one is refused when the hub cannot be
// reached, not held and later delivered to a card that is still parked.
func TestAWakeSayToAnUnreachableRoomIsRefusedNotHeld(t *testing.T) {
	d, f := roomDaemon(t)
	peerCard(t, d, "sa1")
	for _, down := range []func(RelaySay) (RelayResult, error){
		func(RelaySay) (RelayResult, error) { return RelayResult{}, ErrRelayDown },
		func(RelaySay) (RelayResult, error) {
			return RelayResult{Unreachable: true, Code: 503, Error: "the room claude-sg4 is not attached"}, nil
		},
	} {
		f.set(down)
		code, out := sayAny(t, d, map[string]any{"from": "sa1", "to": "orch@claude-sg4", "text": "x", "wake": true})
		if code != http.StatusFailedDependency || !strings.Contains(out["error"].(string), "claude-sg4") {
			t.Fatalf("%d %+v, want a 424 naming the room", code, out)
		}
		if len(owed(t, d)) != 0 {
			t.Fatal("a wake say was held")
		}
	}
}

// An unconfirmed wake say is reported as unconfirmed, and not held, so it is not repeated.
func TestAnUnconfirmedWakeSayIsNotHeld(t *testing.T) {
	d, f := roomDaemon(t)
	peerCard(t, d, "sa1")
	f.set(func(RelaySay) (RelayResult, error) { return RelayResult{Unconfirmed: true, Error: "no answer"}, nil })
	_, out := sayAny(t, d, map[string]any{"from": "sa1", "to": "orch@claude-sg4", "text": "x", "wake": true})
	if out["delivered"] != "unconfirmed" || len(owed(t, d)) != 0 {
		t.Fatalf("%+v", out)
	}
}

// L1 of the review. An older hub drops the wake and the target answers `parked`. The sender is told
// the hub is too old, never to send again with wake=true, which would loop.
func TestAWakeSayAnsweredParkedMeansTheHubIsOld(t *testing.T) {
	d, f := roomDaemon(t)
	peerCard(t, d, "sa1")
	f.set(func(s RelaySay) (RelayResult, error) {
		return RelayResult{OK: true, Delivered: "parked", Warning: "not sent: this card is parked, send again with wake=true",
			To: s.To + "@" + s.Room, Card: s.Room + "~L1"}, nil
	})
	code, out := sayAny(t, d, map[string]any{"from": "sa1", "to": "orch@claude-sg4", "text": "x", "wake": true})
	msg, _ := out["error"].(string)
	if code != http.StatusFailedDependency || !strings.Contains(msg, "older than cross-room wake") ||
		strings.Contains(msg, "send again") {
		t.Fatalf("%d %+v", code, out)
	}

	// Without wake a parked answer is the target's own, and passes through as it always did.
	code, out = sayAny(t, d, map[string]any{"from": "sa1", "to": "orch@claude-sg4", "text": "x"})
	if code != http.StatusOK || out["delivered"] != "parked" {
		t.Fatalf("%d %+v", code, out)
	}
}
