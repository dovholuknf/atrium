//go:build integration

package hubstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/gitsync"
)

// A CALLER THAT GAVE UP IS NOT A BROKEN DISK. f-001: a request whose client went away reached a push-log write
// with its context, the write came back "context canceled", and guard halted the hub on it. Every room was then
// let go. A cancelled or timed-out caller must never halt the store.
func TestACancelledCallerDoesNotHaltTheStore(t *testing.T) {
	s := open(t)
	added(t, s, "sparta")
	var told error
	s.OnHalt = func(cause error) { told = cause }

	gone, cancel := context.WithCancel(context.Background())
	cancel()
	late, cancel2 := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel2()

	row := gitsync.PushRow{Repo: "atrium", Ref: "refs/heads/f", New: "abc", Room: "sparta", Card: "c1"}
	for name, ctx := range map[string]context.Context{"cancelled": gone, "past its deadline": late} {
		// A WRITE runs on a context detached from the caller's, so it lands whoever stopped waiting.
		batch, err := s.Begin(ctx, row)
		if err != nil {
			t.Fatalf("%s: a write was abandoned with its caller: %v", name, err)
		}
		if err := s.Settle(ctx, batch, row.Ref); err != nil {
			t.Fatalf("%s: settling was abandoned with its caller: %v", name, err)
		}
		// A READ may give up with its caller, and says so, and halts nothing.
		if _, err := s.Pending(ctx, ""); err != nil && errors.Is(err, ErrHalted) {
			t.Fatalf("%s: a read whose caller left came back as a halt: %v", name, err)
		}
		if _, _, _, err := s.Owner(ctx, "atrium", row.Ref); err != nil && errors.Is(err, ErrHalted) {
			t.Fatalf("%s: a read whose caller left came back as a halt: %v", name, err)
		}
	}
	if halted, cause := s.Halted(); halted {
		t.Fatalf("the store halted on a caller going away: %v", cause)
	}
	if told != nil {
		t.Fatalf("the hub was told the store halted: %v", told)
	}
	if _, err := s.ByName("sparta"); err != nil {
		t.Fatalf("the room is no longer known after a caller went away: %v", err)
	}
	if _, _, ok, err := s.Owner(context.Background(), "atrium", row.Ref); err != nil || !ok {
		t.Fatalf("the detached write did not land: ok=%v err=%v", ok, err)
	}
}

// A cancelled caller is excused only for its own context. The database going away under a request whose
// caller ALSO left is still the database going away, and still halts.
func TestARealFailureStillHaltsWhenTheCallerAlsoLeft(t *testing.T) {
	s := open(t)
	gone, cancel := context.WithCancel(context.Background())
	cancel()
	s.db.Close()
	if _, err := s.Begin(gone, gitsync.PushRow{Repo: "atrium", Ref: "refs/heads/f"}); !errors.Is(err, ErrHalted) {
		t.Fatalf("a closed database did not halt: %v", err)
	}
	if halted, _ := s.Halted(); !halted {
		t.Fatal("the store did not halt")
	}
}
