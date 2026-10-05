//go:build darwin

package roomstats

import (
	"bufio"
	"os/exec"
	"regexp"
	"strconv"
	"sync"
	"time"
)

// macOS has no kern.cp_time, and host_statistics is cgo. The one pure Go way to
// the machine CPU is top, so one long-lived `top -l 0` streams a sample every
// few seconds and no process is started per reading. Each sample is a percent
// over its own interval, so it is turned into synthetic cumulative ticks (a
// fixed number per sample) and the rest of the pipeline treats it like any
// other platform's counters.
const (
	topSampleSecs = 5
	ticksPerSamp  = 1000
)

var topIdleRe = regexp.MustCompile(`CPU usage:.*?([0-9.]+)% idle`)

// parseTopIdle is the idle percent from top's "CPU usage:" line.
func parseTopIdle(line string) (float64, bool) {
	m := topIdleRe.FindStringSubmatch(line)
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil || v < 0 || v > 100 {
		return 0, false
	}
	return v, true
}

type topCPU struct {
	mu          sync.Mutex
	running     bool
	idle, total uint64
	retryAt     time.Time
}

var topSampler topCPU

// read starts the stream on first use, restarting it after a pause if top
// exits. ok is false until the first sample lands.
func (t *topCPU) read() (idle, total uint64, ok bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.running && time.Now().After(t.retryAt) {
		t.running = true
		go t.run()
	}
	return t.idle, t.total, t.total > 0
}

func (t *topCPU) run() {
	defer func() {
		t.mu.Lock()
		t.running = false
		t.retryAt = time.Now().Add(30 * time.Second)
		t.mu.Unlock()
	}()
	cmd := exec.Command("top", "-l", "0", "-n", "0", "-s", strconv.Itoa(topSampleSecs))
	out, err := cmd.StdoutPipe()
	if err != nil || cmd.Start() != nil {
		return
	}
	defer func() {
		cmd.Process.Kill()
		cmd.Wait()
	}()
	skipFirst := true
	sc := bufio.NewScanner(out)
	for sc.Scan() {
		idle, ok := parseTopIdle(sc.Text())
		if !ok {
			continue
		}
		if skipFirst {
			// top's first sample has no interval behind it
			skipFirst = false
			continue
		}
		t.mu.Lock()
		t.idle += uint64(idle*ticksPerSamp/100 + 0.5)
		t.total += ticksPerSamp
		t.mu.Unlock()
	}
}
