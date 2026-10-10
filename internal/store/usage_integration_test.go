//go:build integration

package store

import (
	"testing"
	"time"
)

// A card's usage rows come back newest first, sum by cause, and outlive the
// card, which is the point of keeping them.
func TestSessionUsageKeepsHistory(t *testing.T) {
	s := openTestStore(t)
	card, _, err := s.Register(Observed{WireName: "spender", Worktree: "/w"})
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	rows := []*SessionUsage{
		{TaskID: card.ID, ResumeID: "s1", Started: base, Ended: base.Add(time.Minute), Cause: UsageOperator,
			Replies: 3, Input: 6, Output: 300, CacheWrite1h: 1000, CacheRead: 9000, Context: 10006,
			LastMessage: "m3", Cost: 0.01},
		{TaskID: card.ID, ResumeID: "s1", Started: base.Add(time.Hour), Ended: base.Add(time.Hour),
			Cause: UsageKeepalive, Replies: 1, Input: 3, Output: 1, CacheRead: 10006, Cost: 0.002},
		{TaskID: card.ID, ResumeID: "s1", Started: base.Add(2 * time.Hour), Ended: base.Add(2*time.Hour + time.Minute),
			Cause: UsageResume, AfterResume: true, Replies: 1, Input: 2, Output: 50, CacheWrite1h: 10006,
			Context: 10008, LastMessage: "m4", Cost: 0.08},
	}
	for _, r := range rows {
		if err := s.AddSessionUsage(r); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.SessionUsageOf(card.ID, 0)
	if err != nil || len(got) != 3 {
		t.Fatalf("rows %d (%v), want 3", len(got), err)
	}
	if got[0].Cause != UsageResume || !got[0].AfterResume || got[0].CacheWrite1h != 10006 {
		t.Fatalf("newest row %+v", got[0])
	}
	all, byCause, err := s.SessionUsageTotals(card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if all.Rows != 3 || all.Replies != 5 || all.CacheWrite1h != 11006 || all.CacheRead != 19006 {
		t.Fatalf("totals %+v", all)
	}
	if byCause[UsageKeepalive] == nil || byCause[UsageKeepalive].CacheRead != 10006 {
		t.Fatalf("keep-alive total %+v", byCause[UsageKeepalive])
	}

	// The daemon's restart point skips keep-alive rows, which are not in the
	// transcript.
	last, err := s.LastTranscriptUsage(card.ID, "s1")
	if err != nil || last == nil || last.LastMessage != "m4" {
		t.Fatalf("last transcript row %+v (%v)", last, err)
	}
	if none, _ := s.LastTranscriptUsage(card.ID, "other"); none != nil {
		t.Fatalf("another session's row %+v", none)
	}

	if _, err := s.db.Exec(`DELETE FROM task WHERE id = ?`, card.ID); err != nil {
		t.Fatal(err)
	}
	if kept, _ := s.SessionUsageOf(card.ID, 0); len(kept) != 3 {
		t.Fatalf("a forgotten card kept %d rows, want 3", len(kept))
	}
}

// Re-running the migration is harmless.
func TestSessionUsageMigrationReruns(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.db.Exec(`DELETE FROM schema_migration WHERE name = '0063_session_usage'`); err != nil {
		t.Fatal(err)
	}
	if err := s.migrate(); err != nil {
		t.Fatal(err)
	}
}
