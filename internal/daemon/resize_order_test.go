package daemon

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"
)

// slowPTY yields inside Resize, which is what widens the window between a
// viewport being computed and being applied.
type slowPTY struct {
	*fakePTY
}

func (s *slowPTY) Resize(cols, rows int) error {
	runtime.Gosched()
	if cols%3 == 0 {
		time.Sleep(50 * time.Microsecond)
	}
	return s.fakePTY.Resize(cols, rows)
}

// TWO VIEWERS RESIZING AT ONCE MUST LEAVE THE PTY AT THE SIZE THEY AGREE ON.
//
// The agreed size is computed from the viewers, so the last compute has to be
// the last apply. Otherwise the pty, the ring's marks and the viewers disagree.
func TestConcurrentViewportChangesLeavePtyRingAndViewersAgreeing(t *testing.T) {
	for round := 0; round < 200; round++ {
		f := newFakePTY()
		r := &runner{
			taskID:   "resize-order",
			pty:      &slowPTY{fakePTY: f},
			started:  time.Now(),
			buf:      newRing(1<<16, 80),
			watchers: map[chan []byte]struct{}{},
			done:     make(chan struct{}),
			// Short, so the heights still waiting settle in the loop below.
			hold: time.Millisecond,
		}
		var wg sync.WaitGroup
		for g := 0; g < 8; g++ {
			wg.Add(1)
			go func(g int) {
				defer wg.Done()
				id := fmt.Sprintf("viewer-%d", g)
				for i := 0; i < 10; i++ {
					_ = r.setViewport(id, 60+g*7+i, 20+(g+i)%9)
					if i%3 == 2 {
						r.dropViewport(id)
					}
				}
				if g%2 == 0 {
					r.dropViewport(id)
				}
			}(g)
		}
		wg.Wait()
		// The last height may still be waiting out the hold.
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
			r.resizeMu.Lock()
			waiting := r.pendingRows != 0
			r.resizeMu.Unlock()
			if !waiting {
				break
			}
			time.Sleep(time.Millisecond)
		}

		want := agreedViewport(r.views)
		sizes := f.resized()
		if len(r.views) == 0 {
			// The last viewer leaving keeps whatever size the pty had.
			f.Close()
			continue
		}
		curCols, curRows := r.buf.CurrentSize()
		if len(sizes) == 0 {
			t.Fatalf("round %d: no resize at all, want %+v", round, want)
		}
		last := sizes[len(sizes)-1]
		if last != want || (viewport{curCols, curRows}) != want {
			t.Fatalf("round %d: pty %+v, ring %dx%d, viewers agree on %+v", round, last, curCols, curRows, want)
		}
		f.Close()
	}
}
