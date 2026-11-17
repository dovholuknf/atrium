package daemon

import (
	"context"
	"fmt"
	"log"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/daemon/hublink"
	"github.com/dovholuknf/atrium/internal/roomstats"
	"github.com/dovholuknf/atrium/internal/store"
)

var (
	HostWatchEvery = 30 * time.Second
	HostWatchStart = 20 * time.Second
)

const (
	HostHogPctDefault     = 90
	HostHogMinutesDefault = 5
	// hostBareTicks is how many samples in a row an sshd session must show no TCP connection, so one caught between
	// accept and its socket is not flagged.
	hostBareTicks = 2
)

const (
	HostShapeHog      = "hog"
	HostShapeSSHDBare = "sshd-no-tcp"
)

type HostFlag struct {
	PID      int       `json:"pid"`
	Name     string    `json:"name"`
	Shape    string    `json:"shape"`
	CPUPct   float64   `json:"cpu_pct"`
	RSSBytes uint64    `json:"rss_bytes"`
	IOPerSec int64     `json:"io_bytes_per_sec"`
	Since    time.Time `json:"since"`
	Started  time.Time `json:"started,omitempty"`
	Owner    string    `json:"owner,omitempty"`
	Whose    string    `json:"whose,omitempty"`
	Limits   []string  `json:"limits,omitempty"`
	Stop     string    `json:"stop"`
	Say      string    `json:"say"`
}

type HostStatus struct {
	At         time.Time  `json:"at,omitempty"`
	Procs      int        `json:"procs"`
	HogPct     int        `json:"hog_pct"`
	HogMinutes int        `json:"hog_minutes"`
	Flags      []HostFlag `json:"flags"`
	Limits     []string   `json:"limits,omitempty"`
	Err        string     `json:"error,omitempty"`
	Notice     string     `json:"notice,omitempty"`
}

type hostTrack struct {
	started   time.Time
	cpu       time.Duration
	io        uint64
	at        time.Time
	rate      float64
	ioRate    int64
	busySince time.Time
	bare      int
	bareSince time.Time
	whose     *string
}

type hostWatch struct {
	mu     sync.Mutex
	track  map[int]*hostTrack
	status HostStatus
}

func (d *Daemon) hostLevels() (pct, minutes int) {
	read := func(key string, def int) int {
		if v, err := d.st.Setting(key); err == nil {
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
				return n
			}
		}
		return def
	}
	return read(store.SettingHostHogPct, HostHogPctDefault), read(store.SettingHostHogMinutes, HostHogMinutesDefault)
}

// hostObserve folds one reading into the tracks and answers what is flagged now, before owner, whose and wording.
func (w *hostWatch) hostObserve(procs []roomstats.HostProc, conns func([]int) map[int]int, now time.Time,
	pct float64, hogFor time.Duration) (flags []HostFlag, limits []string) {
	if w.track == nil {
		w.track = map[int]*hostTrack{}
	}
	byPID := make(map[int]roomstats.HostProc, len(procs))
	for _, p := range procs {
		byPID[p.PID] = p
	}
	for pid, t := range w.track {
		if p, ok := byPID[pid]; !ok || !p.Started.Equal(t.started) {
			delete(w.track, pid)
		}
	}
	for _, p := range procs {
		t := w.track[p.PID]
		if t == nil {
			t = &hostTrack{started: p.Started, cpu: p.CPU, io: p.IO, at: now, ioRate: -1}
			if age := now.Sub(p.Started); !p.Started.IsZero() && age > 0 && float64(p.CPU)/float64(age)*100 >= pct {
				t.busySince = p.Started
			}
			w.track[p.PID] = t
			continue
		}
		dt := now.Sub(t.at)
		if dt <= 0 {
			continue
		}
		t.rate = float64(p.CPU-t.cpu) / float64(dt) * 100
		t.ioRate = -1
		if p.HasIO && p.IO >= t.io {
			t.ioRate = int64(float64(p.IO-t.io) / dt.Seconds())
		}
		if t.rate < pct {
			t.busySince = time.Time{}
		} else if t.busySince.IsZero() {
			t.busySince = t.at
		}
		t.cpu, t.io, t.at = p.CPU, p.IO, now
	}
	flag := func(p roomstats.HostProc, t *hostTrack, shape string, since time.Time) {
		flags = append(flags, HostFlag{PID: p.PID, Name: p.Name, Shape: shape, CPUPct: roundPct(t.rate), RSSBytes: p.RSS,
			IOPerSec: t.ioRate, Since: since, Started: p.Started, Owner: p.Owner})
	}
	for _, p := range procs {
		if t := w.track[p.PID]; !t.busySince.IsZero() && t.rate >= pct && now.Sub(t.busySince) >= hogFor {
			flag(p, t, HostShapeHog, t.busySince)
		}
	}
	bare, blind := sshdBare(procs, byPID, conns)
	for _, p := range procs {
		t := w.track[p.PID]
		if !bare[p.PID] {
			t.bare, t.bareSince = 0, time.Time{}
			continue
		}
		if t.bare++; t.bare == 1 {
			t.bareSince = now
		}
		if t.bare >= hostBareTicks && !hostFlagged(flags, p.PID) {
			flag(p, t, HostShapeSSHDBare, t.bareSince)
		}
	}
	if len(blind) > 0 {
		sort.Ints(blind)
		limits = append(limits, fmt.Sprintf("the TCP connections of sshd sessions %s cannot be read without admin",
			joinInts(blind)))
	}
	sort.SliceStable(flags, func(i, j int) bool { return flags[i].Since.Before(flags[j].Since) })
	return flags, limits
}

