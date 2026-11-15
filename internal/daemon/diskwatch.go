package daemon

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/base/cardroot"
	"github.com/dovholuknf/atrium/internal/daemon/home"
	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"github.com/dovholuknf/atrium/internal/roomstats"
	"github.com/dovholuknf/atrium/internal/store"
)

var (
	DiskWatchEvery   = time.Minute
	DiskWatchStart   = 10 * time.Second
	DiskReclaimEvery = 5 * time.Minute
	DiskTempAge      = 24 * time.Hour
)

const (
	DiskWarnDefault     = 20 << 30
	DiskCriticalDefault = 10 << 30
)

const (
	DiskOK       = "ok"
	DiskWarning  = "warning"
	DiskCritical = "critical"
)

var diskCacheDirs = []string{"go-build", "go-mod", ".npm", "npm-cache", ".cache"}

type DiskUser struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
}

type DiskDrive struct {
	Drive string     `json:"drive"`
	Paths []string   `json:"paths"`
	Free  uint64     `json:"free_bytes"`
	Total uint64     `json:"total_bytes"`
	Level string     `json:"level"`
	Users []DiskUser `json:"users"`
}

type DiskStatus struct {
	Level      string      `json:"level"`
	Say        string      `json:"say,omitempty"`
	Notice     string      `json:"notice,omitempty"`
	FreedBytes int64       `json:"freed_bytes"`
	FreedAt    time.Time   `json:"freed_at,omitempty"`
	Warn       uint64      `json:"warn_bytes"`
	Critical   uint64      `json:"critical_bytes"`
	Drives     []DiskDrive `json:"drives"`
}

type diskWatch struct {
	mu          sync.Mutex
	status      DiskStatus
	lastReclaim time.Time
	freed       int64
	freedAt     time.Time
	// run keeps a timed reclaim and the button's from sweeping at once.
	run sync.Mutex
}

func DiskLevel(free, warn, critical uint64) string {
	switch {
	case free < critical:
		return DiskCritical
	case free < warn:
		return DiskWarning
	}
	return DiskOK
}

// diskLevels holds a critical level above the warning one down to it, so warning always comes first.
func (d *Daemon) diskLevels() (warn, critical uint64) {
	read := func(key string, def uint64) uint64 {
		if v, err := d.st.Setting(key); err == nil {
			if mb, err := strconv.ParseUint(strings.TrimSpace(v), 10, 64); err == nil && mb > 0 {
				return mb << 20
			}
		}
		return def
	}
	warn, critical = read(store.SettingDiskWarnFree, DiskWarnDefault), read(store.SettingDiskCriticalFree, DiskCriticalDefault)
	if critical > warn {
		critical = warn
	}
	return warn, critical
}

func (d *Daemon) diskPaths() []string {
	var out []string
	if root, err := d.CardRoot(); err == nil {
		out = append(out, root)
	}
	out = append(out, cardroot.StateDir(), filepath.Dir(d.opts.DBPath), d.worktreeVolume(), os.TempDir())
	if h, err := os.UserHomeDir(); err == nil {
		out = append(out, h)
	}
	for _, tool := range []string{"git", "go", "node"} {
		if p, err := exec.LookPath(tool); err == nil {
			out = append(out, filepath.Dir(p))
		}
	}
	if hs, err := d.st.Harnesses(); err == nil {
		for _, h := range hs {
			if p := d.hk.runnerFound(h); p != "" {
				out = append(out, filepath.Dir(p))
			}
		}
	}
	return out
}

// existing is the nearest folder of path that exists, since a card root not made yet is still on a drive.
func existing(path string) string {
	for {
		if _, err := os.Stat(path); err == nil || filepath.Dir(path) == path {
			return path
		}
		path = filepath.Dir(path)
	}
}

