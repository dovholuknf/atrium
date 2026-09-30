package daemon

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// What an automatic run leaves in the card's record, and how it sits beside the other things
// that act on a card. See design section 9.

// A run's start and result are `notified` events by auto-new-context, its prompts are
// `prompted` from new-context, and a person's name is on none of them.
func TestAutoContextEventsAttribution(t *testing.T) {
	r := newAutoRig(t, store.AutoNewContextTagged, AutoContextTag, OriginAgentTag)
	r.size(bigK)
	autoQuiet()
	r.wantStart("past the line")
	r.cycle("conv-2", 20)
	r.tick()

	until(t, "the result event", func() bool {
		for _, e := range r.notified(autoContextBy) {
			if e["done"] == true {
				return true
			}
		}
		r.tick()
		return false
	})
	var started, done map[string]any
	for _, e := range r.notified(autoContextBy) {
		if e["started"] == true {
			started = e
		}
		if e["done"] == true {
			done = e
		}
	}
	if started == nil || started["tokens"] != float64(bigK*1000) || started["threshold"] == nil {
		t.Fatalf("start event %v", started)
	}
	if done == nil || done["before"] != float64(bigK*1000) || done["after"] != float64(20_000) {
		t.Fatalf("result event %v", done)
	}

	evs, err := r.d.st.Events(r.id(), 500)
	if err != nil {
		t.Fatal(err)
	}
	prompts := 0
	for _, e := range evs {
		if e.Kind != store.EventPrompted {
			continue
		}
		prompts++
		p := string(e.Payload)
		if !strings.Contains(p, `"from":"`+newContextBy+`"`) {
			t.Fatalf("a prompt not from %s: %s", newContextBy, p)
		}
	}
	if prompts < 2 {
		t.Fatalf("%d prompts recorded, want the capture and the wake at least", prompts)
	}
}

// The chip an automatic run puts on the board says so, with the size and the line.
func TestAutoContextChipMarkedAuto(t *testing.T) {
	r := newAutoRig(t, store.AutoNewContextTagged, AutoContextTag, OriginAgentTag)
	r.size(bigK)
	autoQuiet()
	r.wantStart("past the line")
	v, _ := r.d.newContextFor(r.id()).(map[string]any)
	if v == nil || v["auto"] != true || v["tokens"] != int64(bigK*1000) || v["threshold"] == nil {
		t.Fatalf("the chip is %v, want auto with tokens and threshold", v)
	}
	// A person's own press is not marked.
	h := newAutoRig(t, store.AutoNewContextOff)
	if err := h.d.StartNewContext(h.id()); err != nil {
		t.Fatal(err)
	}
	if v, _ := h.d.newContextFor(h.id()).(map[string]any); v == nil || v["auto"] == true {
		t.Fatalf("a person's press is marked auto: %v", v)
	}
}

// A say sent while a run is under way is held and told queued, and typed after the wake. A
// director's worker report is one such say and is not lost.
func TestAutoContextHoldsMessagesThenDeliversAfterWake(t *testing.T) {
	r := newAutoRig(t, store.AutoNewContextTagged, AutoContextTag, OriginAgentTag)
	peerCard(t, r.d, "alice")
	r.size(bigK)
	autoQuiet()
	r.wantStart("past the line")
	r.capturePrompted()
	if !r.d.holdingMessages(r.id()) {
		t.Fatal("not holding during an automatic run")
	}
	out := sayViaMessage(t, r.d, "alice", r.id(), heldSay)
	if out["delivered"] != "queued" || out["warning"] != newContextHoldNote {
		t.Fatalf("answer while holding: %v", out)
	}
	// A worker's report to its director, the launcher of this card's worker.
	worker := peerCard(t, r.d, "worker")
	if err := r.d.st.SetLineage(worker.ID, "cycler", r.id()); err != nil {
		t.Fatal(err)
	}
	report := sayViaMessage(t, r.d, "worker", r.id(), "WORKERREPORT")
	if report["delivered"] != "queued" {
		t.Fatalf("a worker report during the run: %v", report)
	}
	if strings.Contains(r.f.written(), heldSay) || strings.Contains(r.f.written(), "WORKERREPORT") {
		t.Fatalf("a say was typed during the run: %q", r.f.written())
	}

	r.cycle("conv-2", 20)
	until(t, "the held says", func() bool {
		return strings.Contains(r.f.written(), heldSay) && strings.Contains(r.f.written(), "WORKERREPORT")
	})
	got := r.f.written()
	wake := newContextWake(HandoffName(r.task))
	if strings.Index(got, heldSay) < strings.Index(got, wake) || strings.Index(got, "WORKERREPORT") < strings.Index(got, wake) {
		t.Fatalf("a held say went ahead of the wake prompt: %q", got)
	}
	if r.d.holdingMessages(r.id()) {
		t.Fatal("still holding after the wake")
	}
}

