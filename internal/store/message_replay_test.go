package store

import "testing"

// A block that carried a message is never replayed.
//
// The banner tells the model the call was interrupted rather than refused, and
// to retry it. The retry is the same command with the same dedup key, so
// without this it lands on the replay path and is handed the identical
// already-delivered message, carrying the identical instruction to retry. The
// model cannot get out of that, and the message it keeps being shown was
// delivered once and marked delivered.
func TestAMessageDeliveryIsNeverReplayed(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "courier", Worktree: "/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}

	p, _, err := s.RecordPermission(task.ID, "Bash", "make all", "key-msg", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DecidePermissionBy(p.ID, "block", "run the tests first", DecidedByMessage); err != nil {
		t.Fatal(err)
	}

	// The retry the banner asked for, well inside the replay window.
	again, decided, err := s.RecordPermission(task.ID, "Bash", "make all", "key-msg", "")
	if err != nil {
		t.Fatal(err)
	}
	if decided {
		t.Fatal("the retry was answered with the message it had already been given")
	}
	if again.ID == p.ID {
		t.Fatal("the retry reused the delivery row rather than asking properly")
	}
	if again.DecidedAt != nil {
		t.Fatal("the retry came back already decided")
	}
}

// The ordinary replay still works. A decision that judged the command really
// was an answer to it, and a reconnect must not ask the operator twice.
func TestANonMessageBlockStillReplays(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "judged", Worktree: "/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}

	p, _, err := s.RecordPermission(task.ID, "Bash", "rm -rf /", "key-no", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DecidePermissionBy(p.ID, "block", "no", "you"); err != nil {
		t.Fatal(err)
	}

	again, decided, err := s.RecordPermission(task.ID, "Bash", "rm -rf /", "key-no", "")
	if err != nil {
		t.Fatal(err)
	}
	if !decided {
		t.Fatal("a refusal stopped replaying inside the window")
	}
	if again.ID != p.ID {
		t.Fatal("a retry within the window made a second request")
	}
}
