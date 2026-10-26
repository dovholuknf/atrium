//go:build integration

package roomstats

import (
	"runtime"
	"testing"
	"time"
)

// measure runs the probe for a while and returns its p99 and how many samples
// it took. A starved probe takes few, which is the thing being measured, so it
// is the caller that decides how many are enough.
func measure(d time.Duration) (p99 time.Duration, n int, ok bool) {
	p := &Drift{}
	stop := make(chan struct{})
	go p.Run(stop)
	time.Sleep(d) // wait: the probe running for d is what is measured
	close(stop)
	return p.Drain()
}

// A loaded process reads worse than an idle one. One P and spinning goroutines
// make the timer wait its turn.
func TestLoadedProcessDriftsMore(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real work")
	}
	prev := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(prev)

	idle, n, ok := measure(time.Second)
	if !ok || n < 5 {
		t.Fatalf("idle probe took %d samples", n)
	}

	quit := make(chan struct{})
	defer close(quit)
	for i := 0; i < 16; i++ {
		go func() {
			x := 0
			for {
				select {
				case <-quit:
					return
				default:
				}
				for j := 0; j < 1e6; j++ {
					x += j
				}
				_ = x
			}
		}()
	}
	// A scheduler can have a quiet moment, so give it three looks. Under this
	// load the probe may take only a few samples, or none in a look, and a
	// look with no sample is no reading rather than a failure.
	var loaded time.Duration
	for try := 0; try < 3 && loaded <= idle; try++ {
		if p99, _, ok := measure(time.Second); ok {
			loaded = p99
		}
	}
	if loaded <= idle {
		t.Fatalf("loaded p99 %v is not above idle p99 %v", loaded, idle)
	}
}

func TestTickCarriesLatency(t *testing.T) {
	d := &Drift{}
	d.record(7 * time.Millisecond)
	s := New(Sources{Room: "r", Drift: d})
	s.Tick()
	if s.snap.Latency == nil || s.snap.Latency.DriftP99Ms != 7 {
		t.Fatalf("latency %+v", s.snap.Latency)
	}
	if got := s.snap.Latency.DriftSeriesMs; len(got) != 60 || got[59] == nil || *got[59] != 7 {
		t.Fatalf("series %v", got)
	}
}
