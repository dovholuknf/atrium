package gitsync

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/base/testwait"
)

const (
	pushedTestHead = "0123456789abcdef0123456789abcdef01234567"
	pushedTestTip  = "89abcdef0123456789abcdef0123456789abcdef"
)

func pushedTestStore(t *testing.T, fn func(context.Context, string, string, string) (Pushed, error)) *Store {
	t.Helper()
	s := (&Hub{Dir: t.TempDir(), Runner: NewRunner()}).Store()
	s.pushedFn = fn
	return s
}

func waitPushed(t *testing.T, s *Store, want string) PushedView {
	t.Helper()
	var v PushedView
	testwait.Eventually(t, 5*time.Second, "the snapshot to say "+want, func() bool {
		var err error
		v, err = s.PushedSnapshot("github/o/r", "main", pushedTestHead)
		if err != nil {
			t.Fatal(err)
		}
		return !v.Building && v.State == want
	})
	return v
}

// A request answers from memory however slow git is: the first one says it is building with an empty answer, and
// none waits for the build.
func TestPushedSnapshotAnswersAtOnceWhileGitIsSlow(t *testing.T) {
	release := make(chan struct{})
	var calls atomic.Int64
	s := pushedTestStore(t, func(ctx context.Context, _, _, _ string) (Pushed, error) {
		calls.Add(1)
		<-release
		return Pushed{State: PushedMatches, HubSHA: pushedTestHead}, nil
	})
	defer close(release)

	for i := 0; i < 20; i++ {
		start := time.Now()
		v, err := s.PushedSnapshot("github/o/r", "main", pushedTestHead)
		if err != nil {
			t.Fatal(err)
		}
		if d := time.Since(start); d > 50*time.Millisecond {
			t.Fatalf("request %d took %v", i, d)
		}
		if !v.Building || v.State != "" || !v.Computed.IsZero() {
			t.Fatalf("before the first build: %+v", v)
		}
	}
	time.Sleep(20 * time.Millisecond)
	if n := calls.Load(); n != 1 {
		t.Fatalf("20 requests started %d builds, want 1", n)
	}
}

// A rebuild changes what the next request answers.
func TestPushedSnapshotUpdatesAfterARebuild(t *testing.T) {
	var tip atomic.Value
	tip.Store(pushedTestHead)
	s := pushedTestStore(t, func(ctx context.Context, _, _, head string) (Pushed, error) {
		if tip.Load().(string) == head {
			return Pushed{State: PushedMatches, HubSHA: head}, nil
		}
		return Pushed{State: PushedBehind, HubSHA: tip.Load().(string)}, nil
	})
	v := waitPushed(t, s, PushedMatches)
	if v.Computed.IsZero() {
		t.Fatal("the snapshot does not say how old it is")
	}
	tip.Store(pushedTestTip)
	s.RefreshView()
	if v := waitPushed(t, s, PushedBehind); v.HubSHA != pushedTestTip {
		t.Fatalf("view = %+v", v)
	}
}

func TestPushedSnapshotRefusesWhatPushedRefuses(t *testing.T) {
	s := pushedTestStore(t, nil)
	if _, err := s.PushedSnapshot("github/o/r", "main", "nope"); err == nil || !strings.Contains(err.Error(), "head") {
		t.Fatalf("err = %v", err)
	}
	if _, err := s.PushedSnapshot("github/o/r", "bad..branch", pushedTestHead); err == nil {
		t.Fatal("a bad branch was taken")
	}
}
