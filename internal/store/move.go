package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// What the room does for a move to another room: hold a card still, keep everything said to it meanwhile, and
// remember where it went. The hub drives the sequence and the rooms run these pieces, each safe to run twice. See
// docs/rnd/room-handoff-design.md, sections 3 and 5.

// DefaultFreezeLease is how long a freeze holds with no word from the hub.
const DefaultFreezeLease = 30 * time.Minute

// MaxMoveHops is how far a lookup follows `moved_to` before it gives up.
const MaxMoveHops = 8

var (
	// ErrMoved is a refusal to undo a card whose `moved_to` is set. After that the move only goes forward.
	ErrMoved = errors.New("the card has moved, so a move cannot be undone")
	// ErrLeaseExpired is a refusal to cut a card over once its freeze lease has run out. The card may already
	// have undone itself, so the hub's `moved_to` and the self-undo can never both happen.
	ErrLeaseExpired = errors.New("the freeze lease has run out")
	// ErrNotFrozen is a move step on a card that is not held by that move.
	ErrNotFrozen = errors.New("the card is not frozen by this move")
	// ErrFrozenByOther is a freeze on a card another move already holds.
	ErrFrozenByOther = errors.New("the card is frozen by another move")
)

// Freeze is a card held for a move.
type Freeze struct {
	TaskID     string    `json:"task_id"`
	MoveID     string    `json:"move_id"`
	FrozenAt   time.Time `json:"frozen_at"`
	LeaseUntil time.Time `json:"lease_until"`
}

// Expired reports a lease that has run out.
func (f *Freeze) Expired(at time.Time) bool { return !at.Before(f.LeaseUntil) }

