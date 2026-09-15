package daemon

import (
	"sync"
	"sync/atomic"
	"time"
)

// Coming back up, and saying so, so the board does not narrate it.
//
// A restart is not an event. The cards that were running die with the old
// daemon, the reaper marks them so, the fixtures come back a moment later and
// `reopenSaved` brings up the rest. To anything diffing the board against what
// it saw a second ago, every one of those is a card that was not there before,
// which is exactly what the board's arrival alert is for. So a restart of six
// terminals rang six times to tell the operator that the thing he had just
// restarted had restarted.
//
// The board cannot work this out for itself. It sees cards appear and has no
// way to tell "somebody launched this" from "this is the same session coming
// back", because from the outside those are the same fact arriving the same
// way. Only the daemon knows it is mid-boot.
//
// So the daemon says so, on `/v1/health`, which every page already polls and
// already reads for the build id. While it is settling the board RE-SEEDS what
// it knows instead of diffing against it, which is the same thing it does on a
// fresh page load and for the same reason.
//
// NOT A SUPPRESSION OF THE ALERTS THEMSELVES. A permission request during the
// window still rings, because an agent that comes back up already blocked is
// the one thing in a restart worth being interrupted for.

// settleFloor is how long a daemon is settling no matter what it finds.
//
// A daemon with no fixtures at all still has sessions that will rejoin: a
// claude session the operator never stopped finds the new daemon through its
// next hook and re-registers, and that takes as long as it takes somebody to
// finish a turn. This is the floor under that, not an estimate of it.
const settleFloor = 20 * time.Second

// settleCap is the backstop, and only the backstop.
// A DURATION IS THE WRONG ANSWER AND THIS IS THE SECOND VERSION.
//
// The first one was a deadline: a floor, a tail pushed out each time something
// came up, and a cap. It reported `settling: true` correctly and the toasts
// arrived anyway, because ten sessions coming back is not an interval anybody
// can name in advance. Starting a claude with `--resume` is slow, the gap
// between them is deliberate, and on a cold machine the whole parade runs
// minutes. Whatever number is picked here is either too short on the day it
// matters or long enough to swallow real news.
//
// So the window is bounded by THE SET rather than by the clock. Atrium knows
// exactly which cards it is about to bring back, because it read the list to
// bring them back, and it is settling until each one of them has arrived. The
// clock is left in as a backstop and nothing else.
const settleCap = 5 * time.Minute

// settling is open while atrium is putting back cards it already had.
type settling struct {
	// floor is the moment the opening grace ends. Covers a daemon with
	// nothing to restore, whose sessions rejoin through their own hooks.
	floor atomic.Int64
	// deadline is the backstop. A card that never comes back must not buy
	// silence forever.
	deadline atomic.Int64

	mu sync.Mutex
	// pending is what atrium said it would bring back and has not yet.
	pending map[string]bool
}

// settleBoot is the entry that stands for "the startup sequence itself".
//
// The stages name their own lists one at a time, so between one finishing and
// the next naming its own there is an instant where nothing is pending. This
// is held for the whole sequence so that instant cannot be read as the restart
// being over.
const settleBoot = "\x00boot"

// begin opens the window. Called once, before anything is started.
func (s *settling) begin() {
	now := time.Now()
	s.deadline.Store(now.Add(settleCap).UnixMilli())
	s.floor.Store(now.Add(settleFloor).UnixMilli())
	s.expect([]string{settleBoot})
}

// expect names the cards atrium is about to bring back.
//
// Called with the WHOLE list before the first one is started, not one at a
// time as each is reached. A list added to as it is walked is empty between
// the first card arriving and the second being named, and the window closes in
// that gap.
func (s *settling) expect(ids []string) {
	if len(ids) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pending == nil {
		s.pending = map[string]bool{}
	}
	for _, id := range ids {
		if id != "" {
			s.pending[id] = true
		}
	}
}

// arrived says one of them is back, or is never coming.
//
// Both, through one door and on purpose. A card that failed to start is as
// finished as one that succeeded, as far as this is concerned, and leaving it
// pending would hold the window open to the backstop every time a worktree had
// been deleted.
func (s *settling) arrived(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pending, id)
}

// waiting is how many are still to come.
func (s *settling) waiting() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.pending)
}

// done ends the window early. Nothing calls this on the happy path. It exists
// so a passive daemon, which starts nothing, is not quiet about cards it had no
// hand in.
func (s *settling) done() {
	s.floor.Store(0)
	s.deadline.Store(0)
	s.mu.Lock()
	s.pending = nil
	s.mu.Unlock()
}

// on reports whether the daemon is still coming up.
//
// Two ways to be true and they answer different halves. Inside the floor
// because a daemon that has only just started is coming up whatever its list
// says, including when the list is empty and the sessions will rejoin on their
// own. Still waiting on the list because that is the actual question, and the
// backstop is the only thing that overrides it.
func (s *settling) on() bool {
	now := time.Now().UnixMilli()
	if f := s.floor.Load(); f != 0 && now < f {
		return true
	}
	if d := s.deadline.Load(); d == 0 || now >= d {
		return false
	}
	return s.waiting() > 0
}

// Settling reports whether this daemon is still bringing back what it had.
//
// Read by `/v1/health`, which is the one thing every page already asks on a
// timer, so this needs no endpoint and no event of its own.
func (d *Daemon) Settling() bool { return d.settle.on() }