func (d *Daemon) DiskCheck() DiskStatus {
	warn, critical := d.diskLevels()
	st := DiskStatus{Level: DiskOK, Warn: warn, Critical: critical, Drives: []DiskDrive{}}
	byVol := map[string]*DiskDrive{}
	for _, p := range d.diskPaths() {
		if strings.TrimSpace(p) == "" {
			continue
		}
		probe := existing(p)
		_, free, total, err := roomstats.DiskOf(probe)
		vol := roomstats.VolumeOf(probe)
		if err != nil || vol == "" {
			continue
		}
		dr := byVol[vol]
		if dr == nil {
			dr = &DiskDrive{Drive: diskName(probe), Free: free, Total: total, Level: DiskLevel(free, warn, critical)}
			byVol[vol] = dr
		}
		dr.Paths = append(dr.Paths, filepath.ToSlash(p))
	}
	for vol, dr := range byVol {
		if dr.Level != DiskOK {
			dr.Users = d.diskUsers(vol)
		}
		st.Drives = append(st.Drives, *dr)
	}
	sort.Slice(st.Drives, func(i, j int) bool { return st.Drives[i].Free < st.Drives[j].Free })
	for _, dr := range st.Drives {
		if dr.Level == DiskCritical || (dr.Level == DiskWarning && st.Level == DiskOK) {
			st.Level = dr.Level
		}
	}
	d.disk.mu.Lock()
	st.FreedBytes, st.FreedAt = d.disk.freed, d.disk.freedAt
	d.disk.mu.Unlock()
	st.Say = diskSay(st)
	return st
}

func diskName(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if v := filepath.VolumeName(abs); v != "" {
		return strings.ToUpper(v)
	}
	return filepath.ToSlash(abs)
}

func diskSay(st DiskStatus) string {
	if st.Level == DiskOK {
		return ""
	}
	for _, dr := range st.Drives {
		if dr.Level != st.Level {
			continue
		}
		say := fmt.Sprintf("%s has %s free, under the %s level of %s.", dr.Drive, humanBytes(int64(dr.Free)), st.Level,
			humanBytes(int64(map[string]uint64{DiskWarning: st.Warn, DiskCritical: st.Critical}[st.Level])))
		if len(dr.Users) > 0 {
			var parts []string
			for _, u := range dr.Users {
				parts = append(parts, fmt.Sprintf("%s (%s)", u.Path, humanBytes(u.Bytes)))
			}
			say += " atrium's biggest users there: " + strings.Join(parts, ", ") + "."
		}
		if st.Level == DiskCritical {
			say += " New launches on this room are refused until space is freed."
		}
		if st.FreedBytes > 0 {
			say += fmt.Sprintf(" The room already freed %s at %s.", humanBytes(st.FreedBytes), st.FreedAt.Format("15:04"))
		}
		return say
	}
	return ""
}

func (d *Daemon) diskUsers(vol string) []DiskUser {
	var out []DiskUser
	if rows, err := d.st.CardHomesOpen(); err == nil {
		for _, r := range rows {
			if r.Bytes > 0 && roomstats.VolumeOf(existing(filepath.FromSlash(r.Path))) == vol {
				out = append(out, DiskUser{Path: r.Path, Bytes: r.Bytes})
			}
		}
	}
	for _, e := range d.atriumTemp() {
		if roomstats.VolumeOf(e) == vol {
			out = append(out, DiskUser{Path: filepath.ToSlash(e), Bytes: home.HomeBytes(e)})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Bytes > out[j].Bytes })
	if len(out) > 3 {
		out = out[:3]
	}
	return out
}

func (d *Daemon) atriumTemp() []string {
	ents, err := os.ReadDir(os.TempDir())
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), "atrium-") {
			out = append(out, filepath.Join(os.TempDir(), e.Name()))
		}
	}
	return out
}

func (d *Daemon) DiskNotice() string {
	d.disk.mu.Lock()
	st := d.disk.status
	d.disk.mu.Unlock()
	return d.noticeOf(st)
}

func (d *Daemon) noticeOf(st DiskStatus) string {
	if st.Level == DiskOK || st.Say == "" {
		return ""
	}
	lead := "disk getting low. "
	if st.Level == DiskCritical {
		lead = "disk nearly full. "
	}
	return d.RoomWord() + ": " + lead + st.Say
}

func (d *Daemon) DiskState() any {
	d.disk.mu.Lock()
	st := d.disk.status
	d.disk.mu.Unlock()
	if st.Drives == nil {
		return DiskStatus{Level: DiskOK, Drives: []DiskDrive{}}
	}
	st.Notice = d.noticeOf(st)
	return st
}