// sshdBare is the top session of every sshd session tree with no TCP connection anywhere in it, and the tops whose
// connections this account cannot see. A listener holding a connection itself may be holding the session's, so its
// trees are not called bare.
func sshdBare(procs []roomstats.HostProc, byPID map[int]roomstats.HostProc, conns func([]int) map[int]int) (map[int]bool, []int) {
	top := map[int]int{}
	var ask []int
	for _, p := range procs {
		if !roomstats.SSHDSession(p, byPID) {
			continue
		}
		r := p
		for i := 0; i < 16 && r.PPID != r.PID && roomstats.SSHDSession(byPID[r.PPID], byPID); i++ {
			r = byPID[r.PPID]
		}
		top[p.PID] = r.PID
		ask = append(ask, p.PID, r.PPID)
	}
	if len(ask) == 0 {
		return nil, nil
	}
	got := conns(ask)
	rank := map[int]int{roomstats.ConnNone: 0, roomstats.ConnUnknown: 1, roomstats.ConnSome: 2}
	state := map[int]int{}
	for pid, r := range top {
		for _, c := range []int{got[pid], got[byPID[r].PPID]} {
			if s, seen := state[r]; !seen || rank[c] > rank[s] {
				state[r] = c
			}
		}
	}
	bare := map[int]bool{}
	var blind []int
	for r, s := range state {
		switch s {
		case roomstats.ConnNone:
			bare[r] = true
		case roomstats.ConnUnknown:
			blind = append(blind, r)
		}
	}
	return bare, blind
}

func hostFlagged(flags []HostFlag, pid int) bool {
	for _, f := range flags {
		if f.PID == pid {
			return true
		}
	}
	return false
}

func joinInts(ns []int) string {
	s := make([]string, len(ns))
	for i, n := range ns {
		s[i] = strconv.Itoa(n)
	}
	return strings.Join(s, ", ")
}

func roundPct(v float64) float64 { return float64(int(v + 0.5)) }

// hostStop is the command that ends pid. Another account's process needs an elevated prompt.
func hostStop(goos string, f HostFlag, mine bool) string {
	if goos == "windows" {
		cmd := "taskkill /PID " + strconv.Itoa(f.PID) + " /F"
		if f.Shape == HostShapeSSHDBare {
			cmd = "taskkill /PID " + strconv.Itoa(f.PID) + " /T /F"
		}
		if !mine {
			cmd += "  (from an administrator prompt)"
		}
		return cmd
	}
	if mine {
		return "kill " + strconv.Itoa(f.PID)
	}
	return "sudo kill " + strconv.Itoa(f.PID)
}

func hostSay(f HostFlag, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s pid %d", f.Name, f.PID)
	if f.Shape == HostShapeSSHDBare {
		fmt.Fprintf(&b, " is an sshd session with no TCP connection, seen so for %s", ageWords(now.Sub(f.Since)))
		fmt.Fprintf(&b, ", at %.0f%% of a core", f.CPUPct)
	} else {
		fmt.Fprintf(&b, " has held %.0f%% of a core or more for %s", f.CPUPct, ageWords(now.Sub(f.Since)))
	}
	fmt.Fprintf(&b, ", %s in memory", humanBytes(int64(f.RSSBytes)))
	if f.IOPerSec > 0 {
		fmt.Fprintf(&b, ", %s/s of IO", humanBytes(f.IOPerSec))
	}
	if !f.Started.IsZero() {
		fmt.Fprintf(&b, ", started %s", f.Started.Local().Format("2006-01-02 15:04"))
	}
	if f.Owner != "" {
		fmt.Fprintf(&b, ", owner %s", f.Owner)
	}
	if f.Whose != "" {
		fmt.Fprintf(&b, ", %s", f.Whose)
	}
	b.WriteString(".")
	for _, l := range f.Limits {
		b.WriteString(" " + l + ".")
	}
	b.WriteString(" stop it: " + f.Stop)
	return b.String()
}

func ageWords(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf("%d days", int(d/(24*time.Hour)))
	case d >= 2*time.Hour:
		return fmt.Sprintf("%d hours", int(d/time.Hour))
	case d >= time.Minute:
		return fmt.Sprintf("%d min", int(d/time.Minute))
	}
	return fmt.Sprintf("%d s", int(d/time.Second))
}

