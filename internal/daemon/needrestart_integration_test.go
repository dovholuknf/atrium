//go:build integration

package daemon

import (
	"errors"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

func needDaemon(restart func(string) (*store.Task, error)) *Daemon {
	d := &Daemon{act: newActivityTracker()}
	d.hk.needQuiet, d.hk.needQuietTick, d.hk.needRestart = 60*time.Millisecond, 5*time.Millisecond, restart
	return d
}

func TestNeedRestartWaitsForTheCardToGoQuiet(t *testing.T) {
	var at time.Time
	d := needDaemon(func(string) (*store.Task, error) { at = time.Now(); return nil, nil })
	d.act.set("c1", ActivityThinking, "")
	last := time.Now()
	d.restartForNeed("c1")
	if at.Sub(last) < d.hk.needQuiet {
		t.Fatalf("restarted %v after the last activity, want at least %v", at.Sub(last), d.hk.needQuiet)
	}
}

func TestNeedRestartRetriesAFailedRestartAndIsBounded(t *testing.T) {
	calls := 0
	d := needDaemon(func(string) (*store.Task, error) { calls++; return nil, errors.New("still mid-turn") })
	d.restartForNeed("c1")
	if calls != needRestartTries {
		t.Fatalf("%d tries, want %d", calls, needRestartTries)
	}
	calls = 0
	d = needDaemon(func(string) (*store.Task, error) {
		calls++
		if calls < 3 {
			return nil, errors.New("still mid-turn")
		}
		return nil, nil
	})
	d.restartForNeed("c1")
	if calls != 3 {
		t.Fatalf("%d tries, want 3", calls)
	}
}
