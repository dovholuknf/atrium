package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Whether the operator has seen a card's latest turn, and whether that turn
// asked the operator questions they have not answered. See docs/seen-design.md.
//
// DURABLE, UNLIKE ACTIVITY. What a runner is doing right now dies with the
// daemon because it would be a lie after a restart. Whether a turn was read is
// the other kind of fact: it was true when it was written and stays true, and a
// board that forgot it on a restart would mark every card unread or none.
//
// A TURN FACT, NOT A TRANSCRIPT. The questions are the numbered lines of one
// `Open Questions:` block, bounded on the way in by the same caps an ask has.
// Nothing else the session said is stored here or anywhere.

// The ways a turn comes to be seen. Stored as text so the board and an agent
// can say how, which is the difference between "they read it" and "they typed at
// it".
const (
	SeenViewed  = "viewed"
	SeenTyped   = "typed"
	SeenPrompt  = "prompt"
	SeenMessage = "message"
)

// MaxTurnQuestions bounds how many questions one turn may leave on a card, and
// MaxAsk bounds each one. Ten for the reason MaxOpenAsks is ten.
const MaxTurnQuestions = MaxOpenAsks

// Seen is one card's row.
type Seen struct {
	TaskID      string
	TurnEndedAt *time.Time
	SeenAt      *time.Time
	SeenVia     string
	Questions   []string
	// Unparsed is a turn that had an Open Questions heading and no item this
	// could read. The questions exist and their text does not.
	Unparsed    bool
	QuestionsAt *time.Time
	AnsweredAt  *time.Time
	AnsweredVia string
}

// Unseen reports whether the latest turn has ended and not been seen since.
func (s *Seen) Unseen() bool {
	if s == nil || s.TurnEndedAt == nil {
		return false
	}
	return s.SeenAt == nil || s.SeenAt.Before(*s.TurnEndedAt)
}

// Asked reports whether any turn on this card ever left questions.
func (s *Seen) Asked() bool {
	return s != nil && s.QuestionsAt != nil && (len(s.Questions) > 0 || s.Unparsed)
}

// Answered reports whether the operator replied after the questions were
// asked. The whole set at once: atrium cannot tell which question a reply
// addressed.
func (s *Seen) Answered() bool {
	if !s.Asked() {
		return false
	}
	return s.AnsweredAt != nil && !s.AnsweredAt.Before(*s.QuestionsAt)
}

// SeenView is the shape a card view and an agent read.
type SeenView struct {
	TurnEndedAt *time.Time `json:"turn_ended_at,omitempty"`
	SeenAt      *time.Time `json:"seen_at,omitempty"`
	SeenVia     string     `json:"seen_via,omitempty"`
	Unseen      bool       `json:"unseen"`
	// OpenQuestions is absent once answered, so a reader never has to compare
	// two timestamps to know whether a list is still owed.
	OpenQuestions   []string   `json:"open_questions,omitempty"`
	QuestionsUnparsed bool       `json:"questions_unparsed,omitempty"`
	QuestionsAt     *time.Time `json:"questions_at,omitempty"`
	AnsweredAt      *time.Time `json:"answered_at,omitempty"`
	AnsweredVia     string     `json:"answered_via,omitempty"`
	// Answered is absent when no turn ever asked anything, because false there
	// would read as a question still owed.
	Answered *bool `json:"answered,omitempty"`
}

// View shapes a row for a reader. Nil in, nil out.
func (s *Seen) View() *SeenView {
	if s == nil {
		return nil
	}
	v := &SeenView{
		TurnEndedAt: s.TurnEndedAt, SeenAt: s.SeenAt, SeenVia: s.SeenVia,
		Unseen: s.Unseen(), QuestionsAt: s.QuestionsAt,
		AnsweredAt: s.AnsweredAt, AnsweredVia: s.AnsweredVia,
	}
	if s.Asked() {
		answered := s.Answered()
		v.Answered = &answered
		if !answered {
			v.OpenQuestions = s.Questions
			v.QuestionsUnparsed = s.Unparsed
		}
	}
	return v
}

