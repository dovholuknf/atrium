package store

import (
	"database/sql"
	"strings"
	"time"
)

// A say's lifecycle, on record. See docs/say-lifecycle-design.md.
//
// One row per say from a session to a session. The operator's own channel is
// not recorded: the lifecycle is about one session speaking to another. The
// text is NOT copied, only a preview, because a queued say's full text is in
// `message` and a typed one's is in the `prompted` event.

// Say states. See the design for what each means.
const (
	SayQueued      = "queued"
	SayDelivered   = "delivered"
	SayHeld        = "held"
	SayHanded      = "handed"
	SayUnconfirmed = "unconfirmed"
	SayRefused     = "refused"
	SayUnresolved  = "unresolved"
)

// Say channels, once delivered.
const (
	SayViaTerminal = "terminal"
	SayViaHook     = "hook"
	SayViaStop     = "stop"
)

const (
	sayPreviewLen = 200
	sayNoteLen    = 300

	// SayKeep is how long a row is kept, SayKeepMax how many, and SayOwedFor
	// how long a reply asked for stays owed before it lapses.
	SayKeep    = 30 * 24 * time.Hour
	SayKeepMax = 2000
	SayOwedFor = 7 * 24 * time.Hour
)

// Say is one row.
type Say struct {
	ID          string `json:"id"`
	FromTask    string `json:"from_task,omitempty"`
	FromWire    string `json:"from"`
	ToTask      string `json:"to_task,omitempty"`
	ToWire      string `json:"to,omitempty"`
	ToInput     string `json:"to_input"`
	Via         string `json:"via"`
	Room        string `json:"room,omitempty"`
	Door        string `json:"door"`
	Preview     string `json:"preview"`
	Chars       int    `json:"chars"`
	When        string `json:"when,omitempty"`
	State       string `json:"state"`
	Channel     string `json:"channel,omitempty"`
	MessageID   string `json:"message_id,omitempty"`
	RelayID     string `json:"relay_id,omitempty"`
	Note        string `json:"note,omitempty"`
	ReplyWant   bool   `json:"reply_wanted,omitempty"`
	RepliedAt   string `json:"replied_at,omitempty"`
	Lapsed      bool   `json:"lapsed,omitempty"`
	ResetAt     string `json:"reset_at,omitempty"`
	ResetKind   string `json:"reset_kind,omitempty"`
	SentAt      string `json:"sent_at"`
	QueuedAt    string `json:"queued_at,omitempty"`
	DeliveredAt string `json:"delivered_at,omitempty"`
}

// Open reports whether this row is a reply still owed.
func (s Say) Open() bool { return s.ReplyWant && s.RepliedAt == "" && !s.Lapsed }

const sayCols = `id, from_task, from_wire, to_task, to_wire, to_input, via, room, door, preview, chars,
	when_word, state, channel, message_id, relay_id, note, reply_wanted, replied_at, lapsed, reset_at,
	reset_kind, sent_at, queued_at, delivered_at`

func scanSay(r interface{ Scan(...any) error }) (Say, error) {
	var (
		v      Say
		rw, lp int
	)
	err := r.Scan(&v.ID, &v.FromTask, &v.FromWire, &v.ToTask, &v.ToWire, &v.ToInput, &v.Via, &v.Room, &v.Door,
		&v.Preview, &v.Chars, &v.When, &v.State, &v.Channel, &v.MessageID, &v.RelayID, &v.Note, &rw,
		&v.RepliedAt, &lp, &v.ResetAt, &v.ResetKind, &v.SentAt, &v.QueuedAt, &v.DeliveredAt)
	v.ReplyWant, v.Lapsed = rw != 0, lp != 0
	return v, err
}

