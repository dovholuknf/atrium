package link

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dovholuknf/atrium/internal/capability"
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

func TestCapabilitiesListPassesItsOutputSchema(t *testing.T) {
	ctx := context.Background()
	s := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil)
	AddRoomCapabilityTool(s, func(_ context.Context, _, _ string, out any) error {
		v := out.(*CapabilityView)
		v.Caps = []json.RawMessage{json.RawMessage(`{"id":"ssh:x","kind":"reach"}`), json.RawMessage(`{"id":"go","kind":"toolchain"}`)}
		return nil
	})
	st, ct := mcp.NewInMemoryTransports()
	if _, err := s.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "atrium_capabilities", Arguments: map[string]any{"action": "list", "kind": "reach"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("list failed: %+v", res.Content[0])
	}
	b, _ := json.Marshal(res.StructuredContent)
	if !strings.Contains(string(b), `"caps":[{"id":"ssh:x","kind":"reach"}]`) {
		t.Fatalf("%s", b)
	}
}

func TestLaunchNeedsPlacesGPUWorkFromHardware(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	at := now.Add(-time.Hour).Format(time.RFC3339)
	v := CapabilityView{Rooms: []CapabilityRoom{
		{Name: "a", Online: true, Hardware: &capability.Hardware{At: at, GPUs: []capability.GPU{{Name: "small", VRAM: 8 << 30, API: "cuda"}}}},
		{Name: "b", Online: true, Hardware: &capability.Hardware{At: at, GPUs: []capability.GPU{{Name: "big", VRAM: 24 << 30, API: "cuda"}}}},
		{Name: "c", Online: true},
	}}
	need := []string{"gpu:cuda", "vram>=12g"}
	if got, err := roomForNeeds(v, need, "a", false, now); err != nil || got != "b" {
		t.Fatalf("gpu work went to %q, %v", got, err)
	}
	_, err := roomForNeeds(v, need, "a", true, now)
	if err == nil || !strings.Contains(err.Error(), "a: vram>=12g fail: vram 8g") {
		t.Fatalf("a named room with too little vram was not refused with its reason: %v", err)
	}
	if _, err := roomForNeeds(v, need, "c", true, now); err == nil || !strings.Contains(err.Error(), "c: gpu:cuda has no result") {
		t.Fatalf("a room with no hardware probe took gpu work: %v", err)
	}
}