// FrozenMessage is one thing said to a frozen card, kept with the id it will carry to the other room.
type FrozenMessage struct {
	ID        string    `json:"id"`
	TaskID    string    `json:"task_id"`
	Text      string    `json:"text"`
	FromPeer  string    `json:"from_peer,omitempty"`
	WaitTurn  bool      `json:"wait_turn,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// moveTx is inTx for a step that may refuse. A refusal is an answer, so it is carried out in `refused` and the
// transaction ends with nothing written, where an error out of inTx would halt the store.
func (s *Store) moveTx(fn func(tx *Tx, refuse func(error) error) error) error {
	var refused error
	err := s.inTx(func(tx *Tx) error {
		refused = nil
		return fn(tx, func(e error) error { refused = e; return nil })
	})
	if err != nil {
		return err
	}
	return refused
}

func freezeOn(q querier, id string) (*Freeze, error) {
	var f Freeze
	var at, until string
	err := q.QueryRow(`SELECT task_id, move_id, frozen_at, lease_until FROM card_freeze WHERE task_id = ?`, id).
		Scan(&f.TaskID, &f.MoveID, &at, &until)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if f.FrozenAt, err = parseTS(at); err != nil {
		return nil, err
	}
	if f.LeaseUntil, err = parseTS(until); err != nil {
		return nil, err
	}
	return &f, nil
}

// Frozen is the freeze on a card, or nil.
func (s *Store) Frozen(id string) (*Freeze, error) {
	var f *Freeze
	err := s.guard(func() error {
		var err error
		f, err = freezeOn(s.db, id)
		return err
	})
	return f, err
}

// Freeze holds a card for a move. Idempotent by move id: the same move freezing again renews the lease. A card
// another move holds, or one that has already moved, is refused.
func (s *Store) Freeze(id, moveID string, lease time.Duration) (*Freeze, error) {
	if strings.TrimSpace(moveID) == "" {
		return nil, errors.New("a freeze needs a move id")
	}
	if lease <= 0 {
		lease = DefaultFreezeLease
	}
	var out *Freeze
	err := s.moveTx(func(tx *Tx, refuse func(error) error) error {
		out = nil
		t, err := getByOn(tx, `id = ?`, id)
		if err != nil {
			return err
		}
		if t.MovedTo != "" {
			return refuse(fmt.Errorf("%w: %s", ErrMoved, t.MovedTo))
		}
		f, err := freezeOn(tx, id)
		if err != nil {
			return err
		}
		n := now()
		if f != nil && f.MoveID != moveID {
			return refuse(ErrFrozenByOther)
		}
		at := n
		if f != nil {
			at = f.FrozenAt
		}
		until := n.Add(lease)
		if _, err := tx.Exec(`INSERT INTO card_freeze (task_id, move_id, frozen_at, lease_until) VALUES (?,?,?,?)
			ON CONFLICT(task_id) DO UPDATE SET lease_until = excluded.lease_until`,
			id, moveID, ts(at), ts(until)); err != nil {
			return err
		}
		out = &Freeze{TaskID: id, MoveID: moveID, FrozenAt: at, LeaseUntil: until}
		return nil
	})
	return out, err
}

// RenewFreeze extends the lease of the freeze this move holds. A lease that has already run out is not renewed:
// the card may have undone itself.
func (s *Store) RenewFreeze(id, moveID string, lease time.Duration) error {
	if lease <= 0 {
		lease = DefaultFreezeLease
	}
	return s.moveTx(func(tx *Tx, refuse func(error) error) error {
		f, err := freezeOn(tx, id)
		if err != nil {
			return err
		}
		if f == nil || f.MoveID != moveID {
			return refuse(ErrNotFrozen)
		}
		t, err := getByOn(tx, `id = ?`, id)
		if err != nil {
			return err
		}
		if t.MovedTo == "" && f.Expired(now()) {
			return refuse(ErrLeaseExpired)
		}
		_, err = tx.Exec(`UPDATE card_freeze SET lease_until = ? WHERE task_id = ?`, ts(now().Add(lease)), id)
		return err
	})
}

// FreezeQueue is everything said to a frozen card, oldest first.
func (s *Store) FreezeQueue(id string) ([]FrozenMessage, error) {
	var out []FrozenMessage
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(`SELECT id, task_id, text, from_peer, wait_turn, created_at FROM freeze_queue
			WHERE task_id = ? ORDER BY seq ASC`, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var m FrozenMessage
			var wait int
			var at string
			if err := rows.Scan(&m.ID, &m.TaskID, &m.Text, &m.FromPeer, &wait, &at); err != nil {
				return err
			}
			m.WaitTurn = wait != 0
			if m.CreatedAt, err = parseTS(at); err != nil {
				return err
			}
			out = append(out, m)
		}
		return rows.Err()
	})
	return out, err
}

// AckFrozen drops queued messages the other room has acknowledged. Ids it does not hold are ignored, so an ack
// sent twice is harmless.
func (s *Store) AckFrozen(id string, msgIDs ...string) error {
	return s.guard(func() error {
		for _, m := range msgIDs {
			if _, err := s.db.Exec(`DELETE FROM freeze_queue WHERE task_id = ? AND id = ?`, id, m); err != nil {
				return err
			}
		}
		return nil
	})
}

// enqueueFrozenOn keeps a message for a frozen card instead of delivering it. It reports whether the card is
// frozen: false means the caller delivers as it always has.
func enqueueFrozenOn(q querier, m *Message) (bool, error) {
	f, err := freezeOn(q, m.TaskID)
	if err != nil || f == nil {
		return false, err
	}
	wait := 0
	if m.WaitTurn {
		wait = 1
	}
	_, err = q.Exec(`INSERT INTO freeze_queue (id, task_id, text, from_peer, wait_turn, created_at) VALUES (?,?,?,?,?,?)`,
		m.ID, m.TaskID, m.Text, m.FromPeer, wait, ts(m.CreatedAt))
	return err == nil, err
}

// Unfreeze lets a card go. With replay, the queue is put back on the card in order, as if it had never been held:
// the undo of a move. A card whose `moved_to` is set is never replayed, since its queue belongs to the card it
// became. Without replay the queue must already be empty, or it is dropped by the caller's ack: the cut-over's
// last step.
func (s *Store) Unfreeze(id, moveID string, replay bool) (int, error) {
	replayed := 0
	err := s.moveTx(func(tx *Tx, refuse func(error) error) error {
		replayed = 0
		f, err := freezeOn(tx, id)
		if err != nil {
			return err
		}
		if f == nil {
			return nil
		}
		if f.MoveID != moveID && moveID != "" {
			return refuse(ErrFrozenByOther)
		}
		t, err := getByOn(tx, `id = ?`, id)
		if err != nil {
			return err
		}
		if replay && t.MovedTo != "" {
			return refuse(fmt.Errorf("%w: %s", ErrMoved, t.MovedTo))
		}
		if replay {
			n, err := replayFrozenOn(tx, id)
			if err != nil {
				return err
			}
			replayed = n
		}
		_, err = tx.Exec(`DELETE FROM card_freeze WHERE task_id = ?`, id)
		return err
	})
	return replayed, err
}

// replayFrozenOn puts a card's queue back among its messages, keeping each id and its place in the order.
func replayFrozenOn(tx *Tx, id string) (int, error) {
	rows, err := tx.Query(`SELECT id, text, from_peer, wait_turn, created_at FROM freeze_queue
		WHERE task_id = ? ORDER BY seq ASC`, id)
	if err != nil {
		return 0, err
	}
	var msgs []Message
	for rows.Next() {
		var m Message
		var wait int
		var at string
		if err := rows.Scan(&m.ID, &m.Text, &m.FromPeer, &wait, &at); err != nil {
			rows.Close()
			return 0, err
		}
		m.WaitTurn = wait != 0
		m.TaskID = id
		if m.CreatedAt, err = parseTS(at); err != nil {
			rows.Close()
			return 0, err
		}
		msgs = append(msgs, m)
	}
	rows.Close()
	for _, m := range msgs {
		wait := 0
		if m.WaitTurn {
			wait = 1
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO message (id, task_id, text, created_at, from_peer, wait_turn)
			VALUES (?,?,?,?,?,?)`, m.ID, id, m.Text, ts(m.CreatedAt), m.FromPeer, wait); err != nil {
			return 0, err
		}
	}
	if _, err := tx.Exec(`DELETE FROM freeze_queue WHERE task_id = ?`, id); err != nil {
		return 0, err
	}
	return len(msgs), nil
}

