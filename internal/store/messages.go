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
}

// FromHuman reports whether the operator wrote this, as opposed to another
// session.
func (m *Message) FromHuman() bool { return m.FromPeer == "" }

// QueueMessage stores something to say to a session the next time it is
// reachable. From the operator.
func (s *Store) QueueMessage(taskID, text string) (*Message, error) {
	return s.queueMessage(taskID, text, "")
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
	return s.queueMessage(taskID, text, fromPeer)
}

func (s *Store) queueMessage(taskID, text, fromPeer string) (*Message, error) {
	m := &Message{
		ID: newID(), TaskID: taskID, Text: text,
		CreatedAt: now(), FromPeer: fromPeer,
	}
	err := s.guard(func() error {
		if _, err := s.db.Exec(
			`INSERT INTO message (id, task_id, text, created_at, from_peer) VALUES (?,?,?,?,?)`,
			m.ID, m.TaskID, m.Text, ts(m.CreatedAt), m.FromPeer); err != nil {
			return err
		}
		return s.appendEvent(taskID, EventPrompted, map[string]any{
			"queued": true, "text": text, "from_peer": fromPeer,
		})
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
			`SELECT id, task_id, text, created_at, from_peer FROM message
			 WHERE task_id = ? AND delivered_at IS NULL ORDER BY created_at ASC`, taskID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				m       Message
				created string
			)
			if err := rows.Scan(&m.ID, &m.TaskID, &m.Text, &created, &m.FromPeer); err != nil {
				return err
			}
			if m.CreatedAt, err = parseTS(created); err != nil {
				return err
			}
			out = append(out, &m)
		}
		return rows.Err()
	})
	return out, err
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
