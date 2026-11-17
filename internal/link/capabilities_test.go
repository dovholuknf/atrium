package link

import (
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

func TestCapabilityWhichNeedsEveryNeedFreshAndOnline(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	row := func(room, cap, state string, ago time.Duration) hubstore.CapabilityRow {
		return hubstore.CapabilityRow{Room: room, Cap: cap, State: state, At: now.Add(-ago).Format(time.RFC3339)}
	}
	v := CapabilityView{
		Rooms: []CapabilityRoom{{Name: "a", Online: true}, {Name: "b", Online: true}, {Name: "c", Online: false}, {Name: "d", Online: true}},
		Results: []hubstore.CapabilityRow{
			row("a", "ssh:x", "pass", time.Hour), row("a", "build:y", "pass", time.Hour),
			row("b", "ssh:x", "pass", time.Hour), row("b", "build:y", "fail", time.Hour),
			row("c", "ssh:x", "pass", time.Hour), row("c", "build:y", "pass", time.Hour),
			row("d", "ssh:x", "pass", 30*time.Hour), row("d", "build:y", "pass", time.Hour),
		},
	}
	got := CapabilityWhich(v, []string{"ssh:x", "build:y"}, 24*time.Hour, now)
	if len(got) != 3 || got[0].Room != "a" || !got[0].OK {
		t.Fatalf("%+v", got)
	}
	for _, m := range got[1:] {
		if m.OK {
			t.Errorf("%s is OK though it is %s", m.Room, map[bool]string{true: "offline", false: "stale"}[m.Room == "c"])
		}
	}
	if m := CapabilityWhich(v, []string{"ssh:x", "missing"}, 24*time.Hour, now); len(m) != 0 {
		t.Fatalf("a never-run need matched: %+v", m)
	}
}

func TestLaunchNeedsPlacesOnlyOnARoomThatPasses(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	row := func(room, cap, state, reason string) hubstore.CapabilityRow {
		return hubstore.CapabilityRow{Room: room, Cap: cap, State: state, Reason: reason, At: now.Add(-time.Hour).Format(time.RFC3339)}
	}
	v := CapabilityView{
		Rooms: []CapabilityRoom{{Name: "a", Online: true}, {Name: "b", Online: true}, {Name: "c"}},
		Results: []hubstore.CapabilityRow{
			row("a", "ssh:x", "fail", "timeout"), row("b", "ssh:x", "pass", ""), row("c", "ssh:x", "pass", ""),
		},
	}
	need := []string{"ssh:x"}
	if got, err := roomForNeeds(v, need, "a", false, now); err != nil || got != "b" {
		t.Fatalf("an unnamed launch went to %q, %v", got, err)
	}
	if got, err := roomForNeeds(v, need, "b", false, now); err != nil || got != "b" {
		t.Fatalf("the launcher's own passing room was not kept: %q, %v", got, err)
	}
	_, err := roomForNeeds(v, need, "a", true, now)
	if err == nil || !strings.Contains(err.Error(), "a: ssh:x fail: timeout") || strings.Contains(err.Error(), "b:") {
		t.Fatalf("a named room that fails was not refused with its reason: %v", err)
	}
	_, err = roomForNeeds(v, need, "c", true, now)
	if err == nil || !strings.Contains(err.Error(), "c: not attached") {
		t.Fatalf("an offline room took a launch: %v", err)
	}
	if _, err := roomForNeeds(v, []string{"ssh:x", "gone"}, "", false, now); err == nil || !strings.Contains(err.Error(), "gone has no result") {
		t.Fatalf("a need nobody ran placed a launch: %v", err)
	}
}
