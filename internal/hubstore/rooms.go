package hubstore

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// The rooms a hub knows about, which is not the same list as the rooms
// currently dialled in.
//
// A room here is a row somebody made on purpose. It appears the moment it is
// added, before it has ever connected, and stays when it goes away. Liveness is
// a fact about a socket and lives in internal/link; this package never asks and
// must never learn.

// Room states.
const (
	// StateActive is the ordinary one.
	StateActive = "active"
	// StateMarked is a room on its way out.
	//
	// A room marked for deletion starts no new cards. Everything already
	// running continues and is worked out normally, and the room stays visible
	// throughout, because a room mid-cleanup is a thing that still exists.
	//
	// MARKING IS REVERSIBLE, which is what makes it safe to press. Nothing
	// about it destroys anything.
	StateMarked = "marked-for-deletion"
)

// Transports a room may reach a hub over. The hub decides which are on offer;
// the room pastes a token and never chooses.
const (
	TransportDirect = "direct"
	TransportZiti   = "ziti"
	TransportZrok   = "zrok"
	// TransportZrokPublic is the one decision 5 is least convinced by. It is
	// here so a row can hold it, and nothing offers it yet.
	TransportZrokPublic = "zrok-public"
	// TransportLocal is the hub's own room: a pipe inside one process, which
	// crosses no network and has nothing to enrol.
	TransportLocal = "local"
)

// Room is one row: everything the hub knows about a room without asking it.
type Room struct {
	ID string `json:"id"`
	// Name is the hub's name for this room, and the only name that routes.
	Name string `json:"name"`
	// SelfName is what the room calls itself. Observed, shown beside Name, and
	// never used for anything.
	SelfName string `json:"self_name,omitempty"`
	// Transport is a badge. See docs/hub-room-requirements.md: ancillary noise
	// that is worth seeing at a glance and never worth a column.
	Transport string     `json:"transport"`
	State     string     `json:"state"`
	CreatedAt time.Time  `json:"created_at"`
	FirstSeen *time.Time `json:"first_seen_at,omitempty"`
	LastSeen  *time.Time `json:"last_seen_at,omitempty"`
	Version   string     `json:"version,omitempty"`
	// ClearedAt is when this room last said, while connected, that it is
	// holding nothing.
	//
	// THE ROOM'S OWN CONFIRMATION, and the only kind there is. The hub cannot
	// see whether a directory was cleaned up or a session really ended, so it
	// does not decide: it waits to be told, by the room, in the only way a room
	// speaks about itself. Cleared the moment that stops being true.
	ClearedAt *time.Time `json:"cleared_at,omitempty"`
}

// EverConnected reports whether this room has ever dialled in.
//
// The board needs it because a room that never has cannot have cards. It draws
// in the rooms tab, which is the inventory, and nowhere else.
func (r Room) EverConnected() bool { return r.FirstSeen != nil }

// Marked reports whether this room is on its way out.
func (r Room) Marked() bool { return r.State == StateMarked }

// Lively is how recently a room must have been heard from for another process
// to treat it as attached.
//
// Four times the hub's heartbeat, which refreshes `last_seen_at` on every beat.
// Long enough that one slow moment does not read as a disconnection, short
// enough that a room which went away a minute ago does not read as present.
const Lively = 20 * time.Second

// LikelyAttached is an INFERENCE, and the name says so on purpose.
//
// Whether a room is attached is a fact about a socket in the hub's process, and
// nothing outside that process can know it. What this reads is how recently the
// hub wrote down having heard from it. That is enough for a command line to
// refuse to force out a room that is plainly still there, and it is not enough
// for anything that must be right: the hub itself asks its own connection list.
func (r Room) LikelyAttached() bool {
	return r.LastSeen != nil && now().Sub(*r.LastSeen) < Lively
}

