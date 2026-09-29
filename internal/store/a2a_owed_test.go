package store

import (
	"testing"
	"time"
)

// A prompt written inside a transaction must not read the tenant from the
// pool. The pool holds one connection and the transaction has it, so the room
// froze at startup when a ledger notice was queued inside the exit transaction.
func TestPromptInsideATransactionDoesNotDeadlock(t *testing.T) {
	s := openTestStore(t)
	if err := s.SetTenant("sg4"); err != nil {
		t.Fatal(err)
	}
	launcher, _, _ := s.Register(Observed{WireName: "boss", Worktree: "/tmp/boss"})
	card, _, _ := s.Register(Observed{WireName: "kid", Worktree: "/tmp/kid"})
	if err := s.SetLineage(card.ID, "boss", launcher.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- s.inTx(func(tx *Tx) error {
			for _, peer := range []string{"other", "boss"} {
				if _, err := s.appendEventOn(tx, card.ID, EventPrompted, map[string]any{"text": "x", "from_peer": peer}); err != nil {
					return err
				}
			}
			return nil
		})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("appending a prompt inside a transaction deadlocked")
	}
	if got, _ := s.Get(card.ID); !got.OwesReport() {
		t.Fatal("the launcher's prompt inside a transaction did not make the card owe")
	}
}

// Only the launcher's prompt makes a card owe it a report (item 41).
func TestOnlyTheLaunchersPromptOwesAReport(t *testing.T) {
	s := openTestStore(t)
	launcher, _, _ := s.Register(Observed{WireName: "boss", Worktree: "/tmp/boss"})
	card, _, _ := s.Register(Observed{WireName: "kid", Worktree: "/tmp/kid"})
	if err := s.SetLineage(card.ID, "boss", launcher.ID); err != nil {
		t.Fatal(err)
	}
	for name, ev := range map[string]map[string]any{
		"the operator":  {"text": "x", "via": "terminal"},
		"a third":       {"text": "x", "from_peer": "other"},
		"a reopen":      {"text": "x", "via": "launch"},
		"a note":        {"text": "x", "from": "note"},
		"a delivery":    {"delivered": 1, "via": "stop"},
		"a look-alike":  {"text": "x", "from_peer": "boss2"},
		"an empty peer": {"text": "x", "from_peer": " "},
	} {
		if err := s.AppendEvent(card.ID, EventPrompted, ev); err != nil {
			t.Fatal(err)
		}
		if got, _ := s.Get(card.ID); got.OwesReport() || got.OwedAt != nil {
			t.Fatalf("%s made the card owe a report", name)
		}
	}
	if err := s.AppendEvent(card.ID, EventPrompted, map[string]any{"text": "x", "from_peer": "boss"}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(card.ID)
	if !got.OwesReport() || got.PromptedAt == nil {
		t.Fatalf("the launcher's message did not make the card owe: %+v", got.OwedAt)
	}
}
