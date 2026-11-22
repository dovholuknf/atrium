package daemon

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/base/safepath"
	"github.com/dovholuknf/atrium/internal/store"
)

var (
	IncomingSweepStart = 30 * time.Second
	IncomingSweepEvery = time.Hour
)

// DefaultIncomingKeepDays is what an unset incoming_keep_days means.
const DefaultIncomingKeepDays = 7

const incomingRel = ".atrium/incoming"

// atriumWrote is the stamp upload and file drop put in front of every name, so a file a person put there is left alone.
var atriumWrote = regexp.MustCompile(`^\d{8}-\d{6}-`)

// incomingKeep reads the retention age. A value of off, or one that is not a whole number of days, means no sweep.
func (d *Daemon) incomingKeep() (time.Duration, bool) {
	v, err := d.st.Setting(store.SettingIncomingKeepDays)
	if err != nil {
		return 0, false
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return DefaultIncomingKeepDays * 24 * time.Hour, true
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0, false
	}
	return time.Duration(n) * 24 * time.Hour, true
}

// incomingRoots is the directory of every card, live or archived, once each.
func (d *Daemon) incomingRoots() ([]string, error) {
	live, err := d.st.List()
	if err != nil {
		return nil, err
	}
	old, err := d.st.ListArchived(1 << 20)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for _, t := range append(live, old...) {
		if t.Worktree == "" {
			continue
		}
		root := filepath.FromSlash(t.Worktree)
		if k := strings.ToLower(filepath.Clean(root)); !seen[k] {
			seen[k] = true
			out = append(out, root)
		}
	}
	return out, nil
}

// sweepIncoming deletes what atrium wrote under each root's incoming folder once it is older than keep.
func sweepIncoming(roots []string, keep time.Duration, now time.Time) (int, int64) {
	var n int
	var bytes int64
	for _, root := range roots {
		dir, err := safepath.Contained(root, filepath.Join(root, filepath.FromSlash(incomingRel)))
		if err != nil {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.Type().IsRegular() || !atriumWrote.MatchString(e.Name()) {
				continue
			}
			fi, err := e.Info()
			if err != nil || now.Sub(fi.ModTime()) < keep {
				continue
			}
			if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
				log.Printf("[atrium] incoming sweep: %v", err)
				continue
			}
			n++
			bytes += fi.Size()
		}
	}
	return n, bytes
}

func (d *Daemon) sweepIncomingOnce() {
	keep, ok := d.incomingKeep()
	if !ok {
		return
	}
	roots, err := d.incomingRoots()
	if err != nil {
		log.Printf("[atrium] incoming sweep: %v", err)
		return
	}
	n, bytes := sweepIncoming(roots, keep, time.Now())
	log.Printf("[atrium] incoming sweep: removed %d file(s), freed %d bytes", n, bytes)
}

func (d *Daemon) incomingSweeper(ctx context.Context, start, every time.Duration) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(start):
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		d.sweepIncomingOnce()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
