package store

import "testing"

// Whether a runner takes typed input mid-turn, and whether a message waits for
// the turn to end. See internal/daemon/saywhen.go.

// The runner flag survives a round trip, on and off.
func TestMidTurnInputSurvivesBeingSaved(t *testing.T) {
	s := openTestStore(t)
	for _, on := range []bool{true, false} {
		if _, err := s.SaveHarness(Harness{
			ID: "mine", Label: "mine", Cmd: "claude", LaunchMode: LaunchPTY, MidTurnInput: on,
		}); err != nil {
			t.Fatal(err)
		}
		got, err := s.Harness("mine")
		if err != nil {
			t.Fatal(err)
		}
		if got.MidTurnInput != on {
			t.Fatalf("saved mid_turn_input %v, read back %v", on, got.MidTurnInput)
		}
	}
}

// An existing database gets claude and codex switched on by the migration, and
// running it a second time over columns already there is not an error.
func TestTheMigrationSeedsMidTurnInputOnAnExistingDatabase(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.db.Exec(`UPDATE harness SET mid_turn_input = 0`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`DELETE FROM schema_migration WHERE name = '0060_say_when'`); err != nil {
		t.Fatal(err)
	}
	if err := s.migrate(); err != nil {
		t.Fatalf("the migration did not tolerate its columns already being there: %v", err)
	}
	for id, want := range map[string]bool{"claude": true, "codex": true, "ollama": false} {
		h, err := s.Harness(id)
		if err != nil {
			t.Fatal(err)
		}
		if h.MidTurnInput != want {
			t.Fatalf("%s: mid_turn_input %v, want %v", id, h.MidTurnInput, want)
		}
	}
}

// A message that waits for the turn says so when it is read back, and one that
// does not, does not.
func TestAMessageRemembersItWaitsForTheTurn(t *testing.T) {
	s := openTestStore(t)
	task, _, err := s.Register(Observed{WireName: "bob", Runner: "claude", PID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.QueueAfterTurn(task.ID, "after", "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.QueueFromPeer(task.ID, "now", "alice"); err != nil {
		t.Fatal(err)
	}
	msgs, err := s.PendingMessages(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || !msgs[0].WaitTurn || msgs[1].WaitTurn || msgs[0].FromPeer != "alice" {
		t.Fatalf("read back wrong: %+v %+v", msgs[0], msgs[1])
	}
}
