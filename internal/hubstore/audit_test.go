package hubstore

import (
	"testing"
	"time"
)

// The audit log is read newest first and filterable, because the two questions
// people ask are "what happened to THAT machine" and "show me every X". A filter
// that leaked the wrong room, or an order that was not newest first, would make
// the pane lie without erroring.
func TestAuditWhereFiltersAndOrdersNewestFirst(t *testing.T) {
	s := open(t)

	// Increasing time per write, so the order under test is the order asserted
	// rather than a tie broken by id.
	base := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	var tick int
	old := now
	now = func() time.Time { tick++; return base.Add(time.Duration(tick) * time.Second) }
	t.Cleanup(func() { now = old })

	s.Log(&Room{Name: "alpha"}, "room-attached", "first")
	s.Log(nil, "hub-started", "a hub-level line")
	s.Log(&Room{Name: "beta"}, "room-attached", "second")
	s.Log(&Room{Name: "alpha"}, "room-detached", "the room hung up")

	// Everything, newest first.
	all, err := s.AuditWhere(0, "", "")
	if err != nil {
		t.Fatalf("AuditWhere: %v", err)
	}
	if len(all) != 4 {
		t.Fatalf("want 4 rows, got %d", len(all))
	}
	if all[0].Kind != "room-detached" || all[len(all)-1].Kind != "room-attached" {
		t.Fatalf("not newest first: %q .. %q", all[0].Kind, all[len(all)-1].Kind)
	}

	// One room, and it is the folded name, so Alpha finds alpha.
	only, err := s.AuditWhere(0, "Alpha", "")
	if err != nil {
		t.Fatalf("AuditWhere room: %v", err)
	}
	if len(only) != 2 {
		t.Fatalf("want 2 rows for alpha, got %d", len(only))
	}
	for _, e := range only {
		if e.RoomName != "alpha" {
			t.Fatalf("room filter leaked %q", e.RoomName)
		}
	}

	// One kind, across rooms and the hub-level line alike.
	attach, err := s.AuditWhere(0, "", "room-attached")
	if err != nil {
		t.Fatalf("AuditWhere kind: %v", err)
	}
	if len(attach) != 2 {
		t.Fatalf("want 2 room-attached rows, got %d", len(attach))
	}

	// Room and kind together.
	both, err := s.AuditWhere(0, "alpha", "room-detached")
	if err != nil {
		t.Fatalf("AuditWhere room+kind: %v", err)
	}
	if len(both) != 1 || both[0].Detail != "the room hung up" {
		t.Fatalf("want the one alpha detach, got %d rows", len(both))
	}
}

// The log is bounded. A fleet that attaches and detaches all day cannot grow the
// file forever, so a write past the cap drops the oldest.
func TestAuditIsTrimmedToTheCap(t *testing.T) {
	s := open(t)

	base := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	var tick int
	old := now
	now = func() time.Time { tick++; return base.Add(time.Duration(tick) * time.Millisecond) }
	t.Cleanup(func() { now = old })

	over := auditCap + 25
	for i := 0; i < over; i++ {
		s.Log(nil, "hub-started", "line")
	}

	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM room_audit`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != auditCap {
		t.Fatalf("want the table trimmed to %d, got %d", auditCap, count)
	}
}
