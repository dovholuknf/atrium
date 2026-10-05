package hubstore

import (
	"errors"
	"path/filepath"
	"strconv"
	"testing"
)

func openPushStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func pushSubOf(n int) PushSub {
	return PushSub{Endpoint: "https://fcm.googleapis.com/fcm/send/" + strconv.Itoa(n), P256dh: "k", Auth: "a",
		Label: "d" + strconv.Itoa(n), Origin: "https://o"}
}

func TestPushAddCapAndRepeat(t *testing.T) {
	s := openPushStore(t)
	var first string
	for i := 0; i < PushMax; i++ {
		id, added, err := s.PushAdd(pushSubOf(i))
		if err != nil || !added {
			t.Fatalf("%d: %v %v", i, added, err)
		}
		if i == 0 {
			first = id
		}
	}
	if _, _, err := s.PushAdd(pushSubOf(99)); !errors.Is(err, ErrPushFull) {
		t.Fatalf("the ninth: %v", err)
	}
	if halted, _ := s.Halted(); halted {
		t.Fatal("a full table halted the store")
	}
	// The same endpoint is the same device: it is updated, keeps its id, and is switched back on.
	if _, err := s.PushOutcome(first, false, "boom", 1); err != nil {
		t.Fatal(err)
	}
	again := pushSubOf(0)
	again.Label = "renamed"
	id, added, err := s.PushAdd(again)
	if err != nil || added || id != first {
		t.Fatalf("repeat: %q %v %v", id, added, err)
	}
	subs, _ := s.PushSubs()
	if len(subs) != PushMax || subs[0].Label != "renamed" || subs[0].DisabledReason != "" || subs[0].Failures != 0 {
		t.Errorf("%+v", subs[0])
	}
}

func TestPushOutcomeSwitchesOffAtTheLimit(t *testing.T) {
	s := openPushStore(t)
	id, _, _ := s.PushAdd(pushSubOf(1))
	for i := 1; i <= 3; i++ {
		n, err := s.PushOutcome(id, false, "HTTP 500", 3)
		if err != nil || n != i {
			t.Fatalf("failure %d: %d %v", i, n, err)
		}
		subs, _ := s.PushSubs()
		if off := subs[0].DisabledReason != ""; off != (i == 3) {
			t.Fatalf("after %d failures disabled=%v", i, off)
		}
	}
	// An outcome for a row that is gone is not an error.
	if _, err := s.PushOutcome("gone", false, "x", 3); err != nil {
		t.Errorf("%v", err)
	}
}

func TestPushRemove(t *testing.T) {
	s := openPushStore(t)
	a, _, _ := s.PushAdd(pushSubOf(1))
	s.PushAdd(pushSubOf(2))
	if err := s.PushRemoveID(a); err != nil {
		t.Fatal(err)
	}
	if err := s.PushRemoveID(a); !errors.Is(err, ErrNoPushSub) {
		t.Errorf("a second removal: %v", err)
	}
	if err := s.PushRemoveEndpoint(pushSubOf(2).Endpoint); err != nil {
		t.Fatal(err)
	}
	if err := s.PushRemoveEndpoint(pushSubOf(2).Endpoint); !errors.Is(err, ErrNoPushSub) {
		t.Errorf("a second removal: %v", err)
	}
	if halted, _ := s.Halted(); halted {
		t.Error("a missing row halted the store")
	}
}
