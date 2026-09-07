package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// A session saying what it needs, and who it asked.
//
// The mirror of `recap.go`. That records what a session did when its work is
// over; this records what it cannot get past while the work is still going.
// Both are written by the only party that knows, and both are bounded on the
// way in for the same reason: a card is a thing somebody reads at a glance.
//
// WHY THIS IS NOT `Why`. An ask landed there first, because `why` was already
// drawn under the title and putting it anywhere else meant touching the board.
// The two facts do not belong together. `Why` is what this card is FOR,
// written once and read in a week. An ask is a question outstanding this
// minute. Sharing one field meant an ask overwrote the standing intent with
// something stale by lunchtime, with no way to get it back, and the board drew
// whichever it was in the same italic line.
//
// WHY THIS IS NOT THREE COLUMNS EITHER, WHICH IS THE SAME BUG ONE LEVEL DOWN.
// Giving the ask its own `ask`, `ask_at` and `ask_peer` fixed the collision
// with `why` and left a worse one behind. One column holds one value, and
// questions arrive one at a time:
//
//	atrium ask --continue "which of these two schemas is authoritative"
//	atrium ask "which branch is base"
//
// The second UPDATE overwrote the first, and nothing anywhere recorded that
// the first had ever been asked. A session that asks two things got one
// answered and never learned the other was dropped. A shape that can only ever
// hold one thing cannot hold a series, so an ask is a row now, keyed by card,
// the way a message and an event already are.
//
// THE OLD COLUMNS STAY, AND ARE A MIRROR. The board reads `t.ask` and
// `t.ask_peer` today, and so do `fleet.go` and `atrium peers`. They are kept
// populated with the OLDEST OUTSTANDING ask, and NOTHING outside this file
// writes them, so the two cannot disagree: the table is authoritative and
// `mirrorAsk` recomputes the columns after every change. Oldest rather than
// newest because it makes the card a queue that drains: answer what is drawn,
// the next question appears, and the one that has waited longest is never the
// one hidden behind a fresher question.

// MaxAsk bounds what a session may say it needs.
//
// A sentence or two. This is a question, not a transcript: an agent that needs
// to explain at length has somewhere to do that, which is its own terminal,
// and this is going on a card.
const MaxAsk = 500

// MaxOpenAsks bounds how many questions one card may have outstanding.
//
// `MaxAsk` bounds one question. Nothing bounded the SERIES, and a model in a
// loop asks the same thing thirty times a minute, which turns the card into a
// wall and buries the question a human was actually going to answer.
//
// Ten, because a card is a thing somebody reads at a glance and a list longer
// than that is not read at all.
const MaxOpenAsks = 10

// askDropped is what an ask retired by the cap says happened to it.
//
// WHY THE OLDEST GOES AND NOT THE NEWEST. The newest question is the one the
// session is stopped on right now, so refusing it would leave a session stuck
// on something nobody can see, which is the exact failure this whole file is
// about. And the drop is RECORDED, on the row and in the event log, because
// the original defect was not that a question was lost: it was that a question
// was lost silently.
const askDropped = "dropped: too many questions outstanding on this card"

// An Ask is one question a session put to somebody, and whether it was
// answered.
type Ask struct {
	ID     string `json:"id"`
	TaskID string `json:"task_id"`
	Text   string `json:"text"`
	// Peer is the session this was routed to, or empty when it is on the board
	// for a human.
	//
	// PER QUESTION, which is why answering has to be per question too. One
	// card can be waiting on a peer for one thing and on you for another, and
	// a single answer that settled both would be a lie about one of them.
	Peer       string     `json:"peer,omitempty"`
	AskedAt    time.Time  `json:"asked_at"`
	AnsweredAt *time.Time `json:"answered_at,omitempty"`
	// AnsweredBy is who settled it: a peer's wire name, "the operator", "the
	// work finished", or the cap saying it threw this one away.
	AnsweredBy string `json:"answered_by,omitempty"`
}

// Outstanding reports whether anybody still owes this question an answer.
func (a *Ask) Outstanding() bool { return a.AnsweredAt == nil }