// Add writes a room down and gives it a name.
//
// THE HUB NAMES THE ROOM. That name is minted into the join token, the room
// joins with the token, and it is called what the token says. It does not
// choose and it does not ask the transport, so a secret authorises exactly one
// name and there is nothing left to claim.
func (s *Store) Add(name, transport string) (*Room, error) {
	name = strings.TrimSpace(name)
	if err := checkName(name); err != nil {
		return nil, err
	}
	if transport = strings.TrimSpace(transport); transport == "" {
		transport = TransportDirect
	}
	switch transport {
	case TransportDirect, TransportZiti, TransportZrok, TransportZrokPublic, TransportLocal:
	default:
		return nil, fmt.Errorf("no transport called %q. rooms reach a hub over "+
			"direct, ziti or zrok", transport)
	}

	r := &Room{
		ID: newID(), Name: name, Transport: transport,
		State: StateActive, CreatedAt: now(),
	}
	err := s.guard(func() error {
		_, err := s.db.Exec(
			`INSERT INTO room (id, name, name_key, transport, state, created_at)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			r.ID, r.Name, fold(r.Name), r.Transport, r.State, ts(r.CreatedAt))
		return err
	})
	if isConstraint(err) {
		// The inventory question, answered. This is the whole reason the list
		// is durable rather than a picture of what is dialled in.
		return nil, fmt.Errorf("there is already a room called %q on this hub", name)
	}
	if err != nil {
		return nil, err
	}
	s.Log(r, "added", "named by the hub, reachable over "+transport)
	return r, nil
}

// EnsureLocal writes down the hub's own room, if it is not already there.
//
// THE HUB'S OWN ROOM IS A ROOM. It appears on the list beside every other one,
// because the list describes everything that can run agents and a machine you
// are sitting at is not an exception to that. It is also what decision 1's
// settings cog needs something to attach to.
//
// Idempotent, because turning the hub's room off and on again is a switch on
// the board rather than a reinstallation. Nothing here is enrolled: the
// connection never leaves the process, so there is no credential to mint and no
// secret to show once.
func (s *Store) EnsureLocal(name string) (*Room, error) {
	r, err := s.ByName(name)
	if err == nil {
		// THE NAME BEING FREE IS NOT THE SAME AS THE ROOM BEING THIS ONE.
		//
		// A machine called `sg4` could already be on this hub as a real room
		// dialling in over the network, and the hub's own room defaults to the
		// machine's name. Taking that row over would point the hub's in-process
		// room at another machine's identity and quietly break both.
		if r.Transport != TransportLocal {
			return nil, fmt.Errorf("this hub already has a room called %q that reaches "+
				"it over %s. give its own room a different name with --room-name",
				r.Name, r.Transport)
		}
		return r, nil
	}
	if !errors.Is(err, ErrNoSuchRoom) {
		return nil, err
	}
	return s.Add(name, TransportLocal)
}

// checkName refuses names that would not survive being a room.
//
// The name travels in a certificate's common name, in a URL path segment, and
// in an id tagged with a tilde (see `idJoin` in internal/link). Refusing here
// is a sentence somebody reads when they type it; letting it through is a room
// that enrols and then cannot be routed to.
func checkName(name string) error {
	if name == "" {
		return errors.New("a room needs a name. that is what you will call it on the board")
	}
	if len(name) > 64 {
		return errors.New("that name is too long for a room. sixty four characters at most")
	}
	for _, c := range name {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '-', c == '_', c == '.':
		default:
			return fmt.Errorf("a room name cannot contain %q. letters, digits, "+
				"and - _ . are what survive being put in a certificate and a URL", string(c))
		}
	}
	return nil
}

// Rooms lists every room this hub knows about, in name order.
//
// NOT IN THE ORDER THE BOARD DRAWS THEM. Live before offline before never
// connected is the board's grouping and it needs liveness, which this package
// does not have. A stable order here means the caller sorts once and the
// answer does not shuffle between two requests that changed nothing.
func (s *Store) Rooms() ([]Room, error) {
	var out []Room
	err := s.guard(func() error {
		rows, err := s.db.Query(roomCols + ` FROM room ORDER BY name_key`)
		if err != nil {
			return err
		}
		defer rows.Close()
		out = out[:0]
		for rows.Next() {
			r, err := scanRoom(rows)
			if err != nil {
				return err
			}
			out = append(out, *r)
		}
		return rows.Err()
	})
	return out, err
}

// Get reads one room by id.
func (s *Store) Get(id string) (*Room, error) {
	return s.one(`WHERE id = ?`, id)
}

// ByName reads one room by the hub's name for it, folded.
//
// This is how an attaching room is matched to its row: the name in its
// certificate came from the token, and the token came from a row.
func (s *Store) ByName(name string) (*Room, error) {
	return s.one(`WHERE name_key = ?`, fold(name))
}

const roomCols = `SELECT id, name, self_name, transport, state, created_at,
	first_seen_at, last_seen_at, version, cleared_at`

func (s *Store) one(where string, args ...any) (*Room, error) {
	var r *Room
	err := s.guard(func() error {
		row := s.db.QueryRow(roomCols+` FROM room `+where, args...)
		got, err := scanRoom(row)
		if errors.Is(err, sql.ErrNoRows) {
			return refuse(ErrNoSuchRoom)
		}
		r = got
		return err
	})
	if err != nil {
		return nil, err
	}
	return r, nil
}

type scanner interface{ Scan(...any) error }

func scanRoom(sc scanner) (*Room, error) {
	var r Room
	var created, first, last, cleared string
	if err := sc.Scan(&r.ID, &r.Name, &r.SelfName, &r.Transport, &r.State,
		&created, &first, &last, &r.Version, &cleared); err != nil {
		return nil, err
	}
	if t, err := time.Parse(TimeFormat, created); err == nil {
		r.CreatedAt = t
	}
	r.FirstSeen = parseOrNil(first)
	r.LastSeen = parseOrNil(last)
	r.ClearedAt = parseOrNil(cleared)
	return &r, nil
}

// Seen records that a room is attached right now, and what it says about
// itself.
//
// THE OBSERVED HALF, and it is written here exactly because it is not the
// truth. `self_name` and `version` are what the machine reported. The hub's own
// `name` is never touched by this, which is the observed-versus-overrides rule
// holding at the one point where breaking it would be easiest.
//
// `first_seen_at` is set once and never again. It is what separates a room that
// is offline from one that has never been anywhere.
func (s *Store) Seen(id, selfName, version string) error {
	at := ts(now())
	return s.guard(func() error {
		res, err := s.db.Exec(
			`UPDATE room
			    SET self_name = ?, version = ?, last_seen_at = ?,
			        first_seen_at = CASE WHEN first_seen_at = '' THEN ? ELSE first_seen_at END
			  WHERE id = ?`,
			strings.TrimSpace(selfName), strings.TrimSpace(version), at, at, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return refuse(ErrNoSuchRoom)
		}
		return nil
	})
}

// Mark puts a room on its way out, or takes it back off.
//
// Reversible on purpose, and the reason is in the name: marking is a decision
// about the future, not an action on anything that exists. New cards stop, what
// is running is left alone, and pressing it again is business as usual.
func (s *Store) Mark(id string, marked bool) error {
	state := StateActive
	if marked {
		state = StateMarked
	}
	r, err := s.Get(id)
	if err != nil {
		return err
	}
	if err := s.guard(func() error {
		_, err := s.db.Exec(`UPDATE room SET state = ? WHERE id = ?`, state, id)
		return err
	}); err != nil {
		return err
	}
	if marked {
		s.Log(r, "marked-for-deletion", "starts no new cards. this can be undone")
	} else {
		s.Log(r, "unmarked", "back in ordinary service")
	}
	return nil
}

// Remove deletes a room the hub is done with.
//
// THE STORE DOES NOT DECIDE WHETHER THIS IS ALLOWED. A connected room cannot be
// deleted and a room with active cards cannot be deleted, and neither of those
// is knowable from a row: one is a socket and the other is an answer only the
// room itself can give. The caller checks both and this writes the result down.
//
// `why` is kept in the audit log after the row is gone, which is the whole
// reason that table has no foreign key.
func (s *Store) Remove(id, why string) error { return s.remove(id, "removed", why) }

// Force removes a room whose machine is never coming back.
//
// Behind a large warning, and the warning is the point: FORCING REMOVES THE
// HUB'S RECORD AND NOTHING ELSE. If that machine ever comes back it is still
// holding cards, directories and sessions the hub has now forgotten about.
func (s *Store) Force(id, why string) error { return s.remove(id, "forced-out", why) }

func (s *Store) remove(id, kind, why string) error {
	r, err := s.Get(id)
	if err != nil {
		return err
	}
	if err := s.guard(func() error {
		// The cache and the secret go with it, by cascade. The audit does not.
		_, err := s.db.Exec(`DELETE FROM room WHERE id = ?`, id)
		return err
	}); err != nil {
		return err
	}
	s.Log(r, kind, why)
	return nil
}

// ── the hub's own settings ──────────────────────────────

// Setting reads one of the hub's settings, empty when it has never been set.
func (s *Store) Setting(name string) (string, error) {
	var v string
	err := s.guard(func() error {
		err := s.db.QueryRow(`SELECT value FROM hub_setting WHERE name = ?`, name).Scan(&v)
		if errors.Is(err, sql.ErrNoRows) {
			v = ""
			return nil
		}
		return err
	})
	return v, err
}

// SetSetting writes one.
func (s *Store) SetSetting(name, value string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(
			`INSERT INTO hub_setting (name, value, updated_at) VALUES (?, ?, ?)
			 ON CONFLICT (name) DO UPDATE SET value = excluded.value,
			                                  updated_at = excluded.updated_at`,
			name, value, ts(now()))
		return err
	})
}
