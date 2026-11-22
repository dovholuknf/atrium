//go:build integration

package hubstore

import "testing"

func TestCapabilityLatestIsTheNewestPerRoomAndCap(t *testing.T) {
	s := open(t)
	row := func(state, reason, at string) CapabilityRow {
		return CapabilityRow{Room: "room-d", Cap: "go", At: at, State: state, Reason: reason}
	}
	for _, r := range []CapabilityRow{row("fail", "not on PATH", "2026-11-16T10:00:00Z"), row("pass", "ok", "2026-11-16T11:00:00Z"),
		row("pass", "ok", "2026-11-16T12:00:00Z")} {
		if err := s.CapabilityPut([]CapabilityRow{r}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.CapabilityLatest()
	if err != nil || len(got) != 1 || got[0].State != "pass" || got[0].At != "2026-11-16T12:00:00Z" {
		t.Fatalf("%+v %v", got, err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM capability_result`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("a repeat result grew the table: %d rows %v", n, err)
	}
}

func TestCapabilityResultIsPassFailOrUnknown(t *testing.T) {
	s := open(t)
	if err := s.CapabilityPut([]CapabilityRow{{Room: "r", Cap: "c", At: "2026-11-16T10:00:00Z", State: "green"}}); !IsRefusal(err) {
		t.Fatalf("%v", err)
	}
}
