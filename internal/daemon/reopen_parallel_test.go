package daemon

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

func reopenTasks(n int) []*store.Task {
	ts := make([]*store.Task, n)
	for i := range ts {
		ts[i] = &store.Task{ID: string(rune('a' + i))}
	}
	return ts
}

func TestReopenEachRunsCardsTogetherAndOnlyAFewAtATime(t *testing.T) {
	var cur, peak, arrived atomic.Int32
	gate := make(chan struct{})
	var once sync.Once
	var mu sync.Mutex
	seen := map[string]int{}
	ok := reopenEach(reopenTasks(12), func(tk *store.Task) error {
		c := cur.Add(1)
		for p := peak.Load(); c > p && !peak.CompareAndSwap(p, c); p = peak.Load() {
		}
		if arrived.Add(1) == reopenWorkers {
			once.Do(func() { close(gate) })
		}
		select {
		case <-gate:
		case <-time.After(2 * time.Second):
		}
		cur.Add(-1)
		mu.Lock()
		seen[tk.ID]++
		mu.Unlock()
		if tk.ID == "c" {
			return errors.New("gone")
		}
		return nil
	})
	if ok != 11 || len(seen) != 12 {
		t.Fatalf("reopened %d, saw %d distinct, want 11 and 12", ok, len(seen))
	}
	if p := int(peak.Load()); p != reopenWorkers {
		t.Fatalf("peak concurrency %d, want %d", p, reopenWorkers)
	}
}