// OpenCount is how many questions are still owed. Minus one means some are
// and their text could not be read.
func (v *SeenView) OpenCount() int {
	if v == nil || v.Answered == nil || *v.Answered {
		return 0
	}
	if len(v.OpenQuestions) == 0 && v.QuestionsUnparsed {
		return -1
	}
	return len(v.OpenQuestions)
}

// TurnQuestions is what the Stop hook read off the turn's last message.
type TurnQuestions struct {
	// Known is whether the hook could read the message at all. When it could
	// not, the card keeps whatever questions it already had.
	Known bool
	// Block is whether the message had an Open Questions heading.
	Block bool
	List  []string
}

const seenColumns = `task_id, turn_ended_at, seen_at, seen_via, questions, questions_unparsed,
	questions_at, answered_at, answered_via`

func scanSeen(sc interface{ Scan(...any) error }) (*Seen, error) {
	var (
		s                                   Seen
		ended, seen, qs, qat, ans           string
		unparsed                            int
	)
	if err := sc.Scan(&s.TaskID, &ended, &seen, &s.SeenVia, &qs, &unparsed,
		&qat, &ans, &s.AnsweredVia); err != nil {
		return nil, err
	}
	s.Unparsed = unparsed != 0
	for _, f := range []struct {
		raw string
		dst **time.Time
	}{{ended, &s.TurnEndedAt}, {seen, &s.SeenAt}, {qat, &s.QuestionsAt}, {ans, &s.AnsweredAt}} {
		if f.raw == "" {
			continue
		}
		t, err := parseTS(f.raw)
		if err != nil {
			return nil, fmt.Errorf("turn_seen %s: %w", s.TaskID, err)
		}
		*f.dst = &t
	}
	s.Questions = []string{}
	if qs != "" {
		if err := json.Unmarshal([]byte(qs), &s.Questions); err != nil {
			return nil, fmt.Errorf("turn_seen %s questions: %w", s.TaskID, err)
		}
	}
	return &s, nil
}

// GetSeen returns a card's row, or nil when no turn has ever ended on it.
func (s *Store) GetSeen(taskID string) (*Seen, error) {
	var out *Seen
	err := s.guard(func() error {
		row := s.db.QueryRow(`SELECT `+seenColumns+` FROM turn_seen WHERE task_id = ?`, taskID)
		got, err := scanSeen(row)
		if errors.Is(err, sql.ErrNoRows) {
			out = nil
			return nil
		}
		out = got
		return err
	})
	return out, err
}

// SeenAll returns every card's row, keyed by card, in one query. The board
// draws the whole list on every event, so this is read the way OpenAskCounts
// is and never per card.
func (s *Store) SeenAll() (map[string]*Seen, error) {
	out := map[string]*Seen{}
	err := s.guard(func() error {
		out = map[string]*Seen{}
		rows, err := s.db.Query(`SELECT ` + seenColumns + ` FROM turn_seen`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			got, err := scanSeen(rows)
			if err != nil {
				return err
			}
			out[got.TaskID] = got
		}
		return rows.Err()
	})
	return out, err
}

// NoteTurnEnded records that a turn ended, and what it asked.
//
// A turn with a block REPLACES the card's questions, because an agent that
// re-surfaces its questions restates them. A turn with no block, or one whose
// text could not be read, KEEPS them: a turn that ran because a peer reported
// in has not answered what the operator was asked.
func (s *Store) NoteTurnEnded(taskID string, q TurnQuestions) error {
	at := ts(now())
	return s.guard(func() error {
		if err := s.ensureSeen(taskID); err != nil {
			return err
		}
		if _, err := s.db.Exec(`UPDATE turn_seen SET turn_ended_at = ? WHERE task_id = ?`,
			at, taskID); err != nil {
			return err
		}
		if !q.Known || !q.Block {
			return nil
		}
		list := boundQuestions(q.List)
		raw, err := json.Marshal(list)
		if err != nil {
			return err
		}
		unparsed := 0
		if len(list) == 0 {
			unparsed = 1
		}
		_, err = s.db.Exec(`UPDATE turn_seen SET questions = ?, questions_unparsed = ?, questions_at = ?
			WHERE task_id = ?`, string(raw), unparsed, at, taskID)
		return err
	})
}

