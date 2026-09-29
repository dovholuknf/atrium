package store

import (
	"strings"
	"testing"
)

func TestSayRecordLifecycle(t *testing.T) {
	s := openTestStore(t)
	bob, _, err := s.Register(Observed{WireName: "bob", Runner: "claude", PID: 1})
	if err != nil {
		t.Fatal(err)
	}
	alice, _, err := s.Register(Observed{WireName: "alice", Runner: "claude", PID: 2})
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.QueueFromPeer(bob.ID, "please reply", "alice")
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.RecordSay(Say{FromTask: alice.ID, FromWire: "alice", ToTask: bob.ID, ToWire: "bob",
		ToInput: "@b", Via: "alias", Door: "say", State: SayQueued, MessageID: m.ID, ReplyWant: true},
		strings.Repeat("x", 500))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.SayByID(id)
	if got.State != SayQueued || got.Chars != 500 || len([]rune(got.Preview)) != sayPreviewLen {
		t.Fatalf("queued row wrong: %+v", got)
	}
	if n, _ := s.RepliesOwed(); n[bob.ID] != 1 {
		t.Fatalf("owed = %v", n)
	}

	// every delivery path goes through MarkDelivered
	if err := s.MarkDelivered(bob.ID, "stop", []string{m.ID}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.SayByID(id)
	if got.State != SayDelivered || got.Channel != SayViaStop || got.DeliveredAt == "" {
		t.Fatalf("not delivered by stop: %+v", got)
	}

	// a clear after delivery is marked on the record
	if err := s.NoteContextReset(bob.ID, "clear"); err != nil {
		t.Fatal(err)
	}
	got, _ = s.SayByID(id)
	if got.ResetKind != "clear" || got.ResetAt == "" {
		t.Fatalf("reset not noted: %+v", got)
	}

	// a reply from someone else settles nothing, from bob to alice settles it
	if n, _ := s.AnswerSaysFrom(bob.ID, bob.ID); n != 0 {
		t.Fatalf("self settled %d", n)
	}
	if n, _ := s.AnswerSaysFrom(bob.ID, alice.ID); n != 1 {
		t.Fatalf("settled %d", n)
	}
	if n, _ := s.RepliesOwed(); n[bob.ID] != 0 {
		t.Fatalf("still owed: %v", n)
	}
	both, _ := s.SaysFor(alice.ID, 10)
	if len(both) != 1 {
		t.Fatalf("sender cannot see it: %d", len(both))
	}
}

func TestSayRecordSweepKeepsOpenOwed(t *testing.T) {
	s := openTestStore(t)
	old := ts(now().Add(-SayKeep - 1))
	mk := func(reply bool) string {
		id, err := s.RecordSay(Say{FromWire: "a", ToTask: "t", ToInput: "t", Via: "handle", Door: "say",
			State: SayQueued, ReplyWant: reply, SentAt: old}, "hi")
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	plain, owed := mk(false), mk(true)
	if _, err := s.SweepSays(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SayByID(plain); err == nil {
		t.Fatal("old plain row kept")
	}
	// older than the keep window, so it lapsed first and then went too
	if got, err := s.SayByID(owed); err == nil && !got.Lapsed {
		t.Fatalf("old owed row neither lapsed nor gone: %+v", got)
	}
}

func TestSayRecordLapsesForEndedCard(t *testing.T) {
	s := openTestStore(t)
	id, _ := s.RecordSay(Say{FromWire: "a", ToTask: "t1", ToInput: "t", Via: "handle", Door: "say",
		State: SayQueued, ReplyWant: true}, "hi")
	if err := s.LapseSaysFor("t1"); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.RepliesOwed(); n["t1"] != 0 {
		t.Fatal("lapsed reply still counted")
	}
	got, _ := s.SayByID(id)
	if !got.Lapsed {
		t.Fatal("not lapsed")
	}
}
