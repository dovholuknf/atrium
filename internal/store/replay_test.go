package store

import (
	"testing"
	"time"
)

// A dedup key exists for one failure: the daemon dies between recording a
// decision and answering it, the agent reconnects and re-posts, and the
// operator must not be asked twice. Inside the window that replay is correct.
func TestDecidedRequestReplaysInsideTheWindow(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "replay", Worktree: "/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}

	p, decided, err := s.RecordPermission(task.ID, "Bash", "ls", "key-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if decided {
		t.Fatal("a brand new request came back already decided")
	}
	if _, err := s.DecidePermission(p.ID, "approve", ""); err != nil {
		t.Fatal(err)
	}

	again, decided, err := s.RecordPermission(task.ID, "Bash", "ls", "key-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if !decided {
		t.Fatal("a retry within the window was not answered from the earlier decision")
	}
	if again.ID != p.ID {
		t.Fatal("a retry within the window made a second request")
	}
}

// The key cannot be trusted to identify one ATTEMPT. The permission hook
// builds it by hashing the session, the tool and the command, which is stable
// across a retry and equally stable across running the same command tomorrow.
//
// Without the window, one block answered once would replay against every
// identical command for the life of the card, and replays are silent.
func TestStaleDecisionIsNotReplayed(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "stale", Worktree: "/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}

	p, _, err := s.RecordPermission(task.ID, "Bash", "grep x", "key-2", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DecidePermission(p.ID, "block", "no"); err != nil {
		t.Fatal(err)
	}

	// Time moved on past the window, the way it does between two deliberate
	// runs of the same command.
	restore := now
	now = func() time.Time { return restore().Add(ReplayWindow + time.Minute) }
	defer func() { now = restore }()

	again, decided, err := s.RecordPermission(task.ID, "Bash", "grep x", "key-2", "")
	if err != nil {
		t.Fatal(err)
	}
	if decided {
		t.Fatal("a stale block was replayed, which would silently refuse the same command forever")
	}
	if again.ID == p.ID {
		t.Fatal("the stale row was reused rather than a fresh question being asked")
	}
	if again.DecidedAt != nil {
		t.Fatal("the fresh question came back already answered")
	}
}

// Pending is always the same question. The agent is blocked on it right now,
// so there is nothing to go stale and the window must not apply.
func TestPendingRequestIsAlwaysTheSameQuestion(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "pending", Worktree: "/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	p, _, err := s.RecordPermission(task.ID, "Bash", "sleep 100", "key-3", "")
	if err != nil {
		t.Fatal(err)
	}

	restore := now
	now = func() time.Time { return restore().Add(ReplayWindow * 10) }
	defer func() { now = restore }()

	again, decided, err := s.RecordPermission(task.ID, "Bash", "sleep 100", "key-3", "")
	if err != nil {
		t.Fatal(err)
	}
	if decided {
		t.Fatal("a request nobody has answered came back decided")
	}
	if again.ID != p.ID {
		t.Fatal("a still-pending request was duplicated, so the operator would see it twice")
	}
}

// The key belongs to one agent's request. An identical key from another card
// is a different question, and answering it from the first would be answering
// for a session nobody asked about.
func TestDedupKeyIsScopedToOneCard(t *testing.T) {
	s := open(t)
	a, _, err := s.Register(Observed{WireName: "one", Worktree: "/w/one", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := s.Register(Observed{WireName: "two", Worktree: "/w/two", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}

	p, _, err := s.RecordPermission(a.ID, "Bash", "ls", "same", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DecidePermission(p.ID, "approve", ""); err != nil {
		t.Fatal(err)
	}

	other, decided, err := s.RecordPermission(b.ID, "Bash", "ls", "same", "")
	if err != nil {
		t.Fatal(err)
	}
	if decided {
		t.Fatal("one card's answer was given to another card's request")
	}
	if other.ID == p.ID {
		t.Fatal("two cards share one permission row")
	}
}

// No key means treat every request as new, which is the behaviour for any
// caller that does not send one.
func TestNoKeyNeverReplays(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "nokey", Worktree: "/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	p, _, err := s.RecordPermission(task.ID, "Bash", "ls", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DecidePermission(p.ID, "approve", ""); err != nil {
		t.Fatal(err)
	}
	again, decided, err := s.RecordPermission(task.ID, "Bash", "ls", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if decided || again.ID == p.ID {
		t.Fatal("an unkeyed request was deduplicated")
	}
}
