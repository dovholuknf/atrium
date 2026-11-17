package store

// THE ROOM'S OWN EVENT LOG. A write this room could not hand to the hub is a row here, numbered by this room with no
// gap and no reuse (AUTOINCREMENT), and it stays until the hub took it. The hub keeps (room, seq) of what it applied,
// so sending a row twice is safe. See docs/design/hub-offline-tolerance.md, stage 1.

// Kinds of room event.
const (
	RoomEventBacklogFile = "backlog.file"
	RoomEventBacklogEdit = "backlog.edit"
	RoomEventMergeAsk    = "merge.ask"
)

// RoomEvent is one row of the log.
type RoomEvent struct {
	Seq       int64
	Kind      string
	Key       string
	Sender    string
	Method    string
	Path      string
	Body      string
	At        string
	SentAt    string
	Attempts  int
	LastError string
}

// RoomEventAppend writes one event and returns its seq.
func (s *Store) RoomEventAppend(e RoomEvent) (int64, error) {
	var seq int64
	err := s.guard(func() error {
		res, err := s.db.Exec(`INSERT INTO room_event (kind, key, sender, method, path, body, at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			e.Kind, e.Key, e.Sender, e.Method, e.Path, e.Body, ts(s.now()))
		if err != nil {
			return err
		}
		seq, err = res.LastInsertId()
		return err
	})
	return seq, err
}

// RoomEventSetBody gives an event its key and body once they are known: a filing's id, the hub's or this room's.
func (s *Store) RoomEventSetBody(seq int64, key, body string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE room_event SET key = ?, body = ? WHERE seq = ?`, key, body, seq)
		return err
	})
}

// RoomEventsAfter is the events after a seq, oldest first, sent or not. A hub that says it holds this room up to a seq is
// owed exactly these, which is how a hub that never saw this room is caught up from its log.
func (s *Store) RoomEventsAfter(after int64, limit int) ([]RoomEvent, error) {
	var out []RoomEvent
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(`SELECT seq, kind, key, sender, method, path, body, at, sent_at, attempts, last_error
			FROM room_event WHERE seq > ? ORDER BY seq LIMIT ?`, after, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e RoomEvent
			if err := rows.Scan(&e.Seq, &e.Kind, &e.Key, &e.Sender, &e.Method, &e.Path, &e.Body, &e.At, &e.SentAt, &e.Attempts,
				&e.LastError); err != nil {
				return err
			}
			out = append(out, e)
		}
		return rows.Err()
	})
	return out, err
}

// RoomEventMax is the highest seq this room wrote, 0 for none.
func (s *Store) RoomEventMax() (n int64, err error) {
	err = s.guard(func() error {
		return s.db.QueryRow(`SELECT COALESCE(MAX(seq), 0) FROM room_event`).Scan(&n)
	})
	return n, err
}

// RoomEventSent marks an event the hub took. The row stays, as the log, and a refusal the hub gave is kept as its note.
func (s *Store) RoomEventSent(seq int64, note string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE room_event SET sent_at = ?, last_error = ? WHERE seq = ?`, ts(s.now()), note, seq)
		return err
	})
}

// RoomEventFailed counts a try and keeps why.
func (s *Store) RoomEventFailed(seq int64, why string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE room_event SET attempts = attempts + 1, last_error = ? WHERE seq = ?`, why, seq)
		return err
	})
}

// RoomEventsHeld is how many events wait for the hub, and when the oldest was written.
func (s *Store) RoomEventsHeld() (n int, oldest string, err error) {
	err = s.guard(func() error {
		return s.db.QueryRow(`SELECT COUNT(*), COALESCE(MIN(at), '') FROM room_event WHERE sent_at = ''`).Scan(&n, &oldest)
	})
	return n, oldest, err
}