// AddAsk records a question, and returns the row it wrote.
//
// Truncated rather than refused, the same rule SetRecap follows: a session
// that wrote too much still wrote something worth keeping, and refusing makes
// an agent retry, and the retry is longer.
//
// An empty peer means the question is on the board for a human, which is what
// it has always meant.
func (s *Store) AddAsk(taskID, text, peer string) (*Ask, error) {
	text = truncateAsk(strings.TrimSpace(text))
	if text == "" {
		// Nothing to record. Refusing here would be the store deciding what an
		// endpoint should have validated, and `handleHelp` already does.
		return nil, fmt.Errorf("an ask needs some text")
	}
	a := &Ask{
		ID: newID(), TaskID: taskID, Text: text,
		Peer: strings.TrimSpace(peer), AskedAt: now(),
	}
	err := s.guard(func() error {
		if err := s.enforceAskCap(taskID); err != nil {
			return err
		}
		if _, err := s.db.Exec(
			`INSERT INTO ask (id, task_id, text, peer, asked_at, answered_at, answered_by)
			 VALUES (?,?,?,?,?,'','')`,
			a.ID, a.TaskID, a.Text, a.Peer, ts(a.AskedAt)); err != nil {
			return err
		}
		if _, err := s.db.Exec(
			`UPDATE task SET last_activity_at = ? WHERE id = ?`, ts(now()), taskID); err != nil {
			return err
		}
		return s.mirrorAsk(taskID)
	})
	if err != nil {
		return nil, err
	}
	return a, nil
}

// enforceAskCap retires the oldest outstanding asks until there is room for
// one more. Runs inside AddAsk's guard, so it uses the connection directly.
func (s *Store) enforceAskCap(taskID string) error {
	open, err := s.readAsks(taskID, true)
	if err != nil {
		return err
	}
	for i := 0; i <= len(open)-MaxOpenAsks; i++ {
		if _, err := s.db.Exec(
			`UPDATE ask SET answered_at = ?, answered_by = ? WHERE id = ?`,
			ts(now()), askDropped, open[i].ID); err != nil {
			return err
		}
		if err := s.appendEvent(taskID, EventSubmitted, map[string]any{
			"kind": "ask-dropped", "ask": open[i].Text, "peer": open[i].Peer,
			"why": askDropped,
		}); err != nil {
			return err
		}
	}
	return nil
}

// OpenAsks returns the questions nobody has answered, oldest first.
//
// Oldest first because that is the order they should be answered in, and
// because the first element is what the mirrored columns hold.
func (s *Store) OpenAsks(taskID string) ([]*Ask, error) {
	var out []*Ask
	err := s.guard(func() error {
		var err error
		out, err = s.readAsks(taskID, true)
		return err
	})
	return out, err
}

// AllAsks returns every question this card ever asked, oldest first, answered
// ones included. What the board needs to show a history rather than a state.
func (s *Store) AllAsks(taskID string) ([]*Ask, error) {
	var out []*Ask
	err := s.guard(func() error {
		var err error
		out, err = s.readAsks(taskID, false)
		return err
	})
	return out, err
}

