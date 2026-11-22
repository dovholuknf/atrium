package store

import (
	"sync"
	"time"
)

// holdActivityFor is how long a hold stands if no real work releases it first.
const holdActivityFor = 5 * time.Minute

type actHold struct {
	at    time.Time
	until time.Time
}

type actHolds struct {
	mu sync.Mutex
	by map[string]actHold
}

// HoldActivity pins each card's last_activity_at at its current value against the writes a runner restart makes (the
// exit, the relaunch, the session start). A prompt or a tool call releases it, and a resumed runner's own startup events do not.
func (s *Store) HoldActivity(ids []string) error {
	return s.guard(func() error {
		s.holds.mu.Lock()
		defer s.holds.mu.Unlock()
		if s.holds.by == nil {
			s.holds.by = map[string]actHold{}
		}
		for _, id := range ids {
			t, err := s.getBy(`id = ?`, id)
			if err != nil {
				continue
			}
			s.holds.by[id] = actHold{at: t.LastActivityAt, until: s.now().Add(holdActivityFor)}
		}
		return nil
	})
}

// activityAt is the value a write stamps into last_activity_at: now, or the held value while a hold stands.
func (s *Store) activityAt(id string) time.Time {
	s.holds.mu.Lock()
	defer s.holds.mu.Unlock()
	h, ok := s.holds.by[id]
	if ok && s.now().Before(h.until) {
		return h.at
	}
	delete(s.holds.by, id)
	return s.now()
}

// ReleaseActivity ends a card's hold, for real work: a prompt or a tool call.
func (s *Store) ReleaseActivity(id string) {
	s.holds.mu.Lock()
	defer s.holds.mu.Unlock()
	delete(s.holds.by, id)
}
