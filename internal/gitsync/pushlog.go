package gitsync

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

// The push log: one row per ref a push updated, and a marker row when a branch is released. See
// docs/rnd/hub-forge-design.md 3.2. The hub's store (internal/hubstore) keeps it in a table, and
// MemPushLog is the same rules in memory, for the tests of everything above it.

// PushRow is one row of the log.
type PushRow struct {
	// Repo is `<host>/<owner>/<repo>` and Ref is the full ref name, `refs/heads/fix/x`.
	Repo, Ref string
	// Old and New are the shas before and after. Both are empty on a release marker.
	Old, New string
	// Room and Card are the pusher. BOTH ARE EMPTY FOR THE OPERATOR. On a release marker they are the
	// owner that was released.
	Room, Card string
	At         time.Time
	// ReleasedBy is, on a push, what released the branch this push then took over ("operator",
	// "card gone", "card dead", "card done 8 days"), and on a marker the same. Empty otherwise.
	ReleasedBy string
	// Release marks a row that is no push: from here the branch has no owner until the next push.
	Release bool
}

// Operator says whether a row is the operator's.
func (r PushRow) Operator() bool { return !r.Release && r.Room == "" && r.Card == "" }

// BranchRecord is what the log knows of one branch, for the board.
type BranchRecord struct {
	Ref string
	// Room and Card are the owner, both empty for the operator. For a released branch they are the owner
	// it had.
	Room, Card string
	// At is the time of the branch's latest push.
	At time.Time
	// Released is whether the branch has no owner right now.
	Released bool
}

// PushLog is the hub's record of what was pushed. Every method is safe for concurrent use. The hub
// holds the repository's lock around a push, so within one repository the calls are in order.
type PushLog interface {
	// Append records rows, all or none.
	Append(ctx context.Context, rows ...PushRow) error
	// Owner is the first pusher of a ref since it was last released. ok is false for a ref with no
	// owner: never pushed, or released.
	Owner(ctx context.Context, repo, ref string) (room, card string, ok bool, err error)
	// Release ends the ownership of a ref. By says why: "operator", or what the card's room said.
	Release(ctx context.Context, repo, ref, by string) error
	// Branches is each ref of refs/heads the repository has rows for.
	Branches(ctx context.Context, repo string) ([]BranchRecord, error)
	// LastOperatorPush is the time of the operator's latest push to ref, and false when there was none.
	LastOperatorPush(ctx context.Context, repo, ref string) (time.Time, bool, error)
}

// MemPushLog is a PushLog in memory.
type MemPushLog struct {
	mu   sync.Mutex
	rows []PushRow
	// Now is the clock. Nil is time.Now.
	Now func() time.Time
	// FailAppend, when set, is what Append answers. For tests of a log that cannot write.
	FailAppend error
}

func (m *MemPushLog) now() time.Time {
	if m.Now != nil {
		return m.Now().UTC()
	}
	return time.Now().UTC()
}

// Rows is a copy of every row, oldest first.
func (m *MemPushLog) Rows() []PushRow {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]PushRow(nil), m.rows...)
}

func (m *MemPushLog) Append(_ context.Context, rows ...PushRow) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailAppend != nil {
		return m.FailAppend
	}
	for _, r := range rows {
		if r.At.IsZero() {
			r.At = m.now()
		}
		m.rows = append(m.rows, r)
	}
	return nil
}

// ownerLocked is the first push after the latest release marker.
func (m *MemPushLog) ownerLocked(repo, ref string) (PushRow, bool) {
	var first PushRow
	have := false
	for _, r := range m.rows {
		if r.Repo != repo || r.Ref != ref {
			continue
		}
		if r.Release {
			have = false
			continue
		}
		if !have {
			first, have = r, true
		}
	}
	return first, have
}

func (m *MemPushLog) Owner(_ context.Context, repo, ref string) (string, string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.ownerLocked(repo, ref)
	return r.Room, r.Card, ok, nil
}

func (m *MemPushLog) Release(_ context.Context, repo, ref, by string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.ownerLocked(repo, ref)
	if !ok {
		return nil
	}
	m.rows = append(m.rows, PushRow{Repo: repo, Ref: ref, Room: o.Room, Card: o.Card, At: m.now(),
		ReleasedBy: by, Release: true})
	return nil
}

func (m *MemPushLog) Branches(_ context.Context, repo string) ([]BranchRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	type state struct {
		rec      BranchRecord
		hasOwner bool
	}
	byRef := map[string]*state{}
	for _, r := range m.rows {
		if r.Repo != repo || !isHead(r.Ref) {
			continue
		}
		s := byRef[r.Ref]
		if s == nil {
			s = &state{rec: BranchRecord{Ref: r.Ref}}
			byRef[r.Ref] = s
		}
		if r.Release {
			s.rec.Released, s.hasOwner = true, false
			continue
		}
		if !s.hasOwner {
			s.rec.Room, s.rec.Card, s.hasOwner = r.Room, r.Card, true
		}
		s.rec.Released, s.rec.At = false, r.At
	}
	out := make([]BranchRecord, 0, len(byRef))
	for _, s := range byRef {
		out = append(out, s.rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref < out[j].Ref })
	return out, nil
}

func (m *MemPushLog) LastOperatorPush(_ context.Context, repo, ref string) (time.Time, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var at time.Time
	found := false
	for _, r := range m.rows {
		if r.Repo == repo && r.Ref == ref && r.Operator() && (!found || r.At.After(at)) {
			at, found = r.At, true
		}
	}
	return at, found, nil
}

func isHead(ref string) bool {
	return len(ref) > len("refs/heads/") && ref[:len("refs/heads/")] == "refs/heads/"
}

// ── asking a room about a card ──────────────────────────

// CardState is what a card's room says about it.
type CardState struct {
	// Status is the card's own: "done" and "dead" matter here, anything else is a live card.
	Status string
	// IdleSeconds is how long since the card last did anything. For a done card it is the time since it
	// finished, as nearly as the room can say.
	IdleSeconds int
}

// ErrCardGone is the answer when the room is reachable and has no such card: it was culled.
var ErrCardGone = errors.New("the room has no such card")

// ErrRoomUnreachable is the answer when the room cannot be asked right now.
var ErrRoomUnreachable = errors.New("the room cannot be asked right now")

// CardLookup asks a room about one card. It answers CardState, or ErrCardGone, or ErrRoomUnreachable (or
// another error, which is treated as unreachable).
type CardLookup func(ctx context.Context, room, card string) (CardState, error)

// DoneRelease is how long a card has been done before its branches are released.
const DoneRelease = 7 * 24 * time.Hour

// released says whether a card's state frees its branches, and why in a few words.
func released(st CardState, err error) (bool, string) {
	switch {
	case errors.Is(err, ErrCardGone):
		return true, "card gone"
	case err != nil:
		return false, ""
	case st.Status == "dead":
		return true, "card dead"
	case st.Status == "done" && time.Duration(st.IdleSeconds)*time.Second > DoneRelease:
		return true, "card done over 7 days"
	}
	return false, ""
}
