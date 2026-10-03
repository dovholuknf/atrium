package hubstore

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// Change requests between rooms, the rows half. See migration 0009 for the rules of the table, and
// internal/link/changerequest.go for who may do what: this file keeps the rows and enforces the rules that hold
// whoever calls it.
//
//   - A REQUEST ENDS ONCE. open becomes merged, closed or withdrawn, and a request that is not open answers
//     ErrCRFinal with the row as it is, which is how a click on an old screen learns another screen got there first.
//   - ONE OPEN REQUEST PER SOURCE AND TARGET. Asking again while one is open hands that one back.
//   - NOTHING HERE READS A BRANCH OR A SHA. The caller says what the source was at, and what a merge reached.

// Change request states, as the CHECK constraint spells them.
const (
	CROpen      = "open"
	CRMerged    = "merged"
	CRClosed    = "closed"
	CRWithdrawn = "withdrawn"
)

// The bounds of a request's own words.
const (
	CRTitleMax = 200
	CRWhyMax   = 4000
	CRNoteMax  = 1000
)

// CROperator is the card a request carries when the operator did it.
const CROperator = "operator"

var (
	// ErrCRNotFound is an id the hub has never held.
	ErrCRNotFound = errors.New("no change request has that id")
	// ErrCRFinal is a change to a request that is no longer open. The row comes back with it.
	ErrCRFinal = errors.New("that change request is already finished")
)

// CRParty is a room and a card. A card of CROperator is the operator, and then the room is empty.
type CRParty struct {
	Room string `json:"room"`
	Card string `json:"card"`
}

// CRSource is where the work is. A Room of "" is a branch pushed to the hub.
type CRSource struct {
	Room   string `json:"room,omitempty"`
	Branch string `json:"branch"`
	SHA    string `json:"sha"`
}

// CRTarget is the branch the work is asked to go into.
type CRTarget struct {
	Branch string `json:"branch"`
}

// ChangeRequest is one row, as the API carries it.
type ChangeRequest struct {
	ID        string   `json:"id"`
	Repo      string   `json:"repo"`
	Source    CRSource `json:"source"`
	Target    CRTarget `json:"target"`
	Title     string   `json:"title"`
	Why       string   `json:"why"`
	Change    string   `json:"change"`
	State     string   `json:"state"`
	CreatedBy CRParty  `json:"created_by"`
	CreatedAt string   `json:"created_at"`
	ClosedAt  *string  `json:"closed_at"`
	ClosedBy  *CRParty `json:"closed_by"`
	Note      string   `json:"note"`
	MergedSHA *string  `json:"merged_sha"`
	Owner     CRParty  `json:"owner"`
}

// CRNew is a request to make. The owner is what the hub worked out for the source branch, and may be empty.
type CRNew struct {
	Repo                     string
	SourceRoom, SourceBranch string
	SourceSHA                string
	Target                   string
	Title, Why, Change       string
	CreatedBy                CRParty
	Owner                    CRParty
}

// CRFilter narrows a list. Empty fields match everything. State is one state, or "" for all of them, and Finished
// is every state but open (and takes the place of State). Room matches the source room OR the owner's room, without
// regard to case, as a room's name is anywhere else.
type CRFilter struct {
	State, Target, Repo string
	Finished            bool
	Room                string
}

// CRText says why a piece of a request's words is refused, or "". A control character is refused, and so are
// the line and paragraph separators and the bidi overrides, which a board would draw as something else than
// what was written. `lines` lets a newline and a tab through, for the why.
func CRText(what, s string, max int, lines bool) string {
	if len([]rune(s)) > max {
		return fmt.Sprintf("the %s is over %d characters", what, max)
	}
	for _, r := range s {
		switch {
		case lines && (r == '\n' || r == '\t'):
		case unicode.IsControl(r), r == ' ', r == ' ', r >= '‪' && r <= '‮', r >= '⁦' && r <= '⁩':
			return "the " + what + " has a control character in it"
		}
	}
	return ""
}

// Check says why a request cannot be made, or "".
func (n CRNew) Check() string {
	switch {
	case strings.TrimSpace(n.Title) == "":
		return "a change request needs a title"
	case n.Repo == "" || n.SourceBranch == "" || n.Target == "":
		return "a change request needs a repo, a source branch and a target branch"
	}
	if why := CRText("title", n.Title, CRTitleMax, false); why != "" {
		return why
	}
	if why := CRText("why", n.Why, CRWhyMax, true); why != "" {
		return why
	}
	return ""
}

const crCols = `id, repo, source_room, source_branch, source_sha, target_branch, title, why, change_id, state,
	created_room, created_card, created_at, closed_at, closed_room, closed_card, note, merged_sha, owner_room, owner_card`

type crScanner interface{ Scan(dest ...any) error }

func scanCR(sc crScanner) (ChangeRequest, error) {
	var c ChangeRequest
	var closedAt, closedRoom, closedCard, merged string
	err := sc.Scan(&c.ID, &c.Repo, &c.Source.Room, &c.Source.Branch, &c.Source.SHA, &c.Target.Branch, &c.Title, &c.Why,
		&c.Change, &c.State, &c.CreatedBy.Room, &c.CreatedBy.Card, &c.CreatedAt, &closedAt, &closedRoom, &closedCard,
		&c.Note, &merged, &c.Owner.Room, &c.Owner.Card)
	if err != nil {
		return c, err
	}
	if closedAt != "" {
		c.ClosedAt = &closedAt
		c.ClosedBy = &CRParty{Room: closedRoom, Card: closedCard}
	}
	if merged != "" {
		c.MergedSHA = &merged
	}
	return c, nil
}