// With both due, exactly one begins and the other waits, and the cycle's own prompts do not
// move the idle clock.
func TestAutoContextIdleParkDoesNotFight(t *testing.T) {
	quickPark(t)
	r := newAutoRig(t, store.AutoNewContextTagged, AutoContextTag, OriginAgentTag)
	if err := r.d.st.SetSetting(store.SettingIdleParkAfter, "60"); err != nil {
		t.Fatal(err)
	}
	r.size(bigK)
	autoQuiet()
	base := r.d.idleSince(r.fresh())
	r.wantStart("past the line")

	// The idle park is due as well: it finds the card held by the run and leaves it.
	r.d.parkIdle(idleAfter(3 * time.Hour))
	if settlesTo(t, r.d, r.id(), false) {
		t.Fatal("the idle park took a card in the middle of its automatic run")
	}
	if n := r.count(capturePromptText); n != 1 {
		t.Fatalf("%d capture prompts with both due, want the one", n)
	}

	// Its own prompts leave the idle clock where it was.
	r.captureTurn()
	if got := r.d.idleSince(r.fresh()); !got.Equal(base) {
		t.Fatalf("the cycle moved the idle clock from %v to %v", base, got)
	}
}

// The keepalive does not refresh a cache the run is about to throw away.
func TestAutoContextKeepaliveSkipsWhileHolding(t *testing.T) {
	r := newAutoRig(t, store.AutoNewContextTagged, AutoContextTag, OriginAgentTag)
	r.size(bigK)
	autoQuiet()
	r.wantStart("past the line")
	v := r.d.ka.decide(r.fresh(), &store.KeepaliveCard{State: store.KeepaliveOn})
	if v.act != "skip" || v.why != "new context running" {
		t.Fatalf("the keepalive verdict during a run is %+v", v)
	}
}

// A worker mid run is not culled, and is told to try again after.
func TestAutoContextCullRefusedWhileHolding(t *testing.T) {
	r := newAutoRig(t, store.AutoNewContextTagged, AutoContextTag, OriginAgentTag, SubagentTag)
	r.size(bigK)
	autoQuiet()
	r.wantStart("past the line")
	_, err := r.d.Cull(r.id(), "")
	if !errors.Is(err, errCullNewContext) {
		t.Fatalf("cull during a run: %v, want the new context refusal", err)
	}
}

// An exit that wins the race ends the terminal under the run, which then fails cleanly: no
// /clear over a handoff nobody wrote, and a chip that says why.
func TestAutoContextCullExitFailsRunCleanly(t *testing.T) {
	r := newAutoRig(t, store.AutoNewContextTagged, AutoContextTag, OriginAgentTag)
	r.size(bigK)
	autoQuiet()
	r.wantStart("past the line")
	r.capturePrompted()
	endRunner(r.d, r.id())
	if reason := r.failed(); reason == "" {
		t.Fatal("the run did not fail")
	}
	time.Sleep(30 * time.Millisecond)
	if strings.Contains(r.f.written(), "/clear") {
		t.Fatalf("typed /clear after the terminal went: %q", r.f.written())
	}
	if r.d.holdingMessages(r.id()) {
		t.Fatal("a failed run is still holding messages")
	}
}

// The launcher's earlier notice, for a card the run has not reached, says atrium will cycle it.
func TestSa87NoticeWordingOnSubjectCard(t *testing.T) {
	r := newAutoRig(t, store.AutoNewContextTagged, AutoContextTag, OriginAgentTag)
	if err := r.d.st.SetSetting(store.SettingAutoNewContextK, "400"); err != nil {
		t.Fatal(err)
	}
	r.size(160)
	autoQuiet()
	r.wantNone("under the automatic line")
	got := contextNotices(t, r.d, r.launcher.ID)
	if len(got) != 1 || !strings.Contains(got[0].Text, "is at 160k context and atrium will cycle its context at 400k, so it needs no action.") {
		t.Fatalf("the notice reads %v", got)
	}

	// A card that is not a subject keeps the old wording.
	o := newAutoRig(t, store.AutoNewContextOff, OriginAgentTag)
	o.size(160)
	o.tick()
	got = contextNotices(t, o.d, o.launcher.ID)
	if len(got) != 1 || !strings.Contains(got[0].Text, "Tell it to report what it has and stop") {
		t.Fatalf("the notice for a card off the setting reads %v", got)
	}
}
