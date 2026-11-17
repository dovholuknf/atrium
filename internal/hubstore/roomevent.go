package hubstore

// WHAT THE HUB HAS TAKEN OF A ROOM'S EVENT LOG. The room owns the log and numbers it. The hub keeps only (room, seq),
// so a row sent twice is applied once. See docs/design/hub-offline-tolerance.md, stage 1.

// RoomEventSeen is whether the hub already took this room's event.
func (s *Store) RoomEventSeen(room string, seq int64) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM room_event_seen WHERE room = ? AND seq = ?`, fold(room), seq).Scan(&n)
	return n > 0, err
}

// RoomEventTake records an event as taken, applied or refused. Taking one twice changes nothing.
func (s *Store) RoomEventTake(room string, seq int64, kind, key string, applied bool) error {
	a := 0
	if applied {
		a = 1
	}
	_, err := s.db.Exec(`INSERT OR IGNORE INTO room_event_seen (room, seq, kind, key, applied, at) VALUES (?, ?, ?, ?, ?, ?)`,
		fold(room), seq, kind, key, a, ts(s.now()))
	return err
}

// RoomEventHigh is the highest seq the hub took of a room, 0 for none.
func (s *Store) RoomEventHigh(room string) (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COALESCE(MAX(seq), 0) FROM room_event_seen WHERE room = ?`, fold(room)).Scan(&n)
	return n, err
}
