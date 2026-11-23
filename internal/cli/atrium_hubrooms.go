package cli

import (
	"errors"
	"fmt"
	"sync"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

// hubRooms answers whether a room may attach to this hub.
//
// A HALTED STORE IS NOT A ROOM THAT WAS NEVER MADE. f-001: the store halted, and the heartbeat asks this of every
// attached room, so the halt came back as "this hub has no room called X" and every room was let go and turned
// away when it tried to come back. The rooms hold the work and the hub holds none, so a hub whose store has halted
// keeps routing for the rooms it already knew. It remembers each name the store confirmed, and answers from that
// memory only while the store cannot answer. A room it never knew is still refused, because nothing that could
// mint or confirm one is working.
type hubRooms struct {
	store *hubstore.Store

	mu    sync.Mutex
	known map[string]bool
}

func newHubRooms(store *hubstore.Store) *hubRooms {
	k := &hubRooms{store: store, known: map[string]bool{}}
	if rooms, err := store.Rooms(); err == nil {
		for _, r := range rooms {
			k.known[r.Name] = true
		}
	}
	return k
}

// attaching is link.Hub.Attaching: asked when a room attaches and again on every heartbeat.
func (k *hubRooms) attaching(name, host, ver string) error {
	r, err := k.store.ByName(name)
	switch {
	case err == nil && r.State == hubstore.StatePending:
		return fmt.Errorf("%q is waiting for this hub's operator to approve it. "+
			"`%s %s` on the hub lets it in", name, atriumCmd("rooms approve"), name)
	case err == nil:
		k.mu.Lock()
		k.known[name] = true
		k.mu.Unlock()
		// Last heard from is bookkeeping. A store that halts on writing it does not unmake the room.
		if err := k.store.Seen(r.ID, host, ver); err != nil && !errors.Is(err, hubstore.ErrHalted) {
			return err
		}
		return nil
	case errors.Is(err, hubstore.ErrHalted):
		k.mu.Lock()
		knew := k.known[name]
		k.mu.Unlock()
		if knew {
			return nil
		}
		return fmt.Errorf("this hub's store has halted, and %q is not a room it knew before it did: %v", name, err)
	default:
		// The store answered, and the answer is no: forced out, or never made.
		k.mu.Lock()
		delete(k.known, name)
		k.mu.Unlock()
		return fmt.Errorf("this hub has no room called %q. "+
			"`%s %s` on the hub makes one, and prints "+
			"the string to paste here", name, atriumCmd("rooms add"), name)
	}
}

// readopt is link.Hub.Readopt: a room with a certificate from this hub's CA and no row gets its row back.
func (k *hubRooms) readopt(name string) error {
	if _, err := k.store.ByName(name); err == nil || errors.Is(err, hubstore.ErrHalted) {
		return nil
	}
	// A room this hub removed or forced out still holds a certificate it signed. Its audit says so, until a wipe.
	past, err := k.store.AuditByName(name, 200)
	if err != nil {
		return err
	}
	for _, e := range past {
		if e.Kind == "removed" || e.Kind == "forced-out" {
			return fmt.Errorf("%q was %s, so it is not readopted", name, e.Kind)
		}
	}
	r, err := k.store.Add(name, hubstore.TransportDirect)
	if err != nil {
		return err
	}
	k.store.Log(r, "readopted", "dialled in with a certificate this hub signed, and the hub had no row for it")
	return k.store.MarkEnrolled(r.ID)
}
