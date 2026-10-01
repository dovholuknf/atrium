package daemon

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// The context ceiling: a card tagged ContextCeilingTag is cycled at context_ceiling_k, mid-turn
// included, whatever the global mode says. See autocontext.go.

var directorTags = []string{OriginAgentTag, DirectorTag, ContextCeilingTag}

// ceilingBusy puts the card in the middle of a long turn.
func ceilingBusy(r *autoRig) {
	_ = r.d.st.SetStatus(r.id(), store.StatusRunning)
	r.d.act.set(r.id(), ActivityTool, "Bash")
}

func TestCeilingCardMidTurnPastCeilingCyclesAndAsksOnce(t *testing.T) {
	r := newAutoRig(t, "", directorTags...)
	ncTiming.nudgeAfter = 60 * time.Millisecond
	ceilingBusy(r)
	r.size(151)
	r.wantStart("mid-turn past the ceiling, with the global mode off")

	until(t, "the stop request", func() bool { return strings.Contains(r.f.written(), newContextStop) })
	if strings.Contains(r.f.written(), "HANDOFF.") {
		t.Fatalf("the capture was typed into a running turn: %q", r.f.written())
	}
	// The card ends its turn, and the cycle goes on through /clear and the wake.
	ncTurnEnds(r.d, r.id())
	r.captureTurn()
	r.newSession("conv-2")
	until(t, "the ceiling wake", func() bool {
		return strings.Contains(r.f.written(), newContextWakeCeiling(HandoffName(r.task)))
	})
	until(t, "the chip to go", func() bool { return r.d.newContextFor(r.id()) == nil })
	if n := strings.Count(r.f.written(), newContextStop); n != 1 {
		t.Fatalf("asked %d times, want once: %q", n, r.f.written())
	}
	got := r.autoNotices()
	if len(got) != 1 || !strings.Contains(got[0], "passed its context ceiling at 151k") {
		t.Fatalf("the launcher notice: %q", got)
	}
}

func TestCeilingCardUnderCeilingDoesNothing(t *testing.T) {
	r := newAutoRig(t, "", directorTags...)
	ceilingBusy(r)
	r.size(149)
	autoQuiet()
	r.wantNone("under the ceiling")
}

// Unchanged for a card without the tag: mid-turn past the global line does nothing.
func TestNonCeilingCardMidTurnPastGlobalKDoesNothing(t *testing.T) {
	r := newAutoRig(t, store.AutoNewContextAgents, OriginAgentTag, DirectorTag)
	_ = r.d.st.SetSetting(store.SettingAutoNewContextK, "200")
	ceilingBusy(r)
	r.size(250)
	autoQuiet()
	r.wantNone("mid-turn past the global line on a card without the tag")
}

func TestCeilingAndGlobalLineTheLowerWins(t *testing.T) {
	r := newAutoRig(t, store.AutoNewContextAgents, directorTags...)
	_ = r.d.st.SetSetting(store.SettingContextCeilingK, "300")
	_ = r.d.st.SetSetting(store.SettingAutoNewContextK, "200")
	if got := r.d.autoThreshold(r.fresh()); got != 200_000 {
		t.Fatalf("threshold %d, want the global 200000", got)
	}
	_ = r.d.st.SetSetting(store.SettingContextCeilingK, "180")
	if got := r.d.autoThreshold(r.fresh()); got != 180_000 {
		t.Fatalf("threshold %d, want the ceiling 180000", got)
	}
	// With the mode off the ceiling stands alone.
	_ = r.d.st.SetSetting(store.SettingAutoNewContext, "off")
	_ = r.d.st.SetSetting(store.SettingAutoNewContextK, "100")
	if got := r.d.autoThreshold(r.fresh()); got != 180_000 {
		t.Fatalf("threshold %d, want the ceiling 180000 with the mode off", got)
	}
}

func TestCeilingNeverBelowNoticeThreshold(t *testing.T) {
	r := newAutoRig(t, "", directorTags...)
	_ = r.d.st.SetSetting(store.SettingContextCeilingK, "50")
	_ = r.d.st.SetSetting("context_threshold_k", "170")
	if got := r.d.autoThreshold(r.fresh()); got != 170_000 {
		t.Fatalf("threshold %d, want the notice threshold 170000", got)
	}
}

