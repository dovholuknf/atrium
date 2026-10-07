package store

import (
	"errors"
	"strings"
	"time"
)

// A message is something the operator wants to say to a running session.
//
// There is no channel into a claude session from outside. It cannot be typed
// at, and nothing can hand its console to another process after the fact. What
// it does have is hooks, and a hook can carry text back to the model:
//
//   - A busy session is making tool calls, and the permission hook's answer
//     carries a free text reason. A queued message is delivered as a block
//     with that message as the reason, which the model reads as an
//     instruction and acts on.
//   - An idle session is making no tool calls at all, which is exactly when
//     you most want to reach it. Its Stop hook fires as the turn ends, and a
//     Stop hook that blocks tells the model to keep going with the reason it
//     was given.
//
// Between the two, a message lands whatever the session is doing.
type Message struct {
	ID          string     `json:"id"`
	TaskID      string     `json:"task_id"`
	Text        string     `json:"text"`
	CreatedAt   time.Time  `json:"created_at"`
	DeliveredAt *time.Time `json:"delivered_at,omitempty"`
	// Via records how it reached the session: "permission" or "stop". Worth
	// knowing, because one of those costs the agent a refused tool call.
	Via string `json:"via,omitempty"`
	// FromPeer is the wire name of the session that sent this, or empty when
	// the operator did.
	//
	// It exists so the banner can say who. A model that reads a peer's request
	// as an instruction from the human acts on it with an authority the peer
	// does not have, and the only thing standing between those two readings is
	// what the envelope says.
	FromPeer string `json:"from_peer,omitempty"`
	// WaitTurn is `when: "done"`: this message waits for the session's turn to
	// end, so the permission hook leaves it and the Stop hook carries it. Set
	// too when the runner does not take input mid-turn.
	WaitTurn bool `json:"wait_turn,omitempty"`
}

// FromHuman reports whether the operator wrote this, as opposed to another
// session.
func (m *Message) FromHuman() bool { return m.FromPeer == "" }

// QueueMessage stores something to say to a session the next time it is
// reachable. From the operator.
func (s *Store) QueueMessage(taskID, text string) (*Message, error) {
	return s.queueMessage(taskID, text, "", "", false)
}

// QueueAfterTurn stores a message that waits for the session's turn to end.
// fromPeer is empty for the operator. See Message.WaitTurn.
func (s *Store) QueueAfterTurn(taskID, text, fromPeer string) (*Message, error) {
	return s.queueMessage(taskID, text, strings.TrimSpace(fromPeer), "", true)
}

// QueueFromPeer stores something one session said to another.
//
// Separate from QueueMessage so that a caller has to decide which it is. A
// single function with an optional sender is one defaulted argument away from
// a peer message that claims to be from you.
func (s *Store) QueueFromPeer(taskID, text, fromPeer string) (*Message, error) {
	if strings.TrimSpace(fromPeer) == "" {
		return nil, errors.New("a peer message has to say which session sent it")
	}
	return s.queueMessage(taskID, text, fromPeer, "", false)
}

// QueuePeerKind is QueueFromPeer, or QueueAfterTurn when waitTurn, for a
// message whose sender gave it a kind. PromptFYI is recorded on the prompt, so
// a launcher's fyi does not make the card owe a report. See promptOwes.
func (s *Store) QueuePeerKind(taskID, text, fromPeer, kind string, waitTurn bool) (*Message, error) {
	fromPeer = strings.TrimSpace(fromPeer)
	if fromPeer == "" && !waitTurn {
		return nil, errors.New("a peer message has to say which session sent it")
	}
	return s.queueMessage(taskID, text, fromPeer, kind, waitTurn)
}

// QueueCaused is QueuePeerKind for a message whose sender knows what kind of delivery it is, one of the Delivery
// kinds, which the turn cost report files its turn under. See delivery.go.
func (s *Store) QueueCaused(taskID, text, fromPeer, kind, cause string, waitTurn bool) (*Message, error) {
	m, err := s.QueuePeerKind(taskID, text, fromPeer, kind, waitTurn)
	if err != nil {
		return nil, err
	}
	// Best effort: a message that cannot be tagged is still queued, and files under what its sender says it is.
	_ = s.SetMessageCause(m.ID, cause)
	return m, nil
}

