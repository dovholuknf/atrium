package main

import (
	"time"

	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link"
)

// Every room this hub knows about, whether or not it is answering.
//
// ── the one place the two halves meet ───────────────────
//
// The record is durable and is the hub's own truth: which rooms exist, what
// they are called, how they connect, which are on their way out. The connection
// list is a set of sockets in this process and is true only right now.
//
// They are joined HERE and nowhere else, so nothing downstream has to remember
// which half it is looking at. `internal/link` holds the sockets and knows
// nothing about a database. `internal/hubstore` holds the record and knows
// nothing about a socket. This file is the seam, and it is the whole reason
// both of those stayed ignorant of each other.
//
// ── and the direction of the join ───────────────────────
//
// THE RECORD IS THE LIST. Every room that exists appears, in name order, and a
// room's liveness is a field on it. Not the other way round: starting from what
// is attached and looking up records would silently drop a room nobody has
// dialled in yet, which is the one the durable list exists for.
//
// A room that is attached with no record cannot happen, because the hub refuses
// it at attach and lets it go on the next beat if its record goes. If it did
// happen it would be a bug worth seeing rather than a row to draw, so nothing
// here invents one.
type inventory struct {
	store *hubstore.Store
	hub   *link.Hub
}

func (i inventory) Known() ([]link.Known, error) {
	rooms, err := i.store.Rooms()
	if err != nil {
		return nil, err
	}
	// Indexed by the same folded name the hub routes on, so a room typed with
	// different capitals in two places is still one room.
	live := map[string]link.Attached{}
	for _, a := range i.hub.Rooms() {
		live[fold(a.Name)] = a
	}

	out := make([]link.Known, 0, len(rooms))
	for _, r := range rooms {
		k := link.Known{
			Name: r.Name, SelfName: r.SelfName, Transport: r.Transport,
			State: r.State, FirstSeen: r.FirstSeen, LastSeen: r.LastSeen,
			Version: r.Version, ClearedAt: r.ClearedAt,
		}
		if a, ok := live[fold(r.Name)]; ok {
			k.Attached = true
			since := a.Since
			k.Since = &since
			// PREFERRED OVER WHAT IS WRITTEN DOWN, because a live connection is
			// telling us right now and the record is what it said last time.
			// This is the only direction that rule runs in: the observed fields
			// take the fresher answer, and the hub's own name never does.
			if a.Host != "" {
				k.SelfName, k.Host = a.Host, a.Host
			}
			if a.Version != "" {
				k.Version = a.Version
			}
		}
		// A CACHED COUNT IS ONLY WORTH SHOWING FOR A ROOM THAT IS NOT HERE.
		// An attached room answers for itself, every time, and a remembered
		// number beside a live one is the third state decision 15 exists to
		// rule out.
		if !k.Attached {
			if n, err := i.store.CardCount(r.ID); err == nil {
				k.Cards = n
			}
			if ok, _, err := i.store.Outstanding(r.ID); err == nil {
				k.Waiting = ok
			}
		}
		out = append(out, k)
	}
	return out, nil
}

// MarkRoom puts a room on its way out, or takes the mark back off.
//
// THE ONLY CHANGE THE BOARD CAN MAKE TO A ROOM, and that is deliberate.
// Marking destroys nothing, starts no new cards, leaves everything running
// alone, and is one click to undo. Adding a room, replacing its join string and
// forgetting one are all things somebody does at a terminal on the hub, because
// atrium has no login and a board reachable over an overlay must not be a way
// to enrol a machine that runs agents. See `Inventory` in internal/link.
func (i inventory) MarkRoom(name string, marked bool) error {
	r, err := i.store.ByName(name)
	if err != nil {
		return knownRooms(i.store, name, err)
	}
	return i.store.Mark(r.ID, marked)
}

