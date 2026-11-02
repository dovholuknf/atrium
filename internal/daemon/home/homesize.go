package home

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
	HomeSizePoll   = time.Minute
)

type HomeWalk struct {
	Path         string
	Stack        []string
	Bytes, Files int64
}

type HomeSizer struct {
	Mu        sync.Mutex
	Walk      *HomeWalk
	LastPass  time.Time
	DoneSeen  map[string]bool
	RootPath  string
	RootFree  uint64
	RootTotal uint64
}

// rootDisk is the card root and the free and total bytes of the drive it is on, as of the last tick.
func (d *Service) RootDisk() (root string, free, total uint64) {
	d.HomeSize.Mu.Lock()
	defer d.HomeSize.Mu.Unlock()
	return d.HomeSize.RootPath, d.HomeSize.RootFree, d.HomeSize.RootTotal
}

func (d *Service) ReadRootDisk() {
	root, err := d.CardRoot()
	s := &d.HomeSize
	s.Mu.Lock()
	defer s.Mu.Unlock()
	s.RootPath, s.RootFree, s.RootTotal = "", 0, 0
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
		s.RootPath, s.RootFree, s.RootTotal = filepath.ToSlash(root), free, total
	}
}

func (d *Service) SizeLoop(ctx context.Context) {
	_ = d.St.CardHomeBackfillIDs()
	t := time.NewTicker(HomeSizePoll)
	defer t.Stop()
	for {
		d.SizeHomes(time.Now(), HomeSizeBudget)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// sizeHomes is one tick. A full pass starts every HomeSizeEvery, and between passes a home whose card has just gone done
// is walked once, since that is when the number matters.
func (d *Service) SizeHomes(now time.Time, budget time.Duration) {
	d.ReadRootDisk()
	s := &d.HomeSize
	rows, err := d.St.CardHomesOpen()
	if err != nil {
		return
	}
	s.Mu.Lock()
	full := s.Walk != nil || s.LastPass.IsZero() || now.Sub(s.LastPass) >= HomeSizeEvery
	if full && s.Walk == nil {
		s.LastPass = now
	}
	if s.DoneSeen == nil {
		s.DoneSeen = map[string]bool{}
	}
	s.Mu.Unlock()

	var todo []store.CardHome
	for _, r := range rows {
		if full {
			todo = append(todo, r)
			continue
		}
		if t, err := d.St.Get(r.Card); err == nil && t.Status == store.StatusDone {
			s.Mu.Lock()
			seen := s.DoneSeen[r.Card]
			s.DoneSeen[r.Card] = true
			s.Mu.Unlock()
			if !seen {
				todo = append(todo, r)
			}
		}
	}
	sort.SliceStable(todo, func(i, j int) bool { return todo[i].MeasuredAt < todo[j].MeasuredAt })
	s.Mu.Lock()
	if s.Walk != nil {
		for i, r := range todo {
			if filepath.ToSlash(r.Path) == s.Walk.Path {
				todo[0], todo[i] = todo[i], todo[0]
				break
			}
		}
	}
	s.Mu.Unlock()

	deadline := time.Now().Add(budget)
	for _, r := range todo {
		if !time.Now().Before(deadline) {
			return
		}
		if !d.WalkHome(r.Path, deadline) {
			return
		}
	}
}

// walkHome walks one home until it is done or the deadline passes, and says whether it was done.
func (d *Service) WalkHome(path string, deadline time.Time) bool {
	s := &d.HomeSize
	s.Mu.Lock()
	w := s.Walk
	if w == nil || w.Path != path {
		w = &HomeWalk{Path: path, Stack: []string{filepath.FromSlash(path)}}
		s.Walk = w
	}
	s.Mu.Unlock()
	for len(w.Stack) > 0 {
		if !time.Now().Before(deadline) {
			return false
		}
		dir := w.Stack[len(w.Stack)-1]
		w.Stack = w.Stack[:len(w.Stack)-1]
		ents, err := os.ReadDir(wtfree.LongPath(dir))
		if err != nil {
			s.Mu.Lock()
			s.Walk = nil
			s.Mu.Unlock()
			return true
		}
		for _, e := range ents {
			switch {
			case e.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0:
			case e.IsDir():
				w.Stack = append(w.Stack, filepath.Join(dir, e.Name()))
			case e.Type().IsRegular():
				if info, err := e.Info(); err == nil {
					w.Bytes += info.Size()
					w.Files++
				}
			}
		}
	}
	if err := d.St.CardHomeMeasure(w.Path, w.Bytes, w.Files); err != nil {
		log.Printf("[atrium] could not record the size of %s: %v", w.Path, err)
	}
	s.Mu.Lock()
	s.Walk = nil
	s.Mu.Unlock()
	return true
}

// cardHomesStat is the room-stats reading of the card root: where it is, its drive, and the sum of the open homes.
func (d *Service) CardHomesStat() (root string, free, total uint64, homes int, bytes int64) {
	root, free, total = d.RootDisk()
	rows, err := d.St.CardHomesOpen()
	if err != nil {
		return
	}
	for _, r := range rows {
		homes++
		bytes += r.Bytes
	}
	return
}