// hostLimits states each field this account could not read, never leaving it out silently.
func hostLimits(f HostFlag) []string {
	var out []string
	if f.Owner == "" {
		out = append(out, "owner: not readable without admin")
	}
	if f.Started.IsZero() {
		out = append(out, "start time: not readable without admin")
	}
	if f.IOPerSec < 0 {
		out = append(out, "disk IO: not readable without admin")
	}
	return out
}

func (d *Daemon) hostWhose(pid int) string {
	if d.hublinkService == nil {
		return ""
	}
	p, ok, err := roomstats.ProcByPID(pid)
	if err != nil || !ok {
		return ""
	}
	hublink.ClassifyProc(&p, d.CardPlaces(), os.Getpid())
	switch p.Kind {
	case roomstats.ProcCardLive:
		return "under live card " + p.Card
	case roomstats.ProcCardClosed:
		return "left by closed card " + p.Card
	case roomstats.ProcAtrium:
		return "atrium's own"
	}
	return "not started by atrium"
}

func (d *Daemon) hostTick(now time.Time) {
	pct, minutes := d.hostLevels()
	st := HostStatus{At: now, HogPct: pct, HogMinutes: minutes, Flags: []HostFlag{}}
	procs, err := roomstats.HostProcs()
	if err != nil {
		st.Err = "cannot read this host's processes: " + err.Error()
	} else {
		st.Procs = len(procs)
		d.host.mu.Lock()
		st.Flags, st.Limits = d.host.hostObserve(procs, roomstats.HostConns, now, float64(pct),
			time.Duration(minutes)*time.Minute)
		var ask []int
		for _, f := range st.Flags {
			if t := d.host.track[f.PID]; t != nil && t.whose == nil {
				ask = append(ask, f.PID)
			}
		}
		d.host.mu.Unlock()
		whose := map[int]string{}
		for _, pid := range ask {
			whose[pid] = d.hostWhose(pid)
		}
		me := currentUser()
		d.host.mu.Lock()
		for i := range st.Flags {
			f := &st.Flags[i]
			if t := d.host.track[f.PID]; t != nil {
				if w, ok := whose[f.PID]; ok {
					t.whose = &w
				}
				if t.whose != nil {
					f.Whose = *t.whose
				}
			}
		}
		d.host.mu.Unlock()
		for i := range st.Flags {
			f := &st.Flags[i]
			if f.Owner == "" {
				f.Owner = roomstats.OwnerOf(f.PID)
			}
			f.Limits = hostLimits(*f)
			f.Stop = hostStop(runtime.GOOS, *f, f.Owner != "" && sameUser(f.Owner, me))
			f.Say = hostSay(*f, now)
		}
	}
	d.hostStore(st)
}

func currentUser() string {
	for _, k := range []string{"USERNAME", "USER", "LOGNAME"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

// sameUser compares the account part only, since Windows answers DOMAIN\name.
func sameUser(owner, me string) bool {
	if i := strings.LastIndexAny(owner, `\/`); i >= 0 {
		owner = owner[i+1:]
	}
	return me != "" && strings.EqualFold(owner, me)
}

func (d *Daemon) hostStore(st HostStatus) {
	d.host.mu.Lock()
	was := d.host.status
	d.host.status = st
	d.host.mu.Unlock()
	old := map[string]bool{}
	for _, f := range was.Flags {
		old[f.Shape+strconv.Itoa(f.PID)] = true
	}
	now := map[string]bool{}
	for _, f := range st.Flags {
		now[f.Shape+strconv.Itoa(f.PID)] = true
		if !old[f.Shape+strconv.Itoa(f.PID)] {
			log.Printf("[atrium] host watch: %s", f.Say)
		}
	}
	for _, f := range was.Flags {
		if !now[f.Shape+strconv.Itoa(f.PID)] {
			log.Printf("[atrium] host watch: %s pid %d is no longer flagged (%s)", f.Name, f.PID, f.Shape)
		}
	}
	if st.Err != "" && st.Err != was.Err {
		log.Printf("[atrium] host watch: %s", st.Err)
	}
}

func (d *Daemon) hostNoticeOf(st HostStatus) string {
	var lines []string
	if st.Err != "" {
		lines = append(lines, d.RoomWord()+": "+st.Err)
	}
	for _, f := range st.Flags {
		lines = append(lines, d.RoomWord()+": "+f.Say)
	}
	return strings.Join(lines, "\n")
}

// HostNotice is the room's runaway lines for the hub, "" while nothing is flagged.
func (d *Daemon) HostNotice() string {
	d.host.mu.Lock()
	st := d.host.status
	d.host.mu.Unlock()
	return d.hostNoticeOf(st)
}

func (d *Daemon) HostState() any {
	d.host.mu.Lock()
	st := d.host.status
	d.host.mu.Unlock()
	if st.Flags == nil {
		st.Flags = []HostFlag{}
	}
	st.Notice = d.hostNoticeOf(st)
	return st
}

func (d *Daemon) hostWatcher(ctx context.Context, start, every time.Duration) {
	wait := start
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = every
		d.hostTick(time.Now())
	}
}