// MarkSeen records that the operator saw the latest turn, and reports whether
// that changed anything.
//
// `shown` is the turn end the board was looking at. One older than the stored
// turn end is a board that has not heard about the newest turn yet, and marks
// nothing, because what it showed was not that turn. Nil is a caller that saw
// the terminal itself, which is always the newest turn.
func (s *Store) MarkSeen(taskID, via string, shown *time.Time) (bool, error) {
	changed := false
	err := s.guard(func() error {
		changed = false
		cur, err := s.seenRow(taskID)
		if err != nil || cur == nil || !cur.Unseen() {
			return err
		}
		if shown != nil && shown.Before(*cur.TurnEndedAt) {
			return nil
		}
		if _, err := s.db.Exec(`UPDATE turn_seen SET seen_at = ?, seen_via = ? WHERE task_id = ?`,
			ts(now()), via, taskID); err != nil {
			return err
		}
		changed = true
		return nil
	})
	return changed, err
}

// MarkAnswered records that the operator replied to the card, which answers
// its questions and sees its turn. Reports whether that changed anything.
//
// Never creates a row. A card no turn has ended on has nothing to answer.
func (s *Store) MarkAnswered(taskID, via string) (bool, error) {
	changed := false
	err := s.guard(func() error {
		changed = false
		cur, err := s.seenRow(taskID)
		if err != nil || cur == nil {
			return err
		}
		at := ts(now())
		if cur.Asked() && !cur.Answered() {
			if _, err := s.db.Exec(`UPDATE turn_seen SET answered_at = ?, answered_via = ? WHERE task_id = ?`,
				at, via, taskID); err != nil {
				return err
			}
			changed = true
		}
		if cur.Unseen() {
			if _, err := s.db.Exec(`UPDATE turn_seen SET seen_at = ?, seen_via = ? WHERE task_id = ?`,
				at, via, taskID); err != nil {
				return err
			}
			changed = true
		}
		return nil
	})
	return changed, err
}

// UnseenCards lists the cards whose latest turn is unseen. Read once at start,
// so the keystroke path can answer from memory. See daemon/seen.go.
func (s *Store) UnseenCards() ([]string, error) {
	all, err := s.SeenAll()
	if err != nil {
		return nil, err
	}
	var out []string
	for id, row := range all {
		if row.Unseen() {
			out = append(out, id)
		}
	}
	return out, nil
}

// seenRow reads a row inside a guard that is already held.
func (s *Store) seenRow(taskID string) (*Seen, error) {
	row := s.db.QueryRow(`SELECT `+seenColumns+` FROM turn_seen WHERE task_id = ?`, taskID)
	got, err := scanSeen(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return got, err
}

// ensureSeen creates a card's row if it has none. The NOT EXISTS guard makes a
// second call a no-op without `INSERT OR IGNORE`, which Postgres does not have.
func (s *Store) ensureSeen(taskID string) error {
	_, err := s.db.Exec(`INSERT INTO turn_seen (task_id)
		SELECT ? WHERE NOT EXISTS (SELECT 1 FROM turn_seen WHERE task_id = ?)`, taskID, taskID)
	return err
}

// boundQuestions trims, drops empties, and applies both caps.
func boundQuestions(in []string) []string {
	out := []string{}
	for _, q := range in {
		q = truncateAsk(strings.TrimSpace(q))
		if q == "" {
			continue
		}
		out = append(out, q)
		if len(out) == MaxTurnQuestions {
			break
		}
	}
	return out
}