// ForgetRoom drops a room's durable record.
//
// FORGET, NOT BAN, and the store's Force is the honest name for it: the record
// and its cached cards and its secret go, the machine is left holding whatever
// it had, and a room that dials in again is written down fresh. `Force` rather
// than `Remove` because Remove insists on the mark-clear-confirm ceremony a
// terminal operator goes through, and forgetting a stale or duplicate row from
// the picker is exactly the case where none of that has happened and there is
// nothing to wait for. The caller in internal/link has already refused an
// attached room, which is the one guard that matters here. See `Inventory`.
func (i inventory) ForgetRoom(name string) error {
	r, err := i.store.ByName(name)
	if err != nil {
		return knownRooms(i.store, name, err)
	}
	return i.store.Force(r.ID, "forgotten from the board")
}

// Remembered is what a room last said it was holding.
//
// THE ONLY PLACE THE CACHE IS READ, and the caller is responsible for only
// asking about a room that is not answering. That rule cannot be enforced here:
// this package cannot see a socket either, and the check that matters is made
// where the live list is, which is where the question is asked.
func (i inventory) Remembered(name string) ([]link.CardState, error) {
	r, err := i.store.ByName(name)
	if err != nil {
		return nil, err
	}
	cards, err := i.store.Cards(r.ID)
	if err != nil {
		return nil, err
	}
	out := make([]link.CardState, 0, len(cards))
	for _, c := range cards {
		out = append(out, link.CardState{ID: c.ID, Status: c.Status, Payload: c.Payload})
	}
	return out, nil
}

// Holding names the rooms with cards remembered for them.
func (i inventory) Holding() ([]string, error) { return i.store.Holding() }

// HubSkin and SetHubSkin are the board skin the ALL view wears, which is the
// hub's own rather than a room's. See `internal/link` and `internal/hubstore`.
func (i inventory) HubSkin() (string, error)     { return i.store.HubSkin() }
func (i inventory) SetHubSkin(name string) error { return i.store.SetHubSkin(name) }

// BoardAuto and SetBoardAuto are the board-wide auto-approve flag, held by the
// hub and enforced hub-side on the permission relay. Board policy, so the hub's
// to hold, the same as the skin. See `internal/link/autoapprove.go`.
func (i inventory) BoardAuto() (bool, *time.Time, error) { return i.store.BoardAuto() }
func (i inventory) SetBoardAuto(on bool, until *time.Time) error {
	return i.store.SetBoardAuto(on, until)
}

// HubSetting and SetHubSetting are the hub's settings table by name, for the
// ones internal/link keeps itself, such as the input-lag switch.
func (i inventory) HubSetting(name string) (string, error) { return i.store.HubSetting(name) }
func (i inventory) SetHubSetting(name, value string) error { return i.store.SetHubSetting(name, value) }

// ShareAuth, SetShareAuth and SetSharePass are the login a PUBLIC zrok board
// share is created behind, held by the hub the same as the skin. This adapts
// between the store's ShareAuth and the link package's identical copy, so
// neither has to import the other. See `hubSettings` in internal/link and
// `docs/ziti-zrok-flow-design.md`.
func (i inventory) ShareAuth() (link.ShareAuth, error) {
	a, err := i.store.ShareAuth()
	if err != nil {
		return link.ShareAuth{}, err
	}
	return link.ShareAuth{Scheme: a.Scheme, User: a.User, Pass: a.Pass, OIDCProvider: a.OIDCProvider}, nil
}

func (i inventory) SetShareAuth(a link.ShareAuth) error {
	return i.store.SetShareAuth(hubstore.ShareAuth{
		Scheme: a.Scheme, User: a.User, Pass: a.Pass, OIDCProvider: a.OIDCProvider,
	})
}

// fold matches how a room name is compared everywhere else: ASCII only, so a
// name folds the same on every machine regardless of locale.
func fold(s string) string {
	out := []byte(s)
	for i, c := range out {
		if c >= 'A' && c <= 'Z' {
			out[i] = c + 32
		}
	}
	return string(out)
}