// launchDiskFull lets a resume through, since it adds nothing to disk.
func (d *Daemon) launchDiskFull(req shared.LaunchRequest) error {
	if req.TaskID != "" || req.Resume != "" {
		return nil
	}
	d.disk.mu.Lock()
	st := d.disk.status
	d.disk.mu.Unlock()
	if st.Level != DiskCritical {
		return nil
	}
	return fmt.Errorf("%s: disk is nearly full. %s", d.RoomWord(), st.Say)
}

func (d *Daemon) diskWatcher(ctx context.Context, start, every time.Duration) {
	if d.reattached != nil {
		select {
		case <-ctx.Done():
			return
		case <-d.reattached:
		}
	}
	wait := start
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = every
		d.diskTick(ctx, time.Now())
	}
}

func (d *Daemon) diskTick(ctx context.Context, now time.Time) {
	st := d.DiskCheck()
	d.disk.mu.Lock()
	due := now.Sub(d.disk.lastReclaim) >= DiskReclaimEvery
	if st.Level != DiskOK && due {
		d.disk.lastReclaim = now
	}
	d.disk.mu.Unlock()
	if st.Level != DiskOK && due {
		d.DiskReclaim(ctx, now)
		return
	}
	d.diskStore(st)
}

func (d *Daemon) diskStore(st DiskStatus) {
	d.disk.mu.Lock()
	changed := d.disk.status.Level != st.Level
	d.disk.status = st
	d.disk.mu.Unlock()
	if changed && st.Say != "" {
		log.Printf("[atrium] disk watch: %s. %s", st.Level, st.Say)
	} else if changed {
		log.Printf("[atrium] disk watch: every drive is above the warning level")
	}
}

func (d *Daemon) DiskReclaim(ctx context.Context, now time.Time) DiskStatus {
	d.disk.run.Lock()
	defer d.disk.run.Unlock()
	var freed int64
	for _, s := range d.sweepEndedFolders(ctx) {
		freed += s.Bytes
	}
	freed += d.reclaimCaches(now) + d.reclaimTemp(now)
	if freed > 0 {
		d.disk.mu.Lock()
		d.disk.freed, d.disk.freedAt = freed, now
		d.disk.mu.Unlock()
	}
	st := d.DiskCheck()
	d.diskStore(st)
	return st
}

func (d *Daemon) DiskFree() any {
	st := d.DiskReclaim(context.Background(), time.Now())
	st.Notice = d.noticeOf(st)
	return map[string]any{"freed_bytes": st.FreedBytes, "status": st}
}

func (d *Daemon) reclaimCaches(now time.Time) (freed int64) {
	rows, err := d.st.CardHomesOpen()
	if err != nil {
		return 0
	}
	for _, row := range rows {
		var t *store.Task
		if row.Card != "" {
			if got, err := d.st.Get(row.Card); err == nil {
				t = got
			}
		}
		if t == nil || !d.cardEnded(t, nil, now) {
			continue
		}
		tmp := home.DirsOf(row.Path).Tmp
		for _, name := range diskCacheDirs {
			dir := filepath.Join(tmp, name)
			if fi, err := os.Lstat(dir); err != nil || !fi.IsDir() {
				continue
			}
			n := home.HomeBytes(dir)
			if err := home.RemoveTree(dir); err == nil {
				freed += n
				log.Printf("[atrium] disk watch removed the cache %s (%s)", filepath.ToSlash(dir), humanBytes(n))
			}
		}
	}
	return freed
}

func (d *Daemon) reclaimTemp(now time.Time) (freed int64) {
	for _, p := range d.atriumTemp() {
		if newest, ok := newestMod(p); !ok || now.Sub(newest) < DiskTempAge {
			continue
		}
		n := home.HomeBytes(p)
		if err := home.RemoveTree(p); err == nil {
			freed += n
			log.Printf("[atrium] disk watch removed %s (%s)", filepath.ToSlash(p), humanBytes(n))
		}
	}
	return freed
}

// newestMod is the latest change anywhere under path, since a folder's own time misses a file rewritten inside it.
func newestMod(path string) (newest time.Time, ok bool) {
	err := filepath.WalkDir(path, func(_ string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		fi, err := e.Info()
		if err != nil {
			return err
		}
		if fi.ModTime().After(newest) {
			newest = fi.ModTime()
		}
		return nil
	})
	return newest, err == nil
}
