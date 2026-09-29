package store

import (
	"testing"
	"time"
)

// Lineage is written once. A reopen runs the launch again, and must not
// rename the parent to whoever pressed the button the second time.
func TestLineageIsWrittenOnce(t *testing.T) {
	s := openTestStore(t)
	card, _, err := s.Register(Observed{WireName: "kid", Worktree: "/tmp/kid"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetLineage(card.ID, "orchestrator", "parent-id"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLineage(card.ID, HumanLauncher, ""); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SpawnedBy != s.Qualify("orchestrator") || got.SpawnedByID != "parent-id" {
		t.Fatalf("lineage = %q / %q, want the first launcher kept and qualified", got.SpawnedBy, got.SpawnedByID)
	}
	if !got.Launched() {
		t.Fatal("a card another session launched does not say so")
	}
}

// The board's own dialog is the human, and a human-launched card owes nobody
// a report.
func TestAHumanLaunchIsNotAnAgentLaunch(t *testing.T) {
	s := openTestStore(t)
	card, _, _ := s.Register(Observed{WireName: "mine", Worktree: "/tmp/mine"})
	if err := s.SetLineage(card.ID, HumanLauncher, ""); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(card.ID)
	if got.SpawnedBy != HumanLauncher || got.Launched() {
		t.Fatalf("spawned_by = %q, launched = %v", got.SpawnedBy, got.Launched())
	}
}

// A prompt makes a report owed, and a report after it pays it.
func TestAPromptOwesAReportAndAReportPaysIt(t *testing.T) {
	s := openTestStore(t)
	card, _, _ := s.Register(Observed{WireName: "w", Worktree: "/tmp/w"})
	got, _ := s.Get(card.ID)
	if got.OwesReport() {
		t.Fatal("a card never prompted owes a report")
	}
	if err := s.AppendEvent(card.ID, EventPrompted, map[string]any{"text": "go", "via": "launch"}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(card.ID)
	if got.PromptedAt == nil || !got.OwesReport() {
		t.Fatalf("a prompted card does not owe a report: prompted_at %v", got.PromptedAt)
	}
	if err := s.MarkReported(card.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(card.ID)
	if got.OwesReport() {
		t.Fatal("a report did not pay what the prompt made owed")
	}
}

// One notice per worker, source and key, however often it is asked for.
func TestANoticeIsRecordedOnce(t *testing.T) {
	s := openTestStore(t)
	first, err := s.RecordNotice("w", "silent-stop", "k1")
	if err != nil || !first {
		t.Fatalf("first = %v, %v", first, err)
	}
	again, err := s.RecordNotice("w", "silent-stop", "k1")
	if err != nil || again {
		t.Fatalf("again = %v, %v", again, err)
	}
	other, err := s.RecordNotice("w", "silent-stop", "k2")
	if err != nil || !other {
		t.Fatalf("a new key was deduplicated: %v, %v", other, err)
	}
}

// A hook is stamped the first time it is heard from and not again.
func TestAHookIsStampedOnce(t *testing.T) {
	s := openTestStore(t)
	card, _, _ := s.Register(Observed{WireName: "h", Worktree: "/tmp/h"})
	if err := s.SawHook(card.ID, HookTool); err != nil {
		t.Fatal(err)
	}
	first, _ := s.Get(card.ID)
	if first.ToolHookSeenAt == nil || first.StopHookSeenAt != nil {
		t.Fatalf("tool %v stop %v", first.ToolHookSeenAt, first.StopHookSeenAt)
	}
	if err := s.SawHook(card.ID, HookTool); err != nil {
		t.Fatal(err)
	}
	second, _ := s.Get(card.ID)
	if !second.ToolHookSeenAt.Equal(*first.ToolHookSeenAt) {
		t.Fatal("a second sighting moved the first")
	}
}

// A turn ends when a working card starts waiting. A resume from done, and a
// prompt that changes no status, are not turns. See 0062_turn_end.
func TestOnlyAWorkingCardEndsATurn(t *testing.T) {
	s := openTestStore(t)
	card, _, _ := s.Register(Observed{WireName: "w", Worktree: "/tmp/w"})
	for _, st := range []string{StatusDone, StatusNeedsInput} {
		if err := s.SetStatus(card.ID, st); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AppendEvent(card.ID, EventPrompted, map[string]any{"text": "/model x", "via": "launch"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.TurnEndedAt(card.ID); got != nil {
		t.Fatalf("a resume and a slash command recorded a turn end at %s", got)
	}
	for _, st := range []string{StatusRunning, StatusNeedsInput} {
		if err := s.SetStatus(card.ID, st); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := s.TurnEndedAt(card.ID); got == nil {
		t.Fatal("a turn that ran recorded no end")
	}
}

// A database from before 0062 is seeded from its event log, so a card that
// really stopped silently before the upgrade still reads stuck after it.
func TestTheTurnEndIsSeededFromTheEventLog(t *testing.T) {
	s := openTestStore(t)
	card, _, _ := s.Register(Observed{WireName: "w", Worktree: "/tmp/w"})
	for _, st := range []string{StatusRunning, StatusNeedsInput} {
		if err := s.SetStatus(card.ID, st); err != nil {
			t.Fatal(err)
		}
	}
	want, _ := s.TurnEndedAt(card.ID)
	if _, err := s.db.Exec(`DELETE FROM turn_end`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`DELETE FROM schema_migration WHERE name = '0062_turn_end'`); err != nil {
		t.Fatal(err)
	}
	if err := s.migrate(); err != nil {
		t.Fatal(err)
	}
	got, err := s.TurnEndedAt(card.ID)
	// The event is stamped a moment after the status write, so within a second.
	if err != nil || got == nil || want == nil || got.Sub(*want).Abs() > time.Second {
		t.Fatalf("seeded turn end %v, want %v (%v)", got, want, err)
	}
}