// ExpireFreeze is a card undoing its own freeze once the lease has run out with no word from the hub and no
// cut-over begun. It and SetMovedTo are one transaction each over the same rows, so they cannot both succeed: an
// expired lease refuses SetMovedTo, and a `moved_to` refuses this. Reports whether it undid the freeze.
func (s *Store) ExpireFreeze(id string, at time.Time) (bool, error) {
	undone := false
	err := s.inTx(func(tx *Tx) error {
		undone = false
		f, err := freezeOn(tx, id)
		if err != nil || f == nil || !f.Expired(at) {
			return err
		}
		t, err := getByOn(tx, `id = ?`, id)
		if err != nil {
			return err
		}
		if t.MovedTo != "" {
			return nil
		}
		if _, err := replayFrozenOn(tx, id); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM card_freeze WHERE task_id = ?`, id); err != nil {
			return err
		}
		undone = true
		return nil
	})
	return undone, err
}

// ExpiredFreezes lists the cards whose lease has run out.
func (s *Store) ExpiredFreezes(at time.Time) ([]string, error) {
	var ids []string
	err := s.guard(func() error {
		ids = nil
		rows, err := s.db.Query(`SELECT task_id FROM card_freeze WHERE lease_until <= ?`, ts(at))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return rows.Err()
	})
	return ids, err
}

// SetMovedTo records where a frozen card went, `room~id`. Refused once the freeze lease has run out. Idempotent
// for the same value. From here the card's queue forwards and is never replayed.
func (s *Store) SetMovedTo(id, moveID, to string) error {
	to = strings.TrimSpace(to)
	if to == "" {
		return errors.New("moved_to needs the card it became")
	}
	return s.moveTx(func(tx *Tx, refuse func(error) error) error {
		t, err := getByOn(tx, `id = ?`, id)
		if err != nil {
			return err
		}
		if t.MovedTo == to {
			return nil
		}
		if t.MovedTo != "" {
			return refuse(fmt.Errorf("%w: it already moved to %s", ErrMoved, t.MovedTo))
		}
		f, err := freezeOn(tx, id)
		if err != nil {
			return err
		}
		if f == nil || f.MoveID != moveID {
			return refuse(ErrNotFrozen)
		}
		if f.Expired(now()) {
			return refuse(ErrLeaseExpired)
		}
		_, err = tx.Exec(`UPDATE task SET moved_to = ? WHERE id = ?`, to, id)
		return err
	})
}

// SetMovedFrom records the card a new one came from, `room~id`.
func (s *Store) SetMovedFrom(id, from string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE task SET moved_from = ? WHERE id = ?`, strings.TrimSpace(from), id)
		return err
	})
}

// MovedHandleHeld reports whether a handle belongs to a card that moved, which keeps it reserved so no later card
// on this room takes it. The card keeps its `wire_name`, and `wire_name` is unique, so this is what says why.
func (s *Store) MovedHandleHeld(handle string) bool {
	held := false
	_ = s.guard(func() error {
		var one int
		err := s.db.QueryRow(`SELECT 1 FROM task WHERE wire_name = ? AND moved_to != '' LIMIT 1`, handle).Scan(&one)
		held = err == nil
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	})
	return held
}

// SeenMove records a move message id, and reports whether it is new. The receiving room drops a repeat.
func (s *Store) SeenMove(msgID, taskID string) (bool, error) {
	fresh := false
	err := s.guard(func() error {
		res, err := s.db.Exec(`INSERT OR IGNORE INTO move_seen (msg_id, task_id, seen_at) VALUES (?,?,?)`,
			msgID, taskID, ts(now()))
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		fresh = n > 0
		return nil
	})
	return fresh, err
}

// ForgetMove drops a seen id, for a delivery that failed after it was recorded so the retry is not taken for a
// repeat.
func (s *Store) ForgetMove(msgID string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`DELETE FROM move_seen WHERE msg_id = ?`, msgID)
		return err
	})
}
