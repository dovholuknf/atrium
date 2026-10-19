package daemon

import (
	"context"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/roomstats"
	"github.com/dovholuknf/atrium/internal/store"
	"github.com/dovholuknf/atrium/internal/wtfree"
)

// HOW BIG EACH HOME IS, observed from the disk. See docs/runtime/card-home-design.md section 7.
//
// One home at a time, links never followed, and a budget per tick: a walk that has not finished when the budget is
// spent is kept as a stack of folders still to read and goes on at the next tick. A walk that meets an error is dropped
// and the row keeps its last number with its age, so a half count is never written.

var (
	HomeSizeEvery  = 10 * time.Minute
	HomeSizeBudget = 2 * time.Second
	homeSizePoll   = time.Minute
)

type homeWalk struct {
	path         string
	stack        []string
	bytes, files int64
}

type homeSizer struct {
	mu        sync.Mutex
	walk      *homeWalk
	lastPass  time.Time
	doneSeen  map[string]bool
	rootPath  string
	rootFree  uint64
	rootTotal uint64
}

// rootDisk is the card root and the free and total bytes of the drive it is on, as of the last tick.
func (d *Daemon) rootDisk() (root string, free, total uint64) {
	d.homeSize.mu.Lock()
	defer d.homeSize.mu.Unlock()
	return d.homeSize.rootPath, d.homeSize.rootFree, d.homeSize.rootTotal
}

func (d *Daemon) readRootDisk() {
	root, err := d.cardRoot()
	s := &d.homeSize
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rootPath, s.rootFree, s.rootTotal = "", 0, 0
	if err != nil {
		return
	}
	probe := root
	for {
		if _, err := os.Stat(probe); err == nil || filepath.Dir(probe) == probe {
			break
		}
		probe = filepath.Dir(probe)
	}
	if _, free, total, err := roomstats.DiskOf(probe); err == nil {
		s.rootPath, s.rootFree, s.rootTotal = filepath.ToSlash(root), free, total
	}
}

func (d *Daemon) sizeLoop(ctx context.Context) {
	_ = d.st.CardHomeBackfillIDs()
	t := time.NewTicker(homeSizePoll)
	defer t.Stop()
	for {
		d.sizeHomes(time.Now(), HomeSizeBudget)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// sizeHomes is one tick. A full pass starts every HomeSizeEvery, and between passes a home whose card has just gone done
// is walked once, since that is when the number matters.
func (d *Daemon) sizeHomes(now time.Time, budget time.Duration) {
	d.readRootDisk()
	s := &d.homeSize
	rows, err := d.st.CardHomesOpen()
	if err != nil {
		return
	}
	s.mu.Lock()
	full := s.walk != nil || s.lastPass.IsZero() || now.Sub(s.lastPass) >= HomeSizeEvery
	if full && s.walk == nil {
		s.lastPass = now
	}
	if s.doneSeen == nil {
		s.doneSeen = map[string]bool{}
	}
	s.mu.Unlock()

	var todo []store.CardHome
	for _, r := range rows {
		if full {
			todo = append(todo, r)
			continue
		}
		if t, err := d.st.Get(r.Card); err == nil && t.Status == store.StatusDone {
			s.mu.Lock()
			seen := s.doneSeen[r.Card]
			s.doneSeen[r.Card] = true
			s.mu.Unlock()
			if !seen {
				todo = append(todo, r)
			}
		}
	}
	sort.SliceStable(todo, func(i, j int) bool { return todo[i].MeasuredAt < todo[j].MeasuredAt })
	s.mu.Lock()
	if s.walk != nil {
		for i, r := range todo {
			if filepath.ToSlash(r.Path) == s.walk.path {
				todo[0], todo[i] = todo[i], todo[0]
				break
			}
		}
	}
	s.mu.Unlock()

	deadline := time.Now().Add(budget)
	for _, r := range todo {
		if !time.Now().Before(deadline) {
			return
		}
		if !d.walkHome(r.Path, deadline) {
			return
		}
	}
}

// walkHome walks one home until it is done or the deadline passes, and says whether it was done.
func (d *Daemon) walkHome(path string, deadline time.Time) bool {
	s := &d.homeSize
	s.mu.Lock()
	w := s.walk
	if w == nil || w.path != path {
		w = &homeWalk{path: path, stack: []string{filepath.FromSlash(path)}}
		s.walk = w
	}
	s.mu.Unlock()
	for len(w.stack) > 0 {
		if !time.Now().Before(deadline) {
			return false
		}
		dir := w.stack[len(w.stack)-1]
		w.stack = w.stack[:len(w.stack)-1]
		ents, err := os.ReadDir(wtfree.LongPath(dir))
		if err != nil {
			s.mu.Lock()
			s.walk = nil
			s.mu.Unlock()
			return true
		}
		for _, e := range ents {
			switch {
			case e.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0:
			case e.IsDir():
				w.stack = append(w.stack, filepath.Join(dir, e.Name()))
			case e.Type().IsRegular():
				if info, err := e.Info(); err == nil {
					w.bytes += info.Size()
					w.files++
				}
			}
		}
	}
	if err := d.st.CardHomeMeasure(w.path, w.bytes, w.files); err != nil {
		log.Printf("[atrium] could not record the size of %s: %v", w.path, err)
	}
	s.mu.Lock()
	s.walk = nil
	s.mu.Unlock()
	return true
}

// cardHomesStat is the room-stats reading of the card root: where it is, its drive, and the sum of the open homes.
func (d *Daemon) cardHomesStat() (root string, free, total uint64, homes int, bytes int64) {
	root, free, total = d.rootDisk()
	rows, err := d.st.CardHomesOpen()
	if err != nil {
		return
	}
	for _, r := range rows {
		homes++
		bytes += r.Bytes
	}
	return
}
