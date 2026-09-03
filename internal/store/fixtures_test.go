package store

import (
	"testing"
)

// A fixture is a definition of what to start, so the ordinary things apply:
// it survives a restart, it can be turned off without being retyped, and it
// comes back in the order somebody chose.
func TestFixtureRoundTrip(t *testing.T) {
	s := open(t)

	f, err := s.SaveFixture(&Fixture{
		Label: "dotfiles", Harness: "claude", Cwd: "D:/git/dotfiles",
		Resume: true, Enabled: true, Sort: 0, Theme: "tangent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.ID == "" {
		t.Fatal("saving did not mint an id")
	}

	got, err := s.Fixtures()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d fixtures, wanted 1", len(got))
	}
	if got[0].Label != "dotfiles" || got[0].Theme != "tangent" ||
		!got[0].Resume || !got[0].Enabled {
		t.Fatalf("what came back is not what was saved: %+v", got[0])
	}
}

// A fixture needs something to start, and saying so beats a start that fails
// later with a stranger message.
func TestFixtureNeedsARunner(t *testing.T) {
	s := open(t)
	if _, err := s.SaveFixture(&Fixture{Label: "nope", Cwd: "D:/x"}); err == nil {
		t.Fatal("a fixture with no runner was saved")
	}
}

// Order is the whole point of `sort`: it is what "the dotfiles one is always
// first" means.
func TestFixturesComeBackInOrder(t *testing.T) {
	s := open(t)
	for _, c := range []struct {
		label string
		sort  float64
	}{{"third", 3}, {"first", 1}, {"second", 2}} {
		if _, err := s.SaveFixture(&Fixture{
			Label: c.label, Harness: "claude", Sort: c.sort, Enabled: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Fixtures()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"first", "second", "third"}
	for i, w := range want {
		if got[i].Label != w {
			t.Fatalf("position %d is %q, wanted %q", i, got[i].Label, w)
		}
	}
}

// The card a fixture started is recorded by the daemon reporting what
// happened, and an edit in the board must not clear it: losing it means the
// next start opens a second card beside the first.
func TestNoteFixtureTaskSurvivesAnEdit(t *testing.T) {
	s := open(t)
	f, err := s.SaveFixture(&Fixture{Label: "one", Harness: "claude", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.NoteFixtureTask(f.ID, "task-123"); err != nil {
		t.Fatal(err)
	}

	// An edit that does not mention the card, the way the board sends one.
	if _, err := s.SaveFixture(&Fixture{
		ID: f.ID, Label: "renamed", Harness: "claude", Enabled: true,
		CreatedAt: f.CreatedAt,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetFixture(f.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Label != "renamed" {
		t.Fatalf("the edit did not take: %+v", got)
	}
	if got.TaskID != "task-123" {
		t.Fatalf("editing forgot which card this starts onto: %q", got.TaskID)
	}
}

// Deleting a fixture is forgetting an instruction, not ending the work it
// started.
func TestDeletingAFixture(t *testing.T) {
	s := open(t)
	f, err := s.SaveFixture(&Fixture{Label: "gone", Harness: "claude", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteFixture(f.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.Fixtures()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("%d fixtures survived a delete", len(got))
	}
}

// A fixture written for a directory that already has a live card takes that
// one over. Two cards with the same name for one directory is the confusion
// this exists to prevent, and it would happen to anybody who pins a session
// and then writes a fixture for it.
func TestAdoptableTaskFindsALiveCard(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{
		WireName: "dotfiles", Worktree: "D:/git/dotfiles", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.AdoptableTask("D:/git/dotfiles")
	if err != nil {
		t.Fatal(err)
	}
	if got != task.ID {
		t.Fatalf("adopted %q, wanted %q", got, task.ID)
	}
}

// A finished card is history. Resuming onto it would rewrite what happened.
func TestAdoptableTaskIgnoresFinishedCards(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{
		WireName: "old", Worktree: "D:/git/old", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatus(task.ID, StatusDead); err != nil {
		t.Fatal(err)
	}
	got, err := s.AdoptableTask("D:/git/old")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("a dead card was offered for adoption: %q", got)
	}
}

// Nothing to adopt is the ordinary case, not a failure.
func TestAdoptableTaskWithNothingThere(t *testing.T) {
	s := open(t)
	for _, in := range []string{"", "   ", "D:/nowhere"} {
		got, err := s.AdoptableTask(in)
		if err != nil {
			t.Fatalf("AdoptableTask(%q): %v", in, err)
		}
		if got != "" {
			t.Fatalf("AdoptableTask(%q) found %q", in, got)
		}
	}
}