// RecordSay writes a row and returns its id. Best effort for the caller: a say
// is not held back because its own bookkeeping failed.
func (s *Store) RecordSay(v Say, text string) (string, error) {
	v.ID = newID()
	v.Chars = len([]rune(text))
	v.Preview = cutRunes(strings.TrimSpace(text), sayPreviewLen)
	v.Note = cutRunes(v.Note, sayNoteLen)
	if v.SentAt == "" {
		v.SentAt = ts(now())
	}
	if v.State == SayQueued && v.QueuedAt == "" {
		v.QueuedAt = v.SentAt
	}
	if v.State == SayDelivered && v.DeliveredAt == "" {
		v.DeliveredAt = v.SentAt
	}
	rw := 0
	if v.ReplyWant {
		rw = 1
	}
	err := s.guard(func() error {
		_, err := s.db.Exec(`INSERT INTO say (`+sayCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,0,?,?,?,?,?)`,
			v.ID, v.FromTask, v.FromWire, v.ToTask, v.ToWire, v.ToInput, v.Via, v.Room, v.Door, v.Preview,
			v.Chars, v.When, v.State, v.Channel, v.MessageID, v.RelayID, v.Note, rw, v.RepliedAt,
			v.ResetAt, v.ResetKind, v.SentAt, v.QueuedAt, v.DeliveredAt)
		if err != nil || v.MessageID == "" {
			return err
		}
		// A hook can drain the queue between the message being queued and this
		// row being written. Adopt a delivery that already happened.
		var via string
		var at sql.NullString
		if e := s.db.QueryRow(`SELECT COALESCE(via,''), delivered_at FROM message WHERE id = ?`,
			v.MessageID).Scan(&via, &at); e == nil && at.Valid && at.String != "" {
			_, err = s.db.Exec(`UPDATE say SET state = ?, channel = ?, delivered_at = ? WHERE id = ?`,
				SayDelivered, sayChannel(via), at.String, v.ID)
		}
		return err
	})
	return v.ID, err
}

// SayQueuedAs moves a row to queued and ties it to its queue row.
func (s *Store) SayQueuedAs(id, messageID string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE say SET state = ?, message_id = ?, queued_at = ? WHERE id = ? AND state != ?`,
			SayQueued, messageID, ts(now()), id, SayDelivered)
		return err
	})
}

// SayDeliveredAs marks a row delivered by a channel, for a say that never had a
// queue row.
func (s *Store) SayDeliveredAs(id, channel string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE say SET state = ?, channel = ?, delivered_at = ? WHERE id = ?`,
			SayDelivered, channel, ts(now()), id)
		return err
	})
}

// SayHeldAs ties a row to the relay outbox row that holds it.
func (s *Store) SayHeldAs(id, relayID string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE say SET state = ?, relay_id = ? WHERE id = ?`, SayHeld, relayID, id)
		return err
	})
}

