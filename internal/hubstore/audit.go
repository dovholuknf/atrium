package hubstore

import (
	"log"
	"strings"
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

// auditCap is how many audit rows are kept. A rolling window, trimmed on write:
// the log's value is history, so the cap is generous, but a fleet that attaches,
// detaches and launches all day cannot be allowed to grow the file forever. A
// row is small, so thousands cost little and answer "what happened last week".
//
// Mirrors the event-sink's bounding posture: keep a window, drop the oldest,
// never stall the writer. The trim runs in the same guard as the insert, so it
// retries contention and never halts the hub on its own.
const auditCap = 5000

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
// A NIL ROOM IS A HUB-LEVEL LINE. The hub starting, a launch refused, the board
// put on a share: these belong to no room, and the row is written with an empty
// room the same as a forced-out one reads.
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
		if _, err := s.db.Exec(
			`INSERT INTO room_audit (id, at, room_id, room_name, kind, detail)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			newID(), ts(now()), id, name, kind, detail); err != nil {
			return err
		}
		// THE ROLLING TRIM, in the same guard as the insert so it retries
		// contention rather than halting.
		//
		// CHEAP ON EVERY WRITE, which is the whole point of the shape. The
		// subquery is one seek down the `room_audit_at` index to the cap-th
		// newest `at`, and the delete is an indexed range under it. Under the cap
		// the subquery returns NULL, `at < NULL` is never true, and nothing is
		// scanned. A NOT-IN over the kept ids reads the same rows but scans the
		// whole table per insert, which turned a best-effort log into a stall.
		//
		// Bounded, not exact: a tie on `at` at the boundary can keep a few rows
		// over the cap, which is what a rolling window is allowed to do.
		_, err := s.db.Exec(
			`DELETE FROM room_audit WHERE at < (
			   SELECT at FROM room_audit ORDER BY at DESC LIMIT 1 OFFSET ?)`,
			auditCap-1)
		return err
	})
	if err != nil {
		log.Printf("[hub] could not record %q for room %q: %v", kind, name, err)
	}
}

// AuditWhere reads the log back newest first, optionally filtered by room name
// and by kind. An empty filter matches everything, so this answers the plain
// "everything, newest first" too.
//
// THE ROOM IS MATCHED BY NAME, FOLDED, the same as everywhere else, so asking
// about `Sparta` finds what was written about `sparta`, and it keeps working
// after the room is gone, which is the case this table exists for.
func (s *Store) AuditWhere(limit int, room, kind string) ([]Entry, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	q := `SELECT id, at, room_id, room_name, kind, detail FROM room_audit`
	var where []string
	var args []any
	if r := strings.TrimSpace(room); r != "" {
		where = append(where, "LOWER(room_name) = ?")
		args = append(args, fold(r))
	}
	if k := strings.TrimSpace(kind); k != "" {
		where = append(where, "kind = ?")
		args = append(args, k)
	}
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY at DESC, id DESC LIMIT ?"
	args = append(args, limit)

	var out []Entry
	err := s.guard(func() error {
		rows, err := s.db.Query(q, args...)
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

// AuditByName reads the log for a room BY THE NAME IT WAS CALLED, newest first.
//
// THE ONE THAT WORKS AFTER THE ROOM IS GONE, which is the case this table
// exists for. `AuditFor` takes an id, so it can only answer for a room that is
// still on the list, and the question somebody actually asks is about the
// machine that was forced out last week.
//
// Folded the same way a room name is compared everywhere else, so asking about
// `Sparta` finds what was written about `sparta`.
func (s *Store) AuditByName(name string, limit int) ([]Entry, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	var out []Entry
	err := s.guard(func() error {
		rows, err := s.db.Query(
			`SELECT id, at, room_id, room_name, kind, detail
			   FROM room_audit WHERE LOWER(room_name) = ?
			  ORDER BY at DESC, id DESC LIMIT ?`, fold(name), limit)
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

// AuditFor reads the log for one room that is still on the list, newest first.
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
