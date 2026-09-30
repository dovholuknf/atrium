package daemon

import (
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// Hysteresis: a card cycled once is not cycled again until it has come back small in a new
// conversation. See design section 4.

// A card that comes back still large stays FIRED, does not cycle again, and its launcher is
// told once that the handoff is probably too large.
func TestAutoContextDoesNotLoopWhenStillLarge(t *testing.T) {
	r := newAutoRig(t, store.AutoNewContextTagged, AutoContextTag, OriginAgentTag)
	r.size(bigK)
	autoQuiet()
	r.wantStart("first")
	r.cycle("conv-2", 310)

	for i := 0; i < 3; i++ {
		autoQuiet()
		r.tick()
		if s := r.d.auto.get(r.id()); s == nil || s.state != autoFired {
			t.Fatalf("tick %d left the card %+v, want it still fired", i, s)
		}
		if r.begun() {
			t.Fatalf("tick %d cycled a card that came back large", i)
		}
	}
	if n := strings.Count(r.f.written(), "Your context is about to be cleared"); n != 1 {
		t.Fatalf("the capture prompt was typed %d times", n)
	}
	var large []string
	for _, m := range r.autoNotices() {
		if strings.Contains(m, "still at 310k") {
			large = append(large, m)
		}
	}
	if len(large) != 1 || !strings.Contains(large[0], "its handoff is probably too large") {
		t.Fatalf("the launcher was told %q, want one notice that the handoff is probably too large", large)
	}
	// The result is written once, with both sizes.
	var done []map[string]any
	for _, e := range r.notified(autoContextBy) {
		if e["done"] == true {
			done = append(done, e)
		}
	}
	if len(done) != 1 || done[0]["before"] != float64(bigK*1000) || done[0]["after"] != float64(310_000) {
		t.Fatalf("result events %v", done)
	}
}

// Small in a NEW conversation re-arms it, and it fires again at the line.
func TestAutoContextRearmsAfterShrinkAndNewSession(t *testing.T) {
	r := newAutoRig(t, store.AutoNewContextTagged, AutoContextTag)
	autoTiming.minGap = 0
	r.size(bigK)
	autoQuiet()
	r.wantStart("first")
	r.cycle("conv-2", 20)

	r.wantNone("just after coming back at 20k")
	if s := r.d.auto.get(r.id()); s == nil || s.state != autoArmed || s.attempts != 0 {
		t.Fatalf("a card back at 20k in a new conversation is %+v, want armed", s)
	}
	if got := len(r.notified(autoContextBy)); got != 2 { // started, done
		t.Fatalf("%d events, want started and done", got)
	}

	// It grows to the line again and is cycled again.
	r.d.act.set(r.id(), ActivityIdle, "")
	r.size(bigK)
	autoQuiet()
	before := len(r.f.written())
	r.wantStart("when it grows back to the line")
	until(t, "the second capture prompt", func() bool { return len(r.f.written()) > before })
}

// Under half the line is not enough on its own. A /clear that did nothing leaves the card in
// the same conversation, and it stays FIRED.
func TestAutoContextStaysFiredInTheSameConversation(t *testing.T) {
	r := newAutoRig(t, store.AutoNewContextTagged, AutoContextTag)
	autoTiming.minGap = 0
	r.size(bigK)
	autoQuiet()
	r.wantStart("first")
	r.cycle("sess-cycler", 20)
	r.size(bigK)
	autoQuiet()
	r.tick()
	if s := r.d.auto.get(r.id()); s == nil || s.state != autoFired {
		t.Fatalf("a card in the conversation the cycle began in is %+v, want fired", s)
	}
	if r.begun() {
		t.Fatal("cycled again in the same conversation")
	}
}

// Two crossings inside the minimum gap fire once.
func TestAutoContextMinGap(t *testing.T) {
	r := newAutoRig(t, store.AutoNewContextTagged, AutoContextTag)
	r.size(bigK)
	autoQuiet()
	r.wantStart("first")
	r.cycle("conv-2", 20)
	r.tick() // re-armed

	r.size(bigK)
	autoQuiet()
	r.wantNone("a second crossing inside the gap")
	r.wantNone("a second crossing inside the gap, next tick")
	autoTiming.minGap = 0
	r.wantStart("once the gap is over")
}