func TestCeilingCardNoAutoTagWins(t *testing.T) {
	r := newAutoRig(t, "", append([]string{NoAutoContextTag}, directorTags...)...)
	ceilingBusy(r)
	r.size(400)
	autoQuiet()
	r.wantNone("on a card tagged no-auto-new-context")
}

// Every gate that protects work still holds a ceiling card, mid-turn or not.
func TestCeilingCardStillWaitsOutTheProtectingGates(t *testing.T) {
	gates := []struct {
		name  string
		on    func(r *autoRig)
		clear func(r *autoRig)
	}{
		{"a pending permission", func(r *autoRig) {
			if _, _, err := r.d.st.RecordPermission(r.id(), "Bash", "ls", "k1", ""); err != nil {
				r.t.Fatal(err)
			}
		}, func(r *autoRig) {
			ps, _ := r.d.st.PendingForTask(r.id())
			for _, p := range ps {
				if _, err := r.d.st.DecidePermission(p.ID, "approve", ""); err != nil {
					r.t.Fatal(err)
				}
			}
		}},
		{"a permission status", func(r *autoRig) { _ = r.d.st.SetStatus(r.id(), store.StatusNeedsPermission) },
			func(r *autoRig) { _ = r.d.st.SetStatus(r.id(), store.StatusRunning) }},
		{"a dialog open", func(r *autoRig) { r.d.act.dialogRaised(r.id()) },
			func(r *autoRig) { r.d.act.set(r.id(), ActivityTool, "Bash") }},
		{"subagents out", func(r *autoRig) { r.d.act.setBackground(r.id(), 1) },
			func(r *autoRig) { r.d.act.setBackground(r.id(), 0) }},
		{"background work", func(r *autoRig) { r.d.act.setBackgroundWork(r.id(), 1) },
			func(r *autoRig) { r.d.act.setBackgroundWork(r.id(), 0) }},
		{"a message held", func(r *autoRig) {
			if _, err := r.d.st.QueueMessage(r.id(), "look at this first"); err != nil {
				r.t.Fatal(err)
			}
		}, func(r *autoRig) {
			var ids []string
			for _, m := range pendingFrom(r.t, r.d, r.id()) {
				ids = append(ids, m.ID)
			}
			if err := r.d.st.MarkDelivered(r.id(), "test", ids); err != nil {
				r.t.Fatal(err)
			}
		}},
		{"a card in the same directory mid cycle", func(r *autoRig) {
			other, _, err := r.d.st.Register(store.Observed{
				WireName: "neighbour", Worktree: filepath.ToSlash(r.dir), Runner: "claude", PID: 2,
			})
			if err != nil {
				r.t.Fatal(err)
			}
			r.d.nctx.claim(other.ID, &newContext{step: NewContextCapture})
			r.t.Cleanup(func() { r.d.nctx.clear(other.ID) })
		}, nil},
	}
	for _, g := range gates {
		t.Run(g.name, func(t *testing.T) {
			r := newAutoRig(t, "", directorTags...)
			ceilingBusy(r)
			r.size(200)
			autoQuiet()
			g.on(r)
			r.wantNone("with " + g.name)
			if g.clear == nil {
				return
			}
			g.clear(r)
			r.wantStart("once " + g.name + " cleared")
		})
	}
}

// The minimum gap between cycles still applies to a ceiling card.
func TestCeilingCardHonoursTheMinimumGap(t *testing.T) {
	r := newAutoRig(t, "", directorTags...)
	r.size(200)
	autoQuiet()
	r.wantStart("past the ceiling")
	r.cycle("conv-2", 200)
	autoQuiet()
	before := len(r.f.written())
	r.tick()
	r.tick()
	if r.begun() || len(r.f.written()) != before {
		t.Fatalf("a second cycle started inside the gap: %q", r.f.written()[before:])
	}
}
