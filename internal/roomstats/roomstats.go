// Package roomstats keeps one snapshot of how a room is doing and pushes it.
//
// The rooms dashboard shows each room's token burn, process, disk and runners.
// The board never polls state, so the room PUSHES: one sampler goroutine owns
// the snapshot, refreshes it every ten seconds and hands the JSON to Publish.
// `GET /v1/room/stats` returns the last pushed bytes and does no work of its
// own. The shape is u-010's event block, see docs/backlog/runtime/r-010.md.
//
// A section whose sampler failed keeps its last good values and gains
// `stale_since`, the time of the first failed sample. A section never sampled
// successfully is absent. Never zeros. Per-runner `cpu_pct` and `rss_bytes` are
// still to come.
package roomstats

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// Version is the event's `v`.
const Version = 1

// Cadences. Disk has its own slower timer, which never blocks the tick.
const (
	Interval     = 10 * time.Second
	DiskInterval = 5 * time.Minute
)

// Kinds is tokens by kind. A rate, the series and by_cause count in + out +
// cache_write. cache_read is reported apart, in Hour and Last24h.
type Kinds struct {
	In         int64 `json:"in"`
	Out        int64 `json:"out"`
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
}

// Tokens is the token section.
type Tokens struct {
	PerMin5m     int64            `json:"per_min_5m"`
	Hour         Kinds            `json:"hour"`
	Last24h      Kinds            `json:"last_24h"`
	ByCause24h   map[string]int64 `json:"by_cause_24h"`
	SeriesPerMin []int64          `json:"series_per_min"`
	SeriesEnd    string           `json:"series_end"`
	StaleSince   string           `json:"stale_since,omitempty"`
}

// Process is the room process.
type Process struct {
	CPUPct     float64 `json:"cpu_pct"`
	RSSBytes   uint64  `json:"rss_bytes"`
	HeapBytes  uint64  `json:"heap_bytes"`
	Goroutines int     `json:"goroutines"`
	StartedAt  string  `json:"started_at"`
	StaleSince string  `json:"stale_since,omitempty"`
}

// Disk is the volume holding the room's worktree root.
type Disk struct {
	Path       string `json:"path"`
	FreeBytes  uint64 `json:"free_bytes"`
	TotalBytes uint64 `json:"total_bytes"`
	DBBytes    int64  `json:"db_bytes"`
	Worktrees  int    `json:"worktrees"`
	StaleSince string `json:"stale_since,omitempty"`
}

// Runner is a harness row: its kind and whether its binary resolves.
type Runner struct {
	Kind     string `json:"kind"`
	Resolves bool   `json:"resolves"`
}

// Snapshot is the event.
type Snapshot struct {
	V       int      `json:"v"`
	Room    string   `json:"room"`
	At      string   `json:"at"`
	Tokens  *Tokens  `json:"tokens,omitempty"`
	Process *Process `json:"process,omitempty"`
	Disk    *Disk    `json:"disk,omitempty"`
	Machine *Machine `json:"machine,omitempty"`
	Latency *Latency `json:"latency,omitempty"`
	Runners []Runner `json:"runners,omitempty"`
	// RunnersStaleSince is the runners array's stale mark, since an array
	// carries no field of its own.
	RunnersStaleSince string `json:"runners_stale_since,omitempty"`
}

// Clock is time, so a test can drive the cadence.
type Clock interface {
	Now() time.Time
	// Ticker returns a channel ticking every d, and a stop.
	Ticker(d time.Duration) (<-chan time.Time, func())
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }
func (realClock) Ticker(d time.Duration) (<-chan time.Time, func()) {
	t := time.NewTicker(d)
	return t.C, t.Stop
}

// Sources is everything a sample reads. A nil one is skipped and its section
// stays absent.
type Sources struct {
	Room    string
	Started time.Time
	Clock   Clock
	// Usage is store.UsageBuckets.
	Usage func(since, until time.Time, bucketSecs int, card string) (*store.UsageSeries, error)
	// ProcessTimes is the room process's CPU time so far, and its resident bytes.
	ProcessTimes func() (cpu time.Duration, rss uint64, err error)
	// Disk reads the volume of the worktree root.
	Disk func() (path string, free, total uint64, err error)
	// DBBytes is the database with its WAL.
	DBBytes func() (int64, error)
	// Worktrees counts distinct existing card worktrees the room knows.
	Worktrees func() (int, error)
	Runners   func() ([]Runner, error)
	// Machine reads whole-machine CPU counters and memory.
	Machine func() (MachineReading, error)
	// Drift is the timer-lateness probe. Run starts it.
	Drift *Drift
	// Publish gets the snapshot's JSON every tick.
	Publish func(data []byte)
}

