package roomstats

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

type fakeClock struct {
	mu   sync.Mutex
	now  time.Time
	tick chan time.Time
	disk chan time.Time
}

func newClock(now time.Time) *fakeClock {
	return &fakeClock{now: now, tick: make(chan time.Time), disk: make(chan time.Time)}
}
func (c *fakeClock) Now() time.Time  { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *fakeClock) set(t time.Time) { c.mu.Lock(); c.now = t; c.mu.Unlock() }
func (c *fakeClock) Ticker(d time.Duration) (<-chan time.Time, func()) {
	if d == Interval {
		return c.tick, func() {}
	}
	return c.disk, func() {}
}

var t0 = time.Date(2026, 9, 29, 19, 4, 10, 0, time.UTC)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "atrium.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func addUsage(t *testing.T, s *store.Store, at time.Time, cause string, in, out, cw, cr int64) {
	t.Helper()
	if err := s.AddSessionUsage(&store.SessionUsage{
		TaskID: "t1", Started: at, Ended: at, Cause: cause,
		Input: in, Output: out, CacheWrite5m: cw, CacheRead: cr,
	}); err != nil {
		t.Fatal(err)
	}
}

func fullSources(s *store.Store, clk Clock, out *[][]byte) Sources {
	return Sources{
		Room: "sg3", Started: t0.Add(-time.Hour), Clock: clk,
		Usage:        s.UsageBuckets,
		ProcessTimes: func() (time.Duration, uint64, error) { return 3 * time.Second, 212000000, nil },
		Disk:         func() (string, uint64, uint64, error) { return "C:/x", 392, 512, nil },
		DBBytes:      func() (int64, error) { return 740, nil },
		Worktrees:    func() (int, error) { return 38, nil },
		Runners: func() ([]Runner, error) {
			return []Runner{{"claude", true}, {"ollama", false}}, nil
		},
		Publish: func(b []byte) { *out = append(*out, b) },
	}
}

