package roomstats

import (
	"sort"
	"sync"
	"time"
)

// Latency is how long this process takes to get to work it was asked to do,
// which is what a person feels as lag. Drift is how late a timer fires against
// when it was due, the measure the sg4, sg3 and m1mini benchmark used. The
// series is the per-minute average of each tick's p99, in ms, on the same 60
// minute grid as the CPU and memory series.
type Latency struct {
	DriftP99Ms    float64    `json:"drift_p99_ms"`
	DriftSeriesMs []*float64 `json:"drift_series_ms"`
}

// DriftInterval is how often the probe's timer is due.
const DriftInterval = 20 * time.Millisecond

// driftCap bounds the samples kept between two ticks. Ten seconds at 20ms is
// 500, so this only bites if the stats tick itself stalls.
const driftCap = 2000

// Drift is a probe that sleeps DriftInterval over and over and records how
// late each wake was.
type Drift struct {
	mu   sync.Mutex
	late []time.Duration
}

// Run probes until stop closes.
func (d *Drift) Run(stop <-chan struct{}) {
	for {
		due := time.Now().Add(DriftInterval)
		t := time.NewTimer(DriftInterval)
		select {
		case <-stop:
			t.Stop()
			return
		case <-t.C:
		}
		d.record(time.Since(due))
	}
}

func (d *Drift) record(late time.Duration) {
	if late < 0 {
		late = 0
	}
	d.mu.Lock()
	if len(d.late) < driftCap {
		d.late = append(d.late, late)
	}
	d.mu.Unlock()
}

// Drain returns the p99 lateness of the samples since the last drain, and how
// many there were. Zero samples is ok false: no reading, not a zero.
func (d *Drift) Drain() (p99 time.Duration, n int, ok bool) {
	d.mu.Lock()
	s := d.late
	d.late = nil
	d.mu.Unlock()
	if len(s) == 0 {
		return 0, 0, false
	}
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	i := (len(s)*99 + 99) / 100
	if i > 0 {
		i--
	}
	return s[i], len(s), true
}