// SayRelayed moves a held row on when the drain sends it or gives up.
func (s *Store) SayRelayed(relayID, state, note string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE say SET state = ?, note = ? WHERE relay_id = ?`,
			state, cutRunes(note, sayNoteLen), relayID)
		return err
	})
}

// sayChannel names what MarkDelivered's `via` was, in the words the record uses.
func sayChannel(via string) string {
	switch via {
	case "permission":
		return SayViaHook
	case "stop":
		return SayViaStop
	}
	return SayViaTerminal
}

// markSaysDelivered is MarkDelivered's half: every delivery path goes through it,
// so no call site has to remember.
func markSaysDelivered(db *sql.DB, via string, ids []string) error {
	n := ts(now())
	for _, id := range ids {
		if _, err := db.Exec(`UPDATE say SET state = ?, channel = ?, delivered_at = ? WHERE message_id = ?`,
			SayDelivered, sayChannel(via), n, id); err != nil {
			return err
		}
	}
	return nil
}

// SaysFor lists the says a card sent or received, newest first.
func (s *Store) SaysFor(taskID string, limit int) ([]Say, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var out []Say
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(`SELECT `+sayCols+` FROM say WHERE to_task = ? OR from_task = ?
			ORDER BY sent_at DESC, id DESC LIMIT ?`, taskID, taskID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			v, err := scanSay(rows)
			if err != nil {
				return err
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, err
}

// SayByID returns one row.
func (s *Store) SayByID(id string) (*Say, error) {
	var out *Say
	err := s.guard(func() error {
		v, err := scanSay(s.db.QueryRow(`SELECT `+sayCols+` FROM say WHERE id = ?`, id))
		if err != nil {
			return err
		}
		out = &v
		return nil
	})
	return out, err
}

// AnswerSaysFrom settles every reply `from` asked `to` for: `to` said something
// back. Only the ones between these two, the way AnswerAsksFrom does.
func (s *Store) AnswerSaysFrom(toTask, fromTask string) (int, error) {
	if toTask == "" || fromTask == "" {
		return 0, nil
	}
	n := 0
	err := s.guard(func() error {
		res, err := s.db.Exec(`UPDATE say SET replied_at = ? WHERE to_task = ? AND from_task = ?
			AND reply_wanted = 1 AND replied_at = '' AND lapsed = 0`, ts(now()), toTask, fromTask)
		if err != nil {
			return err
		}
		got, _ := res.RowsAffected()
		n = int(got)
		return nil
	})
	return n, err
}

// RepliesOwed counts open owed replies per receiving card, for the whole list.
func (s *Store) RepliesOwed() (map[string]int, error) {
	out := map[string]int{}
	err := s.guard(func() error {
		rows, err := s.db.Query(`SELECT to_task, COUNT(*) FROM say
			WHERE reply_wanted = 1 AND replied_at = '' AND lapsed = 0 AND to_task != '' GROUP BY to_task`)
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

// NoteContextReset stamps the says a card has received that the reset may have
// erased: delivered, and either owed a reply or delivered in the last ten minutes.
func (s *Store) NoteContextReset(taskID, kind string) error {
	n := now()
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE say SET reset_at = ?, reset_kind = ?
			WHERE to_task = ? AND state = ? AND reset_at = ''
			AND ((reply_wanted = 1 AND replied_at = '' AND lapsed = 0) OR delivered_at >= ?)`,
			ts(n), kind, taskID, SayDelivered, ts(n.Add(-10*time.Minute)))
		return err
	})
}

// LapseSaysFor gives up on replies owed to a card that has ended.
func (s *Store) LapseSaysFor(taskID string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE say SET lapsed = 1 WHERE to_task = ? AND reply_wanted = 1
			AND replied_at = '' AND lapsed = 0`, taskID)
		return err
	})
}

// SweepSays lapses old owed replies and deletes old rows, then trims to the cap.
// An open owed reply is never deleted by the cap.
func (s *Store) SweepSays() (int, error) {
	n := 0
	err := s.guard(func() error {
		n = 0
		t := now()
		if _, err := s.db.Exec(`UPDATE say SET lapsed = 1 WHERE reply_wanted = 1 AND replied_at = ''
			AND lapsed = 0 AND sent_at < ?`, ts(t.Add(-SayOwedFor))); err != nil {
			return err
		}
		res, err := s.db.Exec(`DELETE FROM say WHERE sent_at < ?`, ts(t.Add(-SayKeep)))
		if err != nil {
			return err
		}
		got, _ := res.RowsAffected()
		n += int(got)
		res, err = s.db.Exec(`DELETE FROM say WHERE id NOT IN
			(SELECT id FROM say ORDER BY sent_at DESC, id DESC LIMIT ?)
			AND NOT (reply_wanted = 1 AND replied_at = '' AND lapsed = 0)`, SayKeepMax)
		if err != nil {
			return err
		}
		got, _ = res.RowsAffected()
		n += int(got)
		return nil
	})
	return n, err
}

// RepliesOwedFor is RepliesOwed for ONE card, for the event that publishes it.
func (s *Store) RepliesOwedFor(taskID string) (int, error) {
	n := 0
	err := s.guard(func() error {
		return s.db.QueryRow(`SELECT COUNT(*) FROM say
			WHERE reply_wanted = 1 AND replied_at = '' AND lapsed = 0 AND to_task = ?`, taskID).Scan(&n)
	})
	return n, err
}
