package store

import (
	"strings"
	"time"
)

// The relay outbox: what this room owes a card on another room. See
// docs/fabric/cross-room-say-design.md.
//
// ON THE SENDER'S ROOM, because the hub holds nothing. A row is a message or a
// launcher notice that could not be carried when it was sent, because the hub
// or the target room was not answering, and it is deleted the moment it goes.

// Relay sources. A say is a session's own words. A notice is atrium telling a
// launcher about its worker. They are dropped differently on an unconfirmed
// send: a say is not sent twice, and a notice is, since a launcher told twice
// is better than a launcher never told.
const (
	RelaySourceSay    = "say"
	RelaySourceNotice = "notice"
)

// RelayRow is one owed message.
type RelayRow struct {
	ID string
	// FromTask is the sending card on this room, empty for a sender that is
	// not a card here. FromWire is its handle, without a room.
	FromTask string
	FromWire string
	// ToRoom, ToName and ToCard are the target. ToCard, when set, is the bare
	// card id on ToRoom and is what the drain addresses.
	ToRoom string
	ToName string
	ToCard string
	Text   string
	When   string
	Source string
	// Kind is `fyi` or empty, the say's kind, sent on when it drains.
	Kind string

	CreatedAt time.Time
	Attempts  int
	LastError string
}

// RelaySpec is a row to write inside another change's transaction, the way a
// report's notice is. See ReportWrite.Relay.
type RelaySpec struct {
	FromTask, FromWire, ToRoom, ToName, ToCard, Text string
}

// HoldRelay writes one owed message and returns it.
func (s *Store) HoldRelay(r RelayRow) (*RelayRow, error) {
	r.ID, r.CreatedAt = newID(), now()
	if r.Source == "" {
		r.Source = RelaySourceSay
	}
	err := s.guard(func() error {
		return insertRelay(s.db, &r)
	})
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func insertRelay(q querier, r *RelayRow) error {
	_, err := q.Exec(`INSERT INTO relay_outbox (id, from_task, from_wire, to_room, to_name, to_card, text,
			when_word, source, kind, created_at, attempts, last_error)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,0,'')`,
		r.ID, r.FromTask, r.FromWire, r.ToRoom, r.ToName, r.ToCard, r.Text, r.When, r.Source, r.Kind, ts(r.CreatedAt))
	return err
}

// holdRelayOn writes a notice row inside a transaction.
func (s *Store) holdRelayOn(tx *Tx, spec RelaySpec) error {
	r := RelayRow{
		ID: newID(), CreatedAt: now(), Source: RelaySourceNotice,
		FromTask: spec.FromTask, FromWire: spec.FromWire,
		ToRoom: spec.ToRoom, ToName: spec.ToName, ToCard: spec.ToCard, Text: spec.Text,
	}
	return insertRelay(tx, &r)
}

// OwedRelays lists what is owed, oldest first.
func (s *Store) OwedRelays(limit int) ([]RelayRow, error) {
	if limit <= 0 {
		limit = 100
	}
	var out []RelayRow
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(`SELECT id, from_task, from_wire, to_room, to_name, to_card, text, when_word,
				source, kind, created_at, attempts, last_error
			FROM relay_outbox ORDER BY created_at, id LIMIT ?`, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r RelayRow
			var at string
			if err := rows.Scan(&r.ID, &r.FromTask, &r.FromWire, &r.ToRoom, &r.ToName, &r.ToCard, &r.Text,
				&r.When, &r.Source, &r.Kind, &at, &r.Attempts, &r.LastError); err != nil {
				return err
			}
			r.CreatedAt, _ = parseTS(at)
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}

// RelaySent deletes a row that went, or that is being given up on.
func (s *Store) RelaySent(id string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`DELETE FROM relay_outbox WHERE id = ?`, id)
		return err
	})
}

// RelayFailed records one more try that did not go, and why.
func (s *Store) RelayFailed(id, why string) error {
	why = strings.TrimSpace(why)
	if len(why) > 500 {
		why = cutRunes(why, 500)
	}
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE relay_outbox SET attempts = attempts + 1, last_error = ? WHERE id = ?`, why, id)
		return err
	})
}