func TestSnapshotShape(t *testing.T) {
	st := openStore(t)
	var pushed [][]byte
	sm := New(fullSources(st, newClock(t0), &pushed))
	sm.SampleDisk()
	sm.Tick()

	var got map[string]any
	if err := json.Unmarshal(sm.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	keys := func(m map[string]any) []string {
		var k []string
		for n := range m {
			k = append(k, n)
		}
		return k
	}
	for _, k := range []string{"v", "room", "at", "tokens", "process", "disk", "runners"} {
		if _, ok := got[k]; !ok {
			t.Errorf("missing %q in %v", k, keys(got))
		}
	}
	// Second wave: ABSENT, not zero and not null.
	if _, ok := got["machine"]; ok {
		t.Error("machine must be absent in wave one")
	}
	if got["v"].(float64) != 1 || got["room"] != "sg3" || got["at"] != "2026-09-29T19:04:10Z" {
		t.Errorf("header wrong: %v", got)
	}
	for _, r := range got["runners"].([]any) {
		if len(r.(map[string]any)) != 2 {
			t.Errorf("runner has extra fields: %v", r)
		}
	}
	tok := got["tokens"].(map[string]any)
	for _, k := range []string{"per_min_5m", "hour", "last_24h", "by_cause_24h", "series_per_min", "series_end"} {
		if _, ok := tok[k]; !ok {
			t.Errorf("tokens.%s missing", k)
		}
	}
	if tok["series_end"] != "2026-09-29T19:04:00Z" || len(tok["series_per_min"].([]any)) != 60 {
		t.Errorf("series wrong: %v", tok["series_end"])
	}
	if _, ok := tok["stale_since"]; ok {
		t.Error("fresh section must not be stale")
	}
	proc := got["process"].(map[string]any)
	for _, k := range []string{"cpu_pct", "rss_bytes", "heap_bytes", "goroutines", "started_at"} {
		if _, ok := proc[k]; !ok {
			t.Errorf("process.%s missing", k)
		}
	}
	disk := got["disk"].(map[string]any)
	for _, k := range []string{"path", "free_bytes", "total_bytes", "db_bytes", "worktrees"} {
		if _, ok := disk[k]; !ok {
			t.Errorf("disk.%s missing", k)
		}
	}
	if proc["cpu_pct"].(float64) != 0.1 { // 3s of cpu over one hour
		t.Errorf("cpu_pct %v", proc["cpu_pct"])
	}
}

func TestTokenArithmetic(t *testing.T) {
	st := openStore(t)
	// Inside the current minute, 3 minutes ago, 30 minutes ago, 5 hours ago.
	addUsage(t, st, t0.Add(-2*time.Second), "operator", 100, 10, 5, 1000)
	addUsage(t, st, t0.Add(-3*time.Minute), "peer", 200, 20, 0, 5000)
	addUsage(t, st, t0.Add(-30*time.Minute), "keepalive", 300, 30, 10, 7000)
	addUsage(t, st, t0.Add(-5*time.Hour), "operator", 400, 40, 0, 9000)
	addUsage(t, st, t0.Add(-30*time.Hour), "operator", 999, 999, 999, 999)

	sm := New(fullSources(st, newClock(t0), new([][]byte)))
	sm.Tick()
	var snap Snapshot
	if err := json.Unmarshal(sm.Bytes(), &snap); err != nil {
		t.Fatal(err)
	}
	tk := snap.Tokens
	// The 5 minute rate counts the first two rows, cache_read left out.
	if want := int64((115 + 220) / 5); tk.PerMin5m != want {
		t.Errorf("per_min_5m %d want %d", tk.PerMin5m, want)
	}
	if tk.SeriesPerMin[59] != 115 || tk.SeriesPerMin[56] != 220 || tk.SeriesPerMin[29] != 340 {
		t.Errorf("series %v", tk.SeriesPerMin)
	}
	var sum int64
	for _, v := range tk.SeriesPerMin {
		sum += v
	}
	if sum != 115+220+340 {
		t.Errorf("series sum %d", sum)
	}
	if want := (Kinds{In: 600, Out: 60, CacheRead: 13000, CacheWrite: 15}); tk.Hour != want {
		t.Errorf("hour %+v want %+v", tk.Hour, want)
	}
	if want := (Kinds{In: 1000, Out: 100, CacheRead: 22000, CacheWrite: 15}); tk.Last24h != want {
		t.Errorf("last_24h %+v want %+v", tk.Last24h, want)
	}
	if want := map[string]int64{"operator": 115 + 440, "peer": 220, "keepalive": 340}; !reflect.DeepEqual(tk.ByCause24h, want) {
		t.Errorf("by_cause_24h %v want %v", tk.ByCause24h, want)
	}
}

func TestCadenceIsTenSecondsAndDiskIsSeparate(t *testing.T) {
	st := openStore(t)
	clk := newClock(t0)
	var mu sync.Mutex
	var pushed int
	diskCalls := 0
	src := fullSources(st, clk, new([][]byte))
	src.Publish = func([]byte) { mu.Lock(); pushed++; mu.Unlock() }
	src.Disk = func() (string, uint64, uint64, error) {
		mu.Lock()
		diskCalls++
		mu.Unlock()
		return "C:/x", 1, 2, nil
	}
	sm := New(src)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() { sm.Run(stop); close(done) }()
	count := func() (int, int) { mu.Lock(); defer mu.Unlock(); return pushed, diskCalls }
	wait := func(want int) {
		t.Helper()
		for i := 0; i < 500; i++ {
			if p, _ := count(); p == want {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		p, _ := count()
		t.Fatalf("pushed %d want %d", p, want)
	}
	wait(1) // the initial sample
	for i := 2; i <= 4; i++ {
		clk.set(clk.Now().Add(Interval))
		clk.tick <- clk.Now()
		wait(i)
	}
	if _, d := count(); d != 1 {
		t.Errorf("disk sampled %d times across three ticks, want 1", d)
	}
	clk.disk <- clk.Now()
	for i := 0; i < 500; i++ {
		if _, d := count(); d == 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, d := count(); d != 2 {
		t.Errorf("disk timer did not sample, calls %d", d)
	}
	close(stop)
	<-done
}

func TestGetEqualsLastPushed(t *testing.T) {
	st := openStore(t)
	var pushed [][]byte
	sm := New(fullSources(st, newClock(t0), &pushed))
	if sm.Bytes() != nil {
		t.Error("bytes before the first sample")
	}
	sm.Tick()
	sm.Tick()
	if len(pushed) != 2 || string(sm.Bytes()) != string(pushed[1]) {
		t.Error("Bytes is not the last pushed snapshot")
	}
}

func TestFailingSamplerKeepsLastGoodValuesAndMarksStale(t *testing.T) {
	st := openStore(t)
	clk := newClock(t0)
	fail := false
	src := fullSources(st, clk, new([][]byte))
	src.ProcessTimes = func() (time.Duration, uint64, error) {
		if fail {
			return 0, 0, errors.New("boom")
		}
		return time.Second, 777, nil
	}
	src.Disk = func() (string, uint64, uint64, error) {
		if fail {
			return "", 0, 0, errors.New("boom")
		}
		return "C:/x", 5, 9, nil
	}
	src.Runners = func() ([]Runner, error) {
		if fail {
			return nil, errors.New("boom")
		}
		return []Runner{{"claude", true}}, nil
	}
	sm := New(src)
	sm.SampleDisk()
	sm.Tick()

	fail = true
	clk.set(t0.Add(10 * time.Second))
	sm.SampleDisk()
	sm.Tick()
	clk.set(t0.Add(20 * time.Second))
	sm.SampleDisk()
	sm.Tick()

	var snap Snapshot
	json.Unmarshal(sm.Bytes(), &snap)
	if snap.Process == nil || snap.Process.RSSBytes != 777 {
		t.Fatalf("process lost its last good values: %+v", snap.Process)
	}
	if snap.Process.StaleSince != "2026-09-29T19:04:20Z" {
		t.Errorf("process stale_since %q, want the first failed sample", snap.Process.StaleSince)
	}
	if snap.Disk == nil || snap.Disk.FreeBytes != 5 || snap.Disk.StaleSince == "" {
		t.Errorf("disk %+v", snap.Disk)
	}
	if len(snap.Runners) != 1 || snap.RunnersStaleSince == "" {
		t.Errorf("runners %+v stale %q", snap.Runners, snap.RunnersStaleSince)
	}
	if snap.Tokens == nil || snap.Tokens.StaleSince != "" {
		t.Error("tokens did not fail and must not be stale")
	}

	fail = false
	clk.set(t0.Add(30 * time.Second))
	sm.Tick()
	snap = Snapshot{}
	json.Unmarshal(sm.Bytes(), &snap)
	if snap.Process.StaleSince != "" {
		t.Error("a good sample clears the stale mark")
	}
}

func TestASectionNeverSampledIsAbsentNotZero(t *testing.T) {
	src := Sources{Room: "r", Clock: newClock(t0),
		Disk: func() (string, uint64, uint64, error) { return "", 0, 0, errors.New("no") }}
	sm := New(src)
	sm.SampleDisk()
	sm.Tick()
	var got map[string]any
	json.Unmarshal(sm.Bytes(), &got)
	if _, ok := got["disk"]; ok {
		t.Errorf("disk must be absent, got %v", got["disk"])
	}
}

func TestWorktreesCountsKnownExistingCardWorktreesOnly(t *testing.T) {
	root := t.TempDir()
	a, b := filepath.Join(root, "a"), filepath.Join(root, "b")
	os.Mkdir(a, 0o755)
	os.Mkdir(b, 0o755)
	os.Mkdir(filepath.Join(root, "unknown"), 0o755) // on disk, no card knows it
	file := filepath.Join(root, "file")
	os.WriteFile(file, nil, 0o644)
	got := CountWorktrees([]string{a, a, b, filepath.Join(root, "gone"), file, ""})
	if got != 2 {
		t.Errorf("worktrees %d, want 2", got)
	}
}
