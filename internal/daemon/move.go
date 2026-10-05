package daemon

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// sayMoved is the answer sayGate gives a card whose `moved_to` is set. It is
// checked before everything else: the old card is done, but the words are for
// whoever it became.
const sayMoved = "moved"

// movedEnd is where a chain of `moved_to` ends.
type movedEnd struct {
	Live *store.Task // the card on this room that is the end of the chain, or nil
	Room string      // the room the chain ends on, when it is not this one
	ID   string      // the bare card id there
}

// Address is the end written as a handle a say can be sent to.
func (e movedEnd) Address() string {
	if e.Live != nil {
		return e.Live.ID
	}
	return e.Room + "~" + e.ID
}

// followMoved is THE ONE LOOKUP that follows `moved_to`. Every path that holds a
// card and might be holding an old one asks it: says, reports, the launcher
// fallback. It follows at most store.MaxMoveHops, keeps the ids it has seen, and
// answers `move chain loops at <id>` rather than spin. A to B and back to A is
// an ordinary chain that ends at the live card. A hop to this room continues
// locally, a hop to any other room is the answer.
func (d *Daemon) followMoved(t *store.Task) (movedEnd, error) {
	seen := map[string]bool{}
	for hops := 0; t != nil; hops++ {
		if t.MovedTo == "" {
			return movedEnd{Live: t}, nil
		}
		key := d.roomName() + "~" + t.ID
		if seen[key] || hops >= store.MaxMoveHops {
			return movedEnd{}, fmt.Errorf("move chain loops at %s", t.ID)
		}
		seen[key] = true
		room, id, ok := splitMovedTo(t.MovedTo)
		if !ok {
			return movedEnd{}, fmt.Errorf("move chain is broken at %s: %q", t.ID, t.MovedTo)
		}
		if d.otherRoom(room) != "" {
			return movedEnd{Room: room, ID: id}, nil
		}
		next, err := d.st.Get(id)
		if err != nil {
			return movedEnd{}, fmt.Errorf("move chain ends at %s, which is not here", id)
		}
		t = next
	}
	return movedEnd{}, fmt.Errorf("move chain is empty")
}

func splitMovedTo(s string) (room, id string, ok bool) {
	i := strings.Index(s, "~")
	if i <= 0 || i == len(s)-1 {
		return "", "", false
	}
	return s[:i], s[i+1:], true
}

// forwardMoved sends a say addressed to a moved card on to where it went and
// writes the sender's answer. `forwarded` tells the sender the new address, so a
// script that holds the old one can learn it.
func (d *Daemon) forwardMoved(ctx context.Context, w http.ResponseWriter, from string, end movedEnd, text, when string, reply, wake bool) {
	code, body := d.sayAcross(ctx, from, end.ID, end.Room, text, when, reply, wake)
	if code < 400 {
		body["forwarded"] = "moved to " + end.Room + "~" + end.ID
		body["note"] = "forwarded: moved to " + end.Room + "~" + end.ID
		body["typed"] = body["delivered"] == "terminal"
		body["queued"] = body["delivered"] != "terminal"
	}
	writeJSONCode(w, code, body)
}

// launcherAfterMove is launcherOf for a launcher that has moved. A launcher that
// ended on a live card here is returned. One that moved to another room re-points
// the worker at it, so the notice goes the remote way, and nil is returned.
func (d *Daemon) launcherAfterMove(worker, l *store.Task) *store.Task {
	if l == nil || l.MovedTo == "" {
		return l
	}
	end, err := d.followMoved(l)
	if err != nil {
		log.Printf("[atrium] %s's launcher %s: %v", worker.DisplayTitle(), l.ID, err)
		return nil
	}
	if end.Live != nil {
		return end.Live
	}
	name := l.Alias
	if name == "" {
		name = l.WireName
	}
	if i := strings.Index(name, "@"); i > 0 {
		name = name[:i]
	}
	if err := d.st.SetLauncher(worker.ID, name+"@"+end.Room, end.Room+"~"+end.ID); err != nil {
		log.Printf("[atrium] could not follow %s's launcher to %s~%s: %v", worker.DisplayTitle(), end.Room, end.ID, err)
		return nil
	}
	worker.SpawnedBy, worker.SpawnedByID = name+"@"+end.Room, end.Room+"~"+end.ID
	return nil
}

// sweepFreezes is the self-undo: a card whose freeze lease ran out with no cut-over is resumed, unfrozen and its
// queue replayed. The store refuses it once `moved_to` is set, so it cannot race the hub's cut-over.
func (d *Daemon) sweepFreezes(at time.Time) {
	ids, err := d.st.ExpiredFreezes(at)
	if err != nil || len(ids) == 0 {
		return
	}
	for _, id := range ids {
		moved, err := d.st.ExpireFreeze(id, at)
		if err != nil || !moved {
			continue
		}
		if t, err := d.st.Get(id); err == nil && isParked(t) {
			if err := d.unpark(id, "move"); err != nil {
				log.Printf("[atrium] %s undid its own move but could not resume: %v", id, err)
			}
		}
		d.releaseHeld(id)
		d.publishTask(id)
		log.Printf("[atrium] %s: the move's lease ran out, the card undid it", id)
	}
}
