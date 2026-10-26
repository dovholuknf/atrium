package store

import (
	"sync"
	"time"
)

// memoMaxAge bounds how long a read memo can answer, so a scope that overlaps another and never closes cannot go stale.
const memoMaxAge = 200 * time.Millisecond

// readMemo holds the settings and runners one list read over and over, once per card, while a Memoized scope is open.
type readMemo struct {
	mu       sync.Mutex
	depth    int
	since    time.Time
	settings map[string]string
	harness  map[string]*Harness
}

// Memoized runs fn with the settings and runners it reads answered from memory after the first read of each. A
// board list asks for the same few rows of every card, and that was a query each. A write through SetSetting,
// SaveHarness or DeleteHarness drops what was held.
func (s *Store) Memoized(fn func()) {
	m := &s.memo
	m.mu.Lock()
	if m.depth == 0 {
		m.since = time.Now()
		m.settings, m.harness = map[string]string{}, map[string]*Harness{}
	}
	m.depth++
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		if m.depth--; m.depth == 0 {
			m.settings, m.harness = nil, nil
		}
		m.mu.Unlock()
	}()
	fn()
}

func (m *readMemo) live() bool { return m.depth > 0 && time.Since(m.since) < memoMaxAge }

func (m *readMemo) drop() {
	m.mu.Lock()
	if m.depth > 0 {
		m.settings, m.harness = map[string]string{}, map[string]*Harness{}
	}
	m.mu.Unlock()
}

func (m *readMemo) setting(key string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.live() {
		return "", false
	}
	v, ok := m.settings[key]
	return v, ok
}

func (m *readMemo) keepSetting(key, v string) {
	m.mu.Lock()
	if m.live() {
		m.settings[key] = v
	}
	m.mu.Unlock()
}

func (m *readMemo) runner(id string) (*Harness, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.live() {
		return nil, false
	}
	h, ok := m.harness[id]
	if !ok {
		return nil, false
	}
	c := *h
	return &c, true
}

func (m *readMemo) keepRunner(id string, h *Harness) {
	m.mu.Lock()
	if m.live() {
		c := *h
		m.harness[id] = &c
	}
	m.mu.Unlock()
}
