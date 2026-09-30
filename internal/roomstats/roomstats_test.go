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
		Machine:      (&machineSeq{}).read,
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
	// One tick has memory but no CPU delta yet.
	mach := got["machine"].(map[string]any)
	if _, ok := mach["cpu_pct"]; ok {
		t.Error("cpu_pct needs two samples")
	}
	if _, ok := mach["mem_used_bytes"]; !ok {
		t.Error("mem_used_bytes missing after one sample")
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

// machineSeq feeds successive readings, or an error while fail is set.
type machineSeq struct {
	n    uint64
	fail bool
}

func (m *machineSeq) read() (MachineReading, error) {
	if m.fail {
		return MachineReading{}, errors.New("boom")
	}
	m.n++
	// each reading: 100 more ticks, 25 of them idle, so 75% busy
	return MachineReading{HasCPU: true, CPUIdle: m.n * 25, CPUTotal: m.n * 100, MemUsed: 4 << 30, MemTotal: 16 << 30}, nil
}

func TestMachineAbsentThenPresentWithAllFields(t *testing.T) {
	st := openStore(t)
	clk := newClock(t0)
	seq := &machineSeq{}
	src := fullSources(st, clk, new([][]byte))
	src.Machine = seq.read
	sm := New(src)
	if sm.Bytes() != nil {
		t.Fatal("bytes before any sample")
	}
	sm.Tick()
	clk.set(t0.Add(10 * time.Second))
	sm.Tick()
	var got map[string]any
	json.Unmarshal(sm.Bytes(), &got)
	m := got["machine"].(map[string]any)
	for _, k := range []string{"cpu_pct", "cpu_series_pct", "mem_used_bytes", "mem_total_bytes", "mem_series_pct"} {
		if _, ok := m[k]; !ok {
			t.Errorf("machine.%s missing", k)
		}
	}
	if m["cpu_pct"].(float64) != 75 || m["mem_used_bytes"].(float64) != 4<<30 {
		t.Errorf("machine %v", m)
	}
	tok := got["tokens"].(map[string]any)
	for _, k := range []string{"cpu_series_pct", "mem_series_pct"} {
		s := m[k].([]any)
		if len(s) != len(tok["series_per_min"].([]any)) {
			t.Errorf("%s length %d", k, len(s))
		}
	}
	// Both end on series_end's minute: 19:04, this minute's average.
	if last := m["cpu_series_pct"].([]any)[59].(float64); last != 75 {
		t.Errorf("cpu series end %v", last)
	}
	if last := m["mem_series_pct"].([]any)[59].(float64); last != 25 {
		t.Errorf("mem series end %v", last)
	}
	if _, ok := m["stale_since"]; ok {
		t.Error("fresh machine must not be stale")
	}
}

func TestMachineSeriesFollowsTheMinuteGrid(t *testing.T) {
	st := openStore(t)
	clk := newClock(t0)
	seq := &machineSeq{}
	src := fullSources(st, clk, new([][]byte))
	src.Machine = seq.read
	sm := New(src)
	sm.Tick()
	clk.set(t0.Add(3 * time.Minute)) // 19:07
	sm.Tick()
	var snap Snapshot
	json.Unmarshal(sm.Bytes(), &snap)
	if snap.Tokens.SeriesEnd != "2026-09-29T19:07:00Z" {
		t.Fatalf("series_end %s", snap.Tokens.SeriesEnd)
	}
	cs := snap.Machine.CPUSeriesPct
	if len(cs) != 60 || cs[59] != 75 || cs[58] != 0 {
		t.Errorf("cpu series %v", cs)
	}
	ms := snap.Machine.MemSeriesPct
	if ms[59] != 25 || ms[56] != 25 || ms[57] != 0 {
		t.Errorf("mem series %v", ms)
	}
}

func TestMachineFailureKeepsValuesAndMarksStale(t *testing.T) {
	st := openStore(t)
	clk := newClock(t0)
	seq := &machineSeq{}
	src := fullSources(st, clk, new([][]byte))
	src.Machine = seq.read
	sm := New(src)
	sm.Tick()
	clk.set(t0.Add(10 * time.Second))
	sm.Tick()
	seq.fail = true
	clk.set(t0.Add(20 * time.Second))
	sm.Tick()
	clk.set(t0.Add(30 * time.Second))
	sm.Tick()
	var snap Snapshot
	json.Unmarshal(sm.Bytes(), &snap)
	m := snap.Machine
	if m == nil || m.CPUPct == nil || *m.CPUPct != 75 || m.MemTotal != 16<<30 {
		t.Fatalf("machine lost its last good values: %+v", m)
	}
	if m.StaleSince != "2026-09-29T19:04:30Z" {
		t.Errorf("stale_since %q, want the first failed sample", m.StaleSince)
	}
	seq.fail = false
	clk.set(t0.Add(40 * time.Second))
	sm.Tick()
	snap = Snapshot{}
	json.Unmarshal(sm.Bytes(), &snap)
	if snap.Machine.StaleSince != "" {
		t.Error("a good sample clears the stale mark")
	}
}

func TestMachineNeverSampledIsAbsent(t *testing.T) {
	src := Sources{Room: "r", Clock: newClock(t0),
		Machine: func() (MachineReading, error) { return MachineReading{}, errors.New("no") }}
	sm := New(src)
	sm.Tick()
	var got map[string]any
	json.Unmarshal(sm.Bytes(), &got)
	if _, ok := got["machine"]; ok {
		t.Errorf("machine must be absent, got %v", got["machine"])
	}
}

func TestMachineWithoutCPUKeepsMemory(t *testing.T) {
	src := Sources{Room: "r", Clock: newClock(t0),
		Machine: func() (MachineReading, error) { return MachineReading{MemUsed: 1, MemTotal: 4}, nil }}
	sm := New(src)
	sm.Tick()
	sm.Tick()
	var got map[string]any
	json.Unmarshal(sm.Bytes(), &got)
	m := got["machine"].(map[string]any)
	if _, ok := m["cpu_pct"]; ok {
		t.Error("cpu_pct must be absent where the platform has none")
	}
	if _, ok := m["cpu_series_pct"]; ok {
		t.Error("cpu_series_pct must be absent where the platform has none")
	}
	if m["mem_series_pct"].([]any)[59].(float64) != 25 {
		t.Errorf("mem series %v", m["mem_series_pct"])
	}
}

func TestCPUBusyPctArithmetic(t *testing.T) {
	cases := []struct {
		pi, pt, i, t uint64
		want         float64
		ok           bool
	}{
		{0, 0, 25, 100, 75, true},
		{100, 200, 150, 300, 50, true},
		{100, 200, 100, 300, 100, true},
		{100, 200, 200, 300, 0, true},
		{100, 200, 100, 200, 0, false}, // did not advance
		{100, 200, 90, 300, 0, false},  // idle went backwards
		{100, 200, 300, 250, 0, false}, // more idle than time
		{100, 200, 101, 203, 66.7, true},
	}
	for _, c := range cases {
		got, ok := cpuBusyPct(c.pi, c.pt, c.i, c.t)
		if ok != c.ok || got != c.want {
			t.Errorf("%+v: got %v %v", c, got, ok)
		}
	}
}

const procStatFixture = `cpu  4705 150 1120 16250 520 30 45 10 0 0
cpu0 1175 30 280 4060 130 8 11 2 0 0
intr 12345 0 0
ctxt 999
`

func TestParseProcStat(t *testing.T) {
	idle, total, err := parseProcStat(procStatFixture)
	if err != nil {
		t.Fatal(err)
	}
	if idle != 16250+520 || total != 4705+150+1120+16250+520+30+45+10 {
		t.Errorf("idle %d total %d", idle, total)
	}
	if _, _, err := parseProcStat("cpu0 1 2 3 4 5\n"); err == nil {
		t.Error("no aggregate line must be an error")
	}
	if _, _, err := parseProcStat("cpu  1 2 x 4 5\n"); err == nil {
		t.Error("a garbled field must be an error")
	}
	// An old kernel with fewer columns still parses.
	if idle, total, err = parseProcStat("cpu  10 0 5 80 5\n"); err != nil || idle != 85 || total != 100 {
		t.Errorf("short line: %d %d %v", idle, total, err)
	}
}

const meminfoFixture = `MemTotal:       16384000 kB
MemFree:          812000 kB
MemAvailable:    8192000 kB
Buffers:          100000 kB
`

func TestParseMeminfo(t *testing.T) {
	total, avail, err := parseMeminfo(meminfoFixture)
	if err != nil {
		t.Fatal(err)
	}
	if total != 16384000*1024 || avail != 8192000*1024 {
		t.Errorf("total %d avail %d", total, avail)
	}
	if _, _, err := parseMeminfo("MemTotal: 1 kB\nMemFree: 1 kB\n"); err == nil {
		t.Error("missing MemAvailable must be an error, not a guess from MemFree")
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