// CRCreate makes a request, or answers the open one for the same source and target with existed true. The id is
// the table's highest n plus one, taken in the same transaction.
func (s *Store) CRCreate(in CRNew) (c ChangeRequest, existed bool, err error) {
	if why := in.Check(); why != "" {
		return c, false, refuse(errors.New(why))
	}
	// A ROOM NAME IS FOLDED, as it is everywhere else (fold, and keyOf in link): the lookup below and the unique index
	// both compare source_room as stored, so "Room-A" and "room-a" would otherwise be two open requests for one source.
	in.SourceRoom = fold(in.SourceRoom)
	at := ts(now())
	err = s.tx(func(t *sql.Tx) error {
		c, existed = ChangeRequest{}, false
		prior, err := scanCR(t.QueryRow(`SELECT `+crCols+` FROM change_request
			WHERE repo = ? AND source_room = ? AND source_branch = ? AND target_branch = ? AND state = 'open'`,
			in.Repo, in.SourceRoom, in.SourceBranch, in.Target))
		if err == nil {
			c, existed = prior, true
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var top int64
		if err := t.QueryRow(`SELECT COALESCE(MAX(n), 0) FROM change_request`).Scan(&top); err != nil {
			return err
		}
		n := top + 1
		_, err = t.Exec(`INSERT INTO change_request (id, n, repo, source_room, source_branch, source_sha, target_branch,
			title, why, change_id, created_room, created_card, created_at, owner_room, owner_card)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			fmt.Sprintf("cr_%d", n), n, in.Repo, in.SourceRoom, in.SourceBranch, in.SourceSHA, in.Target,
			in.Title, in.Why, in.Change, in.CreatedBy.Room, in.CreatedBy.Card, at, in.Owner.Room, in.Owner.Card)
		if err != nil {
			return err
		}
		c, err = scanCR(t.QueryRow(`SELECT `+crCols+` FROM change_request WHERE id = ?`, fmt.Sprintf("cr_%d", n)))
		return err
	})
	return c, existed, err
}

// CRGet is one request, or ErrCRNotFound.
func (s *Store) CRGet(id string) (ChangeRequest, error) {
	var c ChangeRequest
	err := s.guard(func() error {
		var err error
		c, err = scanCR(s.db.QueryRow(`SELECT `+crCols+` FROM change_request WHERE id = ?`, id))
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrCRNotFound
	}
	return c, err
}

// CRList is the requests that match, newest first.
func (s *Store) CRList(f CRFilter) ([]ChangeRequest, error) {
	out := []ChangeRequest{}
	err := s.guard(func() error {
		out = []ChangeRequest{}
		where, args := []string{"1 = 1"}, []any{}
		switch {
		case f.Finished:
			where = append(where, "state <> 'open'")
		case f.State != "":
			where, args = append(where, "state = ?"), append(args, f.State)
		}
		if f.Target != "" {
			where, args = append(where, "target_branch = ?"), append(args, f.Target)
		}
		if f.Repo != "" {
			where, args = append(where, "repo = ?"), append(args, f.Repo)
		}
		if f.Room != "" {
			where, args = append(where, "(lower(source_room) = lower(?) OR lower(owner_room) = lower(?))"), append(args, f.Room, f.Room)
		}
		rows, err := s.db.Query(`SELECT `+crCols+` FROM change_request WHERE `+strings.Join(where, " AND ")+
			` ORDER BY n DESC`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			c, err := scanCR(rows)
			if err != nil {
				return err
			}
			out = append(out, c)
		}
		return rows.Err()
	})
	return out, err
}

// CREnd ends an open request: to is CRMerged, CRClosed or CRWithdrawn, by is who did it, and sha is what a
// merge reached (empty otherwise). A request that is not open answers ErrCRFinal with the row as it is, and
// nothing is written.
func (s *Store) CREnd(id, to string, by CRParty, note, sha string) (ChangeRequest, error) {
	switch to {
	case CRMerged, CRClosed, CRWithdrawn:
	default:
		return ChangeRequest{}, errors.New("a change request ends as merged, closed or withdrawn")
	}
	if why := CRText("note", note, CRNoteMax, true); why != "" {
		return ChangeRequest{}, refuse(errors.New(why))
	}
	at := ts(now())
	var c ChangeRequest
	err := s.tx(func(t *sql.Tx) error {
		var err error
		c, err = scanCR(t.QueryRow(`SELECT `+crCols+` FROM change_request WHERE id = ?`, id))
		if errors.Is(err, sql.ErrNoRows) {
			return refuse(ErrCRNotFound)
		}
		if err != nil {
			return err
		}
		if c.State != CROpen {
			return refuse(ErrCRFinal)
		}
		if _, err := t.Exec(`UPDATE change_request SET state = ?, closed_at = ?, closed_room = ?, closed_card = ?,
			note = ?, merged_sha = ? WHERE id = ? AND state = 'open'`,
			to, at, by.Room, by.Card, note, sha, id); err != nil {
			return err
		}
		c, err = scanCR(t.QueryRow(`SELECT `+crCols+` FROM change_request WHERE id = ?`, id))
		return err
	})
	switch {
	case errors.Is(err, ErrCRNotFound):
		return ChangeRequest{}, ErrCRNotFound
	case errors.Is(err, ErrCRFinal):
		return c, ErrCRFinal
	}
	return c, err
}
