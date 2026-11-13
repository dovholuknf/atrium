package hubstore

import (
	"database/sql"
	"errors"
	"time"
)

// A card's reminders, kept on the hub so they outlive a room restart and a parked card. The hub raises a `reminder`
// growler when one is due. See internal/link/remind.go.

// Reminder states.
const (
	ReminderPending   = "pending"
	ReminderFired     = "fired"
	ReminderCancelled = "cancelled"
)

// ReminderMaxAhead is how far out a reminder may be set, and ReminderMaxPending how many one card may hold.
const (
	ReminderMaxAhead   = 90 * 24 * time.Hour
	ReminderMaxPending = 50
	reminderKeep       = 30 * 24 * time.Hour
)

// ErrReminderFull is a card already holding ReminderMaxPending.
var ErrReminderFull = errors.New("that card already holds 50 pending reminders")

// Reminder is one thing to say to a card, or about it, when it is due. Card is room~id.
type Reminder struct {
	ID        string    `json:"id"`
	Card      string    `json:"card"`
	Text      string    `json:"text"`
	DueAt     time.Time `json:"due_at"`
	Wake      bool      `json:"wake"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	State     string    `json:"state"`
}

const reminderCols = `id, card, text, due_at, wake, created_by, created_at, state`

func scanReminder(sc scanner) (Reminder, error) {
	var r Reminder
	var wake int
	var due, created string
	if err := sc.Scan(&r.ID, &r.Card, &r.Text, &due, &wake, &r.CreatedBy, &created, &r.State); err != nil {
		return r, err
	}
	r.Wake = wake != 0
	if t := parseOrNil(due); t != nil {
		r.DueAt = *t
	}
	if t := parseOrNil(created); t != nil {
		r.CreatedAt = *t
	}
	return r, nil
}

func (s *Store) reminders(where string, args ...any) ([]Reminder, error) {
	out := []Reminder{}
	err := s.guard(func() error {
		out = out[:0]
		rows, err := s.db.Query(`SELECT `+reminderCols+` FROM reminder `+where, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r, err := scanReminder(rows)
			if err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}

// ReminderAdd records a reminder. The caller has checked Text and Card, and that DueAt is inside ReminderMaxAhead.
func (s *Store) ReminderAdd(r Reminder) (Reminder, error) {
	r.ID, r.CreatedAt, r.State = newID(), s.now(), ReminderPending
	err := s.tx(func(t *sql.Tx) error {
		var n int
		if err := t.QueryRow(`SELECT COUNT(*) FROM reminder WHERE card = ? AND state = 'pending'`, r.Card).Scan(&n); err != nil {
			return err
		}
		if n >= ReminderMaxPending {
			return refuse(ErrReminderFull)
		}
		_, err := t.Exec(`INSERT INTO reminder (id, card, text, due_at, wake, created_by, created_at, state)
			VALUES (?, ?, ?, ?, ?, ?, ?, 'pending')`,
			r.ID, r.Card, r.Text, ts(r.DueAt), b2i(r.Wake), r.CreatedBy, ts(r.CreatedAt))
		return err
	})
	if errors.Is(err, ErrReminderFull) {
		return r, ErrReminderFull
	}
	return r, err
}

// ReminderPendingOf is a card's pending reminders, soonest first.
func (s *Store) ReminderPendingOf(card string) ([]Reminder, error) {
	return s.reminders(`WHERE card = ? AND state = 'pending' ORDER BY due_at, id`, card)
}

// ReminderGet is one reminder, or false.
func (s *Store) ReminderGet(id string) (Reminder, bool, error) {
	rs, err := s.reminders(`WHERE id = ?`, id)
	if err != nil || len(rs) == 0 {
		return Reminder{}, false, err
	}
	return rs[0], true, nil
}

// ReminderCancel cancels one pending reminder, and reports whether it was pending.
func (s *Store) ReminderCancel(id string) (bool, error) {
	var n int64
	at := s.now()
	err := s.guard(func() error {
		res, err := s.db.Exec(`UPDATE reminder SET state = 'cancelled', done_at = ? WHERE id = ? AND state = 'pending'`, ts(at), id)
		if err != nil {
			return err
		}
		n, _ = res.RowsAffected()
		return nil
	})
	return n > 0, err
}

// ReminderCancelCard cancels every pending reminder a card holds and says how many.
func (s *Store) ReminderCancelCard(card string) (int64, error) {
	var n int64
	at := s.now()
	err := s.guard(func() error {
		res, err := s.db.Exec(`UPDATE reminder SET state = 'cancelled', done_at = ? WHERE card = ? AND state = 'pending'`, ts(at), card)
		if err != nil {
			return err
		}
		n, _ = res.RowsAffected()
		return nil
	})
	return n, err
}

// ReminderDue is every pending reminder whose time has come, oldest first.
func (s *Store) ReminderDue() ([]Reminder, error) {
	return s.reminders(`WHERE state = 'pending' AND due_at <= ? ORDER BY due_at, id`, ts(s.now()))
}

// ReminderFire marks a reminder done. A cancelled one stays cancelled.
func (s *Store) ReminderFire(id string) error {
	at := s.now()
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE reminder SET state = 'fired', done_at = ? WHERE id = ? AND state = 'pending'`, ts(at), id)
		return err
	})
}

// ReminderPrune drops what fired or was cancelled over a month ago.
func (s *Store) ReminderPrune() error {
	cut := ts(s.now().Add(-reminderKeep))
	return s.guard(func() error {
		_, err := s.db.Exec(`DELETE FROM reminder WHERE state != 'pending' AND done_at < ?`, cut)
		return err
	})
}
