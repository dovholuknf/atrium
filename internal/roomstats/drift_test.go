package roomstats

import (
	"runtime"
	"testing"
	"time"
)

func TestDrainIsAP99AndEmpties(t *testing.T) {
	d := &Drift{}
	if _, _, ok := d.Drain(); ok {
		t.Fatal("an empty probe gave a reading")
	}
	for i := 1; i <= 100; i++ {
		d.record(time.Duration(i) * time.Millisecond)
	}
	p, n, ok := d.Drain()
	if !ok || n != 100 || p != 99*time.Millisecond {
		t.Fatalf("p99 %v n %d ok %v", p, n, ok)
	}
	if _, _, ok := d.Drain(); ok {
		t.Fatal("drain did not empty the probe")
	}
}

// measure runs the probe for a while and returns its p99.
func measure(t *testing.T, d time.Duration) time.Duration {
	t.Helper()
	p := &Drift{}
	stop := make(chan struct{})
	go p.Run(stop)
	time.Sleep(d)
	close(stop)
	p99, n, ok := p.Drain()
	if !ok || n < 5 {
		t.Fatalf("probe took %d samples", n)
	}
	return p99
}

// A loaded process reads worse than an idle one. One P and spinning goroutines
// make the timer wait its turn.
func TestLoadedProcessDriftsMore(t *testing.T) {
	prev := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(prev)

	idle := measure(t, 600*time.Millisecond)

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
	// A scheduler can have a quiet moment, so give it three looks.
	var loaded time.Duration
	for try := 0; try < 3 && loaded <= idle; try++ {
		loaded = measure(t, 600*time.Millisecond)
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
