package hubstore

import (
	"log"
	"time"
)

// What happened to a room, kept after the room is gone.
//
// ── why this outlives its subject ───────────────────────
//
// The table carries no foreign key on purpose. Forcing out a machine that is
// never coming back removes its row, and the record of having done that is
// exactly what somebody will want afterwards. A cascade would delete the
// explanation along with the thing it explains.
//
// ── and why it exists at all ────────────────────────────
//
// A room coming back replaces its cache wholesale. That is the right rule and
// it is also the one that can quietly lose something a person remembers
// seeing. The log is what turns "I am sure there was a card there" from an
// argument into a lookup.

// Entry is one line of the log.
type Entry struct {
	ID string    `json:"id"`
	At time.Time `json:"at"`
	// RoomID is empty for a room that has since been removed, which is the
	// case this table is mostly for.
	RoomID string `json:"room_id,omitempty"`
	// RoomName is denormalised so a forced-out room still reads as something
	// rather than as an identifier nothing resolves.
	RoomName string `json:"room_name"`
	Kind     string `json:"kind"`
	Detail   string `json:"detail,omitempty"`
}

// Log writes one line.
//
// BEST EFFORT, AND IT SAYS SO ON FAILURE. Every caller here is in the middle of
// doing the thing being recorded, and failing an announcement because its
// audit line would not write is the tail wagging the dog. A store that cannot
// write has already halted through `guard`, which is the loud half.
func (s *Store) Log(r *Room, kind, detail string) {
	var id, name string
	if r != nil {
		id, name = r.ID, r.Name
	}
	err := s.guard(func() error {
		_, err := s.db.Exec(
			`INSERT INTO room_audit (id, at, room_id, room_name, kind, detail)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			newID(), ts(now()), id, name, kind, detail)
		return err
	})
	if err != nil {
		log.Printf("[hub] could not record %q for room %q: %v", kind, name, err)
	}
}

// Audit reads the log back, newest first.
func (s *Store) Audit(limit int) ([]Entry, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	var out []Entry
	err := s.guard(func() error {
		rows, err := s.db.Query(
			`SELECT id, at, room_id, room_name, kind, detail
			   FROM room_audit ORDER BY at DESC, id DESC LIMIT ?`, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		out = out[:0]
		for rows.Next() {
			var e Entry
			var at string
			if err := rows.Scan(&e.ID, &at, &e.RoomID, &e.RoomName, &e.Kind, &e.Detail); err != nil {
				return err
			}
			if t, err := time.Parse(TimeFormat, at); err == nil {
				e.At = t
			}
			out = append(out, e)
		}
		return rows.Err()
	})
	return out, err
}

// AuditFor reads the log for one room, newest first.
func (s *Store) AuditFor(roomID string, limit int) ([]Entry, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	var out []Entry
	err := s.guard(func() error {
		rows, err := s.db.Query(
			`SELECT id, at, room_id, room_name, kind, detail
			   FROM room_audit WHERE room_id = ? ORDER BY at DESC, id DESC LIMIT ?`,
			roomID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		out = out[:0]
		for rows.Next() {
			var e Entry
			var at string
			if err := rows.Scan(&e.ID, &at, &e.RoomID, &e.RoomName, &e.Kind, &e.Detail); err != nil {
				return err
			}
			if t, err := time.Parse(TimeFormat, at); err == nil {
				e.At = t
			}
			out = append(out, e)
		}
		return rows.Err()
	})
	return out, err
}