func (s *Store) readAsks(taskID string, openOnly bool) ([]*Ask, error) {
	q := `SELECT id, task_id, text, peer, asked_at, answered_at, answered_by
	      FROM ask WHERE task_id = ?`
	if openOnly {
		q += ` AND answered_at = ''`
	}
	// Ordered by id as well as time, because `now()` is truncated to the
	// millisecond and two asks in the same tick would otherwise come back in
	// whatever order sqlite felt like. The ids are UUIDv7, so they order the
	// same way the clock does.
	q += ` ORDER BY asked_at ASC, id ASC`
	rows, err := s.db.Query(q, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Ask
	for rows.Next() {
		var (
			a               Ask
			asked, answered string
		)
		if err := rows.Scan(&a.ID, &a.TaskID, &a.Text, &a.Peer, &asked, &answered,
			&a.AnsweredBy); err != nil {
			return nil, err
		}
		if a.AskedAt, err = parseTS(asked); err != nil {
			return nil, fmt.Errorf("ask %s asked_at: %w", a.ID, err)
		}
		if answered != "" {
			at, err := parseTS(answered)
			if err != nil {
				return nil, fmt.Errorf("ask %s answered_at: %w", a.ID, err)
			}
			a.AnsweredAt = &at
		}
		out = append(out, &a)
	}
	return out, rows.Err()
}

// OpenAskCounts returns how many questions are outstanding per card, so the
// board can draw a count without a query per card. Shaped like
// UndeliveredCounts, which answers the same question for messages.
func (s *Store) OpenAskCounts() (map[string]int, error) {
	out := map[string]int{}
	err := s.guard(func() error {
		rows, err := s.db.Query(
			`SELECT task_id, COUNT(*) FROM ask WHERE answered_at = '' GROUP BY task_id`)
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

// AnswerAsk settles ONE question by id, and reports whether it was outstanding.
//
// The narrow door, and the one the board wants: a card showing three questions
// needs a way to answer the second one without touching the other two. Every
// broader path below is written in terms of the same UPDATE.
func (s *Store) AnswerAsk(askID, by string) (bool, error) {
	var n int64
	err := s.guard(func() error {
		var taskID string
		if err := s.db.QueryRow(
			`SELECT task_id FROM ask WHERE id = ?`, askID).Scan(&taskID); err != nil {
			if err == sql.ErrNoRows {
				n = 0
				return nil
			}
			return err
		}
		res, err := s.db.Exec(
			`UPDATE ask SET answered_at = ?, answered_by = ? WHERE id = ? AND answered_at = ''`,
			ts(now()), by, askID)
		if err != nil {
			return err
		}
		if n, err = res.RowsAffected(); err != nil {
			return err
		}
		return s.mirrorAsk(taskID)
	})
	return n > 0, err
}

// AnswerAsksFrom settles the questions a card routed to ONE named peer, and
// returns what it settled.
//
// The honest thing to do when a peer answers. `ask_peer` is per question, so a
// peer can only ever have settled the questions addressed to it: clearing a
// card wholesale on the strength of one peer replying would take a question
// for a HUMAN off the board, which is the same disappearance this file exists
// to stop, wearing different clothes.
func (s *Store) AnswerAsksFrom(taskID, peer, by string) ([]*Ask, error) {
	return s.answerMatching(taskID, by, func(a *Ask) bool { return a.Peer == peer })
}

// AnswerAllAsks settles everything outstanding on a card.
//
// The broad door, for the two events that genuinely end every question at
// once: the operator saying something to the card, and the session declaring
// its work over. Neither is addressed to a particular question and neither
// leaves anybody owing an answer.
func (s *Store) AnswerAllAsks(taskID, by string) ([]*Ask, error) {
	return s.answerMatching(taskID, by, func(*Ask) bool { return true })
}

func (s *Store) answerMatching(taskID, by string, match func(*Ask) bool) ([]*Ask, error) {
	var settled []*Ask
	err := s.guard(func() error {
		settled = nil
		open, err := s.readAsks(taskID, true)
		if err != nil {
			return err
		}
		at := ts(now())
		for _, a := range open {
			if !match(a) {
				continue
			}
			if _, err := s.db.Exec(
				`UPDATE ask SET answered_at = ?, answered_by = ? WHERE id = ?`,
				at, by, a.ID); err != nil {
				return err
			}
			settled = append(settled, a)
		}
		if len(settled) == 0 {
			return nil
		}
		return s.mirrorAsk(taskID)
	})
	return settled, err
}

// mirrorAsk rewrites the `ask`, `ask_at` and `ask_peer` columns from the
// table. Called inside a guard by everything that changes an ask.
//
// THIS IS THE ONLY WRITER OF THOSE COLUMNS. That is what stops the row and the
// columns disagreeing, which would be worse than the defect they were part of:
// a board drawing a question the table says was answered an hour ago.
func (s *Store) mirrorAsk(taskID string) error {
	var text, at, peer string
	err := s.db.QueryRow(
		`SELECT text, asked_at, peer FROM ask
		 WHERE task_id = ? AND answered_at = '' ORDER BY asked_at ASC, id ASC LIMIT 1`,
		taskID).Scan(&text, &at, &peer)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == sql.ErrNoRows {
		// Nothing outstanding means nobody owes it an answer, so the peer goes
		// too. Leaving it behind is how a card ends up naming a session that
		// answered it an hour ago.
		text, at, peer = "", "", ""
	}
	_, err = s.db.Exec(
		`UPDATE task SET ask = ?, ask_at = ?, ask_peer = ? WHERE id = ?`,
		text, at, peer, taskID)
	return err
}

// truncateAsk cuts on a rune boundary, so truncating never leaves invalid
// UTF-8 in a field the board renders.
func truncateAsk(ask string) string {
	if len(ask) <= MaxAsk {
		return ask
	}
	cut := MaxAsk
	for cut > 0 && !utf8Start(ask[cut]) {
		cut--
	}
	return strings.TrimSpace(ask[:cut]) + "..."
}

// Asking reports whether this card has a question nobody has answered.
func (t *Task) Asking() bool { return strings.TrimSpace(t.Ask) != "" }

// AskedAPeer reports whether the OLDEST outstanding question went to another
// session rather than onto the board for a human.
//
// Reads the mirror, so it answers for the question the card is drawing. A card
// with a peer question behind a human one reads as waiting on you, which is
// true: you are the one it is waiting on first.
func (t *Task) AskedAPeer() bool { return t.Asking() && t.AskPeer != "" }
