package roomstats

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

// The alert is what the operator should hear about a room with nobody there to
// notice: the machine hot for HotFor with no agent mid-turn, or an enabled
// runner that does not resolve. The hub raises a growler from it and the board
// badges the room tile. Clearing the condition clears the alert, which re-arms
// it. See f-room-cpu-alert.
const (
	HotPct   = 80.0
	HotFor   = 10 * time.Minute
	TopProcs = 5
	// procsEvery is how often the process list is read again while hot.
	procsEvery = time.Minute
	cmdMax     = 120
)

// Process kinds. Only an orphan of a closed card is offered a kill.
const (
	ProcCardLive   = "card-live"
	ProcCardClosed = "card-closed"
	ProcAtrium     = "atrium"
	ProcForeign    = "foreign"
)

// Alert is absent when there is nothing to say.
type Alert struct {
	HotSince       string   `json:"hot_since,omitempty"`
	CPUPct         float64  `json:"cpu_pct,omitempty"`
	Procs          []Proc   `json:"procs,omitempty"`
	RunnersMissing []string `json:"runners_missing,omitempty"`
}

// Proc is one process. CPUPct is of one core, as ps reports it.
type Proc struct {
	PID     int     `json:"pid"`
	PPID    int     `json:"ppid"`
	Etime   string  `json:"etime"`
	CPUPct  float64 `json:"cpu_pct"`
	Command string  `json:"command"`
	Cwd     string  `json:"cwd,omitempty"`
	Kind    string  `json:"kind"`
	Card    string  `json:"card,omitempty"`
}

// TopByCPU is the n busiest, busiest first, each command cut to cmdMax.
func TopByCPU(ps []Proc, n int) []Proc {
	sort.SliceStable(ps, func(i, j int) bool { return ps[i].CPUPct > ps[j].CPUPct })
	if len(ps) > n {
		ps = ps[:n]
	}
	for i := range ps {
		if r := []rune(ps[i].Command); len(r) > cmdMax {
			ps[i].Command = string(r[:cmdMax])
		}
	}
	return ps
}

// parsePS reads `ps -axo pid=,ppid=,etime=,pcpu=,command=`.
func parsePS(text string) []Proc {
	var out []Proc
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 5 {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		ppid, err2 := strconv.Atoi(f[1])
		pct, err3 := strconv.ParseFloat(f[3], 64)
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		out = append(out, Proc{PID: pid, PPID: ppid, Etime: f[2], CPUPct: pct, Command: strings.Join(f[4:], " ")})
	}
	return out
}

// etime is d the way ps prints it, [[dd-]hh:]mm:ss.
func etime(d time.Duration) string {
	s := int(d / time.Second)
	days, h, m := s/86400, s/3600%24, s/60%60
	s %= 60
	switch {
	case days > 0:
		return strconv.Itoa(days) + "-" + pad2(h) + ":" + pad2(m) + ":" + pad2(s)
	case h > 0:
		return pad2(h) + ":" + pad2(m) + ":" + pad2(s)
	}
	return pad2(m) + ":" + pad2(s)
}

func pad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// sampleAlert runs with the lock held, after sampleMachine and runners.
func (s *Sampler) sampleAlert(now time.Time, agents int, procs []Proc, procsOK bool) {
	var a Alert
	for _, r := range s.snap.Runners {
		if r.Enabled && !r.Resolves {
			a.RunnersMissing = append(a.RunnersMissing, r.Kind)
		}
	}
	m := s.snap.Machine
	if m != nil && m.CPUPct != nil && m.StaleSince == "" && *m.CPUPct >= HotPct && agents == 0 {
		if s.hotStart.IsZero() {
			s.hotStart = now
		}
	} else {
		s.hotStart, s.hotProcs, s.procsAt = time.Time{}, nil, time.Time{}
	}
	if procsOK {
		s.hotProcs, s.procsAt = procs, now
	}
	if !s.hotStart.IsZero() && now.Sub(s.hotStart) >= HotFor && (s.src.Procs == nil || s.hotProcs != nil) {
		a.HotSince, a.CPUPct, a.Procs = rfc(s.hotStart), *m.CPUPct, s.hotProcs
	}
	if a.HotSince == "" && a.RunnersMissing == nil {
		s.snap.Alert = nil
		return
	}
	s.snap.Alert = &a
}

// wantProcs is whether this tick reads the process list: hot long enough, and
// none read in the last procsEvery.
func (s *Sampler) wantProcs(now time.Time) bool {
	return s.src.Procs != nil && !s.hotStart.IsZero() && now.Sub(s.hotStart) >= HotFor &&
		now.Sub(s.procsAt) >= procsEvery
}
