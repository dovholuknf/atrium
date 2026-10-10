package gitsync

import (
	"context"
	"sync"
	"time"
)

// THE BOARD'S LIST OF REPOSITORIES is read from a snapshot, never computed on the request. Listing a
// repository's branches costs a git subprocess, and a process spawn is 50-100ms on Windows, so a GET that
// ran one per repository took seconds. The snapshot is rebuilt in the background: after a push, after an
// init, and on a timer for what changes without either (a forge refresh, a branch made on the hub's disk).

// ViewEvery is how often the snapshot is rebuilt when nothing asked for it sooner.
const ViewEvery = 20 * time.Second

// Snapshot is the list as last computed. Computed is empty until the first build has finished, and then
// Repos holds only what the disk's directories say (no branches), so the page still has something to draw.
type Snapshot struct {
	Repos    []RepoView
	Computed time.Time
	Building bool
}

type viewCache struct {
	mu       sync.Mutex
	snap     []RepoView
	at       time.Time
	building bool
	again    bool
	started  bool
}

// Snapshot answers from memory and a directory listing: it runs no git. The first call starts the
// background loop.
func (s *Store) Snapshot() Snapshot {
	s.ensureViewLoop()
	c := &s.views
	c.mu.Lock()
	snap, at, building := c.snap, c.at, c.building
	c.mu.Unlock()
	if at.IsZero() && !building {
		s.RefreshView()
		building = true
	}
	if at.IsZero() {
		ents, _ := s.List()
		snap = make([]RepoView, 0, len(ents))
		for _, e := range ents {
			snap = append(snap, s.bareEntry(e))
		}
		return Snapshot{Repos: snap, Building: true}
	}
	return Snapshot{Repos: snap, Computed: at, Building: building}
}

// ensureViewLoop starts the timer that rebuilds the snapshots, once.
func (s *Store) ensureViewLoop() {
	c := &s.views
	c.mu.Lock()
	start := !c.started
	c.started = true
	c.mu.Unlock()
	if start {
		go s.viewLoop()
	}
}

// RefreshView asks for the snapshots to be rebuilt: the repos list and the pushed answers. It returns at once.
func (s *Store) RefreshView() {
	s.refreshPushed()
	c := &s.views
	c.mu.Lock()
	if c.building {
		c.again = true
		c.mu.Unlock()
		return
	}
	c.building = true
	c.mu.Unlock()
	go s.buildViews()
}

func (s *Store) buildViews() {
	for {
		v, err := s.View(context.Background())
		c := &s.views
		c.mu.Lock()
		if err == nil {
			c.snap, c.at = v, time.Now()
		}
		if c.again {
			c.again = false
			c.mu.Unlock()
			continue
		}
		c.building = false
		c.mu.Unlock()
		return
	}
}

func (s *Store) viewLoop() {
	for {
		time.Sleep(ViewEvery)
		s.RefreshView()
	}
}