func (s *Store) queueMessage(taskID, text, fromPeer, kind string, waitTurn bool) (*Message, error) {
	m := &Message{
		ID: newID(), TaskID: taskID, Text: text,
		CreatedAt: now(), FromPeer: fromPeer, WaitTurn: waitTurn,
	}
	wait := 0
	if waitTurn {
		wait = 1
	}
	err := s.guard(func() error {
		// A card held for a move keeps what is said to it in the freeze queue, with this id, to forward or to
		// replay. Says, reports, notices, nags and wakes all queue here, so this is the one place to hold them.
		if held, err := enqueueFrozenOn(s.db, m); err != nil || held {
			return err
		}
		if _, err := s.db.Exec(
			`INSERT INTO message (id, task_id, text, created_at, from_peer, wait_turn) VALUES (?,?,?,?,?,?)`,
			m.ID, m.TaskID, m.Text, ts(m.CreatedAt), m.FromPeer, wait); err != nil {
			return err
		}
		ev := map[string]any{"queued": true, "text": text, "from_peer": fromPeer}
		if waitTurn {
			ev["when"] = "done"
		}
		if kind == PromptFYI {
			ev["kind"] = PromptFYI
		}
		return s.appendEvent(taskID, EventPrompted, ev)
	})
	if err != nil {
		return nil, err
	}
	return m, nil
}

// PendingMessages returns everything queued for a task, oldest first.
func (s *Store) PendingMessages(taskID string) ([]*Message, error) {
	var out []*Message
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(
			`SELECT id, task_id, text, created_at, from_peer, wait_turn FROM message
			 WHERE task_id = ? AND delivered_at IS NULL ORDER BY created_at ASC`, taskID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				m       Message
				created string
				wait    int
			)
			if err := rows.Scan(&m.ID, &m.TaskID, &m.Text, &created, &m.FromPeer, &wait); err != nil {
				return err
			}
			m.WaitTurn = wait != 0
			if m.CreatedAt, err = parseTS(created); err != nil {
				return err
			}
			out = append(out, &m)
		}
		return rows.Err()
	})
	return out, err
}

// BackdateMessage moves a message's creation into the past. Test support: the
// alternative is a test that sleeps for a quarter of an hour.
func (s *Store) BackdateMessage(id string, by time.Duration) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE message SET created_at = ? WHERE id = ?`, ts(now().Add(-by)), id)
		return err
	})
}

// MarkDelivered records that a message reached the session, and how.
func (s *Store) MarkDelivered(taskID, via string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	return s.guard(func() error {
		n := ts(now())
		for _, id := range ids {
			if _, err := s.db.Exec(
				`UPDATE message SET delivered_at = ?, via = ? WHERE id = ?`, n, via, id); err != nil {
				return err
			}
		}
		// The say record, when this message was one. Every delivery path comes
		// through here. See say.go.
		if err := markSaysDelivered(s.db, via, ids); err != nil {
			return err
		}
		// And a delivery, which the turn that follows is costed against. Best effort: the cost report is not
		// worth a message that stays queued.
		_ = recordMessageDeliveries(s.db, taskID, via, ids)
		return s.appendEvent(taskID, EventPrompted, map[string]any{
			"delivered": len(ids), "via": via,
		})
	})
}

// UndeliveredCounts returns how many messages are waiting per task, so the
// board can show which sessions have something pending for them.
func (s *Store) UndeliveredCounts() (map[string]int, error) {
	out := map[string]int{}
	err := s.guard(func() error {
		rows, err := s.db.Query(
			`SELECT task_id, COUNT(*) FROM message WHERE delivered_at IS NULL GROUP BY task_id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				id string
				n  int
			)
			if err := rows.Scan(&id, &n); err != nil {
				return err
			}
			out[id] = n
		}
		return rows.Err()
	})
	return out, err
}

// UndeliveredCount is UndeliveredCounts for one card.
func (s *Store) UndeliveredCount(taskID string) (int, error) {
	n := 0
	err := s.guard(func() error {
		return s.db.QueryRow(
			`SELECT COUNT(*) FROM message WHERE task_id = ? AND delivered_at IS NULL`, taskID).Scan(&n)
	})
	return n, err
}
