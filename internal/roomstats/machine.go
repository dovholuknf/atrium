package roomstats

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Machine is the whole machine, not this process. CPU is absent where the
// platform cannot give it without cgo (darwin), and until the second sample,
// since a percent needs two readings.
type Machine struct {
	CPUPct       *float64   `json:"cpu_pct,omitempty"`
	CPUSeriesPct []*float64 `json:"cpu_series_pct,omitempty"`
	MemUsedBytes uint64     `json:"mem_used_bytes"`
	MemTotal     uint64     `json:"mem_total_bytes"`
	MemSeriesPct []*float64 `json:"mem_series_pct"`
	StaleSince   string     `json:"stale_since,omitempty"`
}

// MachineReading is one look at the machine. CPU counters are cumulative and
// only mean something as a difference between two readings. HasCPU false says
// this platform has none.
type MachineReading struct {
	HasCPU            bool
	CPUIdle, CPUTotal uint64
	MemUsed, MemTotal uint64
}

// cpuBusyPct is the share of the interval the CPUs were not idle, from two
// cumulative readings. ok is false when the counters did not advance, or went
// backwards, which is a reading to skip and not a zero.
func cpuBusyPct(prevIdle, prevTotal, idle, total uint64) (float64, bool) {
	if total <= prevTotal || idle < prevIdle {
		return 0, false
	}
	dt, di := total-prevTotal, idle-prevIdle
	if di > dt {
		return 0, false
	}
	return round1(float64(dt-di) / float64(dt) * 100), true
}

func round1(v float64) float64 { return float64(int(v*10+0.5)) / 10 }

// memPct is used over total as a percent, one decimal.
func memPct(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return round1(float64(used) / float64(total) * 100)
}

// parseProcStat reads the aggregate "cpu " line of /proc/stat. Idle counts
// idle and iowait, total the first eight fields. guest is already inside user.
func parseProcStat(text string) (idle, total uint64, err error) {
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 5 || f[0] != "cpu" {
			continue
		}
		var v [8]uint64
		for i := 0; i < 8 && i+1 < len(f); i++ {
			if v[i], err = strconv.ParseUint(f[i+1], 10, 64); err != nil {
				return 0, 0, err
			}
			total += v[i]
		}
		return v[3] + v[4], total, nil
	}
	return 0, 0, fmt.Errorf("no cpu line in /proc/stat")
}

// parseMeminfo returns MemTotal and MemAvailable in bytes. Available, not
// free: cache the kernel would give back is not used.
func parseMeminfo(text string) (total, avail uint64, err error) {
	var haveT, haveA bool
	for _, line := range strings.Split(text, "\n") {
		name, rest, ok := strings.Cut(line, ":")
		if !ok || (name != "MemTotal" && name != "MemAvailable") {
			continue
		}
		f := strings.Fields(rest)
		if len(f) == 0 {
			continue
		}
		n, e := strconv.ParseUint(f[0], 10, 64)
		if e != nil {
			return 0, 0, e
		}
		if len(f) > 1 && strings.EqualFold(f[1], "kB") {
			n *= 1024
		}
		if name == "MemTotal" {
			total, haveT = n, true
		} else {
			avail, haveA = n, true
		}
	}
	if !haveT || !haveA {
		return 0, 0, fmt.Errorf("MemTotal or MemAvailable missing from /proc/meminfo")
	}
	if avail > total {
		avail = total
	}
	return total, avail, nil
}

// minuteAvg is a ring of per-minute averages, keyed by the minute. The series
// is read out on the same 60 minute grid as tokens.series_per_min: oldest
// first, the last point the minute series_end names. A minute nothing was
// sampled in is null: 0 would claim an idle, empty machine.
type minuteAvg struct {
	m map[int64]*avgAcc
}

type avgAcc struct {
	sum float64
	n   int
}

func newMinuteAvg() *minuteAvg { return &minuteAvg{m: map[int64]*avgAcc{}} }

func (r *minuteAvg) add(minute time.Time, v float64) {
	k := minute.Unix() / 60
	a := r.m[k]
	if a == nil {
		a = &avgAcc{}
		r.m[k] = a
	}
	a.sum += v
	a.n++
	for old := range r.m {
		if old <= k-60 {
			delete(r.m, old)
		}
	}
}

func (r *minuteAvg) series(minute time.Time) []*float64 {
	out := make([]*float64, 60)
	k := minute.Unix() / 60
	for i := range out {
		if a := r.m[k-int64(59-i)]; a != nil {
			v := round1(a.sum / float64(a.n))
			out[i] = &v
		}
	}
	return out
}
