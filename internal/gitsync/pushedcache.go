package gitsync

import (
	"context"
	"sync"
	"time"
)

// GET /_hub/git/pushed IS READ FROM A SNAPSHOT, as the repos list is (see viewcache.go). Pushed costs a handful of
// git processes, so a request that ran them waited on every one. An answer is kept per (repository, branch, head)
// that somebody asked about. The first ask starts its build and answers `building` with nothing in it, and every
// later ask answers from memory. The answers are rebuilt on a push, an init and the view timer, for as long as
// somebody keeps asking.

// pushedKeep is how long an answer nobody asks about is still rebuilt, and then forgotten.
const pushedKeep = 10 * time.Minute

// pushedMax bounds the answers kept, so a head that moves on every commit cannot grow the map without end.
const pushedMax = 512

// PushedView is a Pushed answer and how old it is. Computed is zero until the first build has finished, and the
// Pushed is then empty (no state).
type PushedView struct {
	Pushed
	Computed time.Time
	Building bool
}

type pushedKey struct{ name, branch, head string }

type pushedEntry struct {
	val      Pushed
	err      error
	at       time.Time
	asked    time.Time
	building bool
	again    bool
}

type pushedCache struct {
	mu sync.Mutex
	m  map[pushedKey]*pushedEntry
}

// PushedSnapshot answers where the hub's branch stands against head from memory: it runs no git. A bad name or
// head is refused at once, as Pushed does. An error is returned only when the build has failed and never
// succeeded, and the next ask tries again.
func (s *Store) PushedSnapshot(name, branch, head string) (PushedView, error) {
	if !ValidSHA(head) {
		return PushedView{}, refuse("head is a full commit id")
	}
	if why := CheckPushBranch(branch); why != "" {
		return PushedView{}, refuse("%s", why)
	}
	s.ensureViewLoop()
	k := pushedKey{canonical(name), branch, head}
	c := &s.pushed
	c.mu.Lock()
	if c.m == nil {
		c.m = map[pushedKey]*pushedEntry{}
	}
	e := c.m[k]
	if e == nil {
		s.evictPushedLocked()
		e = &pushedEntry{}
		c.m[k] = e
	}
	e.asked = time.Now()
	start := e.at.IsZero() && !e.building
	if start {
		e.building = true
	}
	out := PushedView{Pushed: e.val, Computed: e.at, Building: e.building || e.at.IsZero()}
	err := e.err
	if !e.at.IsZero() {
		err = nil
	}
	c.mu.Unlock()
	if start {
		go s.buildPushed(k, e)
	}
	if err != nil && !out.Building {
		return PushedView{}, err
	}
	return out, nil
}

// evictPushedLocked drops the answers nobody has asked about lately, and the oldest asked when the map is full.
func (s *Store) evictPushedLocked() {
	c := &s.pushed
	now := time.Now()
	for k, e := range c.m {
		if !e.building && now.Sub(e.asked) > pushedKeep {
			delete(c.m, k)
		}
	}
	for len(c.m) >= pushedMax {
		var oldest pushedKey
		var at time.Time
		for k, e := range c.m {
			if at.IsZero() || e.asked.Before(at) {
				oldest, at = k, e.asked
			}
		}
		delete(c.m, oldest)
	}
}

// refreshPushed rebuilds every answer somebody has asked about lately. It returns at once.
func (s *Store) refreshPushed() {
	c := &s.pushed
	c.mu.Lock()
	var todo []pushedKey
	var es []*pushedEntry
	now := time.Now()
	for k, e := range c.m {
		if now.Sub(e.asked) > pushedKeep {
			if !e.building {
				delete(c.m, k)
			}
			continue
		}
		if e.building {
			e.again = true
			continue
		}
		e.building = true
		todo, es = append(todo, k), append(es, e)
	}
	c.mu.Unlock()
	for i := range todo {
		go s.buildPushed(todo[i], es[i])
	}
}

func (s *Store) buildPushed(k pushedKey, e *pushedEntry) {
	c := &s.pushed
	for {
		fn := s.pushedFn
		if fn == nil {
			fn = s.Pushed
		}
		v, err := fn(context.Background(), k.name, k.branch, k.head)
		c.mu.Lock()
		if err == nil {
			e.val, e.at, e.err = v, time.Now(), nil
		} else {
			e.err = err
		}
		if e.again {
			e.again = false
			c.mu.Unlock()
			continue
		}
		e.building = false
		c.mu.Unlock()
		return
	}
}
