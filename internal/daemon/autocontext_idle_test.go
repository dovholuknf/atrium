package daemon

import (
	"testing"
	"time"
)

// A failed automatic cycle puts back the idle handoff it found, and one that got
// through leaves its own. r-new-review-25e40102 item 5.
func TestAutoCycleKeepsTheIdleHandoffItFound(t *testing.T) {
	r := newAutoRig(t, "")
	// Ended now: a mark older than the card's last activity is stale and idleSince drops it.
	base, took := time.Now().Add(-time.Hour), time.Now()
	r.d.idle.put(r.id(), &handoffMark{base: base, end: took, written: true, file: "HANDOFF.x.md"})

	r.d.autoIdleHold(r.task)
	if m := r.d.idle.get(r.id()); m == nil || !m.capturing {
		t.Fatalf("hold = %+v, want a capturing mark", m)
	}
	r.d.autoIdleRelease(r.id(), false)
	m := r.d.idle.get(r.id())
	if m == nil || m.capturing || !m.written || !m.end.Equal(took) || m.file != "HANDOFF.x.md" {
		t.Fatalf("after a failed cycle the mark is %+v, want the handoff idle parking took", m)
	}

	r.d.autoIdleHold(r.task)
	r.d.autoIdleRelease(r.id(), true)
	if m := r.d.idle.get(r.id()); m == nil || m.capturing || !m.written || m.prev != nil || m.end.Before(took) {
		t.Fatalf("after a cycle that got through the mark is %+v, want its own", m)
	}
}

func TestAutoCycleWithNoIdleHandoffLeavesNothingOnFailure(t *testing.T) {
	r := newAutoRig(t, "")
	r.d.autoIdleHold(r.task)
	r.d.autoIdleRelease(r.id(), false)
	if m := r.d.idle.get(r.id()); m != nil {
		t.Fatalf("mark = %+v, want none", m)
	}
}
