//go:build integration

package store

import (
	"strings"
	"testing"
	"time"
)

func TestSayRecordLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: a real sqlite database on disk")
	}
	t.Parallel()
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
	t.Parallel()
	s := openTestStore(t)
	old := ts(s.now().Add(-SayKeep - 1))
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
	t.Parallel()
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

func TestSayKeepsBoundedTextAndPagesBack(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: a real sqlite database on disk")
	}
	t.Parallel()
	s := openTestStore(t)
	a, _, _ := s.Register(Observed{WireName: "alice", Runner: "claude", PID: 1})
	b, _, _ := s.Register(Observed{WireName: "bob", Runner: "claude", PID: 2})
	long := strings.Repeat("x", SayTextLen+50)
	var ids []string
	for i := 0; i < 5; i++ {
		text := "say " + string(rune('a'+i))
		if i == 0 {
			text = long
		}
		id, err := s.RecordSay(Say{FromTask: a.ID, FromWire: "alice", ToTask: b.ID, ToWire: "bob", ToInput: "bob",
			Via: "handle", Door: "say", State: SayDelivered, SentAt: ts(s.now().Add(-time.Duration(10-i) * time.Minute))}, text)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	first, _ := s.SayByID(ids[0])
	if len(first.Text) != SayTextLen || first.Chars != SayTextLen+50 {
		t.Fatalf("text not bounded: %d bytes, chars %d", len(first.Text), first.Chars)
	}
	if last, _ := s.SayByID(ids[4]); last.Text != "say e" {
		t.Fatalf("short text not kept whole: %q", last.Text)
	}
	page1, _ := s.SaysBefore(b.ID, "", 2)
	if len(page1) != 2 || page1[0].ID != ids[4] || page1[1].ID != ids[3] {
		t.Fatalf("first page wrong: %+v", page1)
	}
	page2, _ := s.SaysBefore(b.ID, page1[1].ID, 2)
	if len(page2) != 2 || page2[0].ID != ids[2] || page2[1].ID != ids[1] {
		t.Fatalf("second page wrong: %+v", page2)
	}
	page3, _ := s.SaysBefore(b.ID, page2[1].ID, 2)
	if len(page3) != 1 || page3[0].ID != ids[0] {
		t.Fatalf("third page wrong: %+v", page3)
	}
	if gone, _ := s.SaysBefore(b.ID, "no-such-row", 2); len(gone) != 0 {
		t.Fatalf("a cursor naming no row returned %d", len(gone))
	}
}