// Sampler owns the snapshot.
type Sampler struct {
	src Sources

	mu      sync.Mutex
	snap    Snapshot
	last    []byte
	lastCPU time.Duration
	lastAt  time.Time
	staleAt map[string]string

	// machine state: the previous CPU counters, and the per-minute rings.
	prevIdle, prevTotal uint64
	havePrevCPU         bool
	cpuRing, memRing    *minuteAvg
	driftRing           *minuteAvg
}

// New builds a sampler. Nothing runs until Run.
func New(src Sources) *Sampler {
	if src.Clock == nil {
		src.Clock = realClock{}
	}
	if src.Started.IsZero() {
		src.Started = src.Clock.Now()
	}
	return &Sampler{src: src, staleAt: map[string]string{}, lastAt: src.Started,
		cpuRing: newMinuteAvg(), memRing: newMinuteAvg(), driftRing: newMinuteAvg()}
}

func rfc(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// Bytes is the last pushed snapshot, for the first paint. Nil before the first
// sample.
func (s *Sampler) Bytes() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last
}

// Run samples once at once, then every Interval until stop closes. Disk runs
// on its own goroutine and timer.
func (s *Sampler) Run(stop <-chan struct{}) {
	tick, stopTick := s.src.Clock.Ticker(Interval)
	defer stopTick()
	diskTick, stopDisk := s.src.Clock.Ticker(DiskInterval)
	defer stopDisk()
	if s.src.Drift != nil {
		go s.src.Drift.Run(stop)
	}
	s.SampleDisk()
	s.Tick()
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-diskTick:
				s.SampleDisk()
			}
		}
	}()
	for {
		select {
		case <-stop:
			return
		case <-tick:
			s.Tick()
		}
	}
}

// mark records one section's outcome, with the lock held. Success clears the
// stale mark, failure sets it once and leaves the last good values alone.
func (s *Sampler) mark(name string, err error, now time.Time) string {
	if err == nil {
		delete(s.staleAt, name)
		return ""
	}
	log.Printf("[atrium] room-stats %s: %v", name, err)
	if s.staleAt[name] == "" {
		s.staleAt[name] = rfc(now)
	}
	return s.staleAt[name]
}

// SampleDisk refreshes the disk section only. It holds the lock just to store.
func (s *Sampler) SampleDisk() {
	if s.src.Disk == nil {
		return
	}
	now := s.src.Clock.Now()
	var d Disk
	var err error
	d.Path, d.FreeBytes, d.TotalBytes, err = s.src.Disk()
	if err == nil && s.src.DBBytes != nil {
		d.DBBytes, err = s.src.DBBytes()
	}
	if err == nil && s.src.Worktrees != nil {
		d.Worktrees, err = s.src.Worktrees()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.mark("disk", err, now)
	if err != nil {
		if s.snap.Disk != nil {
			s.snap.Disk.StaleSince = st
		}
		return
	}
	s.snap.Disk = &d
}

// Tick refreshes everything but disk, then pushes.
func (s *Sampler) Tick() {
	now := s.src.Clock.Now()

	var tok *Tokens
	var tokErr error
	if s.src.Usage != nil {
		tok, tokErr = s.tokens(now)
	}
	var mr MachineReading
	var machErr error
	if s.src.Machine != nil {
		mr, machErr = s.src.Machine()
	}
	var runners []Runner
	var runErr error
	if s.src.Runners != nil {
		runners, runErr = s.src.Runners()
	}

	s.mu.Lock()
	s.snap.V, s.snap.Room, s.snap.At = Version, s.src.Room, rfc(now)

	if s.src.Usage != nil {
		st := s.mark("tokens", tokErr, now)
		if tokErr == nil {
			s.snap.Tokens = tok
		} else if s.snap.Tokens != nil {
			s.snap.Tokens.StaleSince = st
		}
	}
	if s.src.ProcessTimes != nil {
		s.sampleProcess(now)
	}
	if s.src.Machine != nil {
		s.sampleMachine(now, mr, machErr)
	}
	if s.src.Drift != nil {
		s.sampleLatency(now)
	}
	if s.src.Runners != nil {
		st := s.mark("runners", runErr, now)
		if runErr == nil {
			s.snap.Runners, s.snap.RunnersStaleSince = runners, ""
		} else if s.snap.Runners != nil {
			s.snap.RunnersStaleSince = st
		}
	}
	data, err := json.Marshal(&s.snap)
	if err != nil {
		s.mu.Unlock()
		log.Printf("[atrium] room-stats marshal: %v", err)
		return
	}
	s.last = data
	pub := s.src.Publish
	s.mu.Unlock()
	if pub != nil {
		pub(data)
	}
}

// sampleProcess runs with the lock held. CPU is a rate of process CPU time
// between samples, as a percent of one core.
func (s *Sampler) sampleProcess(now time.Time) {
	cpu, rss, err := s.src.ProcessTimes()
	st := s.mark("process", err, now)
	if err != nil {
		if s.snap.Process != nil {
			s.snap.Process.StaleSince = st
		}
		return
	}
	p := &Process{RSSBytes: rss, StartedAt: rfc(s.src.Started)}
	if wall := now.Sub(s.lastAt); wall > 0 && cpu >= s.lastCPU {
		pct := float64(cpu-s.lastCPU) / float64(wall) * 100
		p.CPUPct = float64(int(pct*10+0.5)) / 10
	}
	s.lastCPU, s.lastAt = cpu, now
	p.HeapBytes, p.Goroutines = memStats()
	s.snap.Process = p
}

// sampleMachine runs with the lock held. CPU needs two readings, so the first
// tick leaves it out. The series end on the minute tokens.series_end names.
func (s *Sampler) sampleMachine(now time.Time, mr MachineReading, err error) {
	st := s.mark("machine", err, now)
	if err != nil {
		if s.snap.Machine != nil {
			s.snap.Machine.StaleSince = st
		}
		return
	}
	minute := now.UTC().Truncate(time.Minute)
	m := s.snap.Machine
	if m == nil {
		m = &Machine{}
	}
	m.StaleSince = ""
	m.MemUsedBytes, m.MemTotal = mr.MemUsed, mr.MemTotal
	s.memRing.add(minute, memPct(mr.MemUsed, mr.MemTotal))
	if mr.HasCPU {
		if s.havePrevCPU {
			if pct, ok := cpuBusyPct(s.prevIdle, s.prevTotal, mr.CPUIdle, mr.CPUTotal); ok {
				m.CPUPct = &pct
				s.cpuRing.add(minute, pct)
			}
		}
		s.prevIdle, s.prevTotal, s.havePrevCPU = mr.CPUIdle, mr.CPUTotal, true
	}
	if m.CPUPct != nil {
		m.CPUSeriesPct = s.cpuRing.series(minute)
	}
	m.MemSeriesPct = s.memRing.series(minute)
	s.snap.Machine = m
}

// sampleLatency runs with the lock held. A tick with no probe samples (the
// probe has not run yet) leaves the section as it was.
func (s *Sampler) sampleLatency(now time.Time) {
	p99, _, ok := s.src.Drift.Drain()
	if !ok {
		return
	}
	minute := now.UTC().Truncate(time.Minute)
	ms := round1(float64(p99) / float64(time.Millisecond))
	s.driftRing.add(minute, ms)
	s.snap.Latency = &Latency{DriftP99Ms: ms, DriftSeriesMs: s.driftRing.series(minute)}
}

// tokens reads two windows, one query each: the last hour by the minute (which
// also gives the 5 minute rate) and the last 24 hours in a single bucket.
func (s *Sampler) tokens(now time.Time) (*Tokens, error) {
	now = now.UTC()
	minute := now.Truncate(time.Minute)
	end := minute.Add(time.Minute)
	hour, err := s.src.Usage(end.Add(-time.Hour), end, 60, "")
	if err != nil {
		return nil, err
	}
	day, err := s.src.Usage(now.Add(-24*time.Hour), now.Add(time.Second), 86400, "")
	if err != nil {
		return nil, err
	}
	t := &Tokens{
		ByCause24h:   map[string]int64{},
		SeriesPerMin: make([]int64, 60),
		SeriesEnd:    rfc(minute),
	}
	first := end.Add(-time.Hour)
	for _, b := range hour.Buckets {
		i := int(b.Start.Sub(first) / time.Minute)
		if i < 0 || i >= 60 {
			continue
		}
		t.SeriesPerMin[i] += b.Total.Input + b.Total.Output + b.Total.CacheWrite5m + b.Total.CacheWrite1h
		addKinds(&t.Hour, &b.Total)
	}
	var sum int64
	for _, v := range t.SeriesPerMin[55:] {
		sum += v
	}
	t.PerMin5m = sum / 5
	for _, b := range day.Buckets {
		addKinds(&t.Last24h, &b.Total)
		for cause, g := range b.Causes {
			t.ByCause24h[cause] += g.Input + g.Output + g.CacheWrite5m + g.CacheWrite1h
		}
	}
	return t, nil
}

func addKinds(k *Kinds, g *store.UsageSums) {
	k.In += g.Input
	k.Out += g.Output
	k.CacheRead += g.CacheRead
	k.CacheWrite += g.CacheWrite5m + g.CacheWrite1h
}
