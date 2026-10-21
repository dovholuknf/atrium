package store

import (
	"database/sql"
	"errors"
)

// Finding states. Waiting is the one the human is looking at, queued is behind it.
const (
	FindingWaiting   = "waiting"
	FindingQueued    = "queued"
	FindingApplied   = "applied"
	FindingKept      = "kept"
	FindingWithdrawn = "withdrawn"
	FindingVetoed    = "vetoed"
)

// ErrNoFinding is the answer for a finding number the card has not got.
var ErrNoFinding = errors.New("no such finding")

// ErrNoReview is the answer for a card that holds no review.
var ErrNoReview = errors.New("this card has no review")

// CardReview is the review a card holds: the base it measures against and the merge-base that base resolved to.
type CardReview struct {
	Card      string `json:"card"`
	Base      string `json:"base"`
	MergeBase string `json:"merge_base"`
	Files     string `json:"files"`
	CreatedAt string `json:"created_at"`
}

// Finding is one change site of a review. Edits is the JSON of the stored `{path, old, new}` list.
type Finding struct {
	Card      string `json:"card"`
	Num       int    `json:"num"`
	Cat       string `json:"cat"`
	Kind      string `json:"kind"`
	Origin    string `json:"origin"`
	Site      string `json:"site"`
	Title     string `json:"title"`
	Edits     string `json:"edits"`
	Note      string `json:"note"`
	Footer    string `json:"footer"`
	Diff      string `json:"diff"`
	BaseDiff  string `json:"base_diff"`
	Lines     int    `json:"lines_vs_base"`
	Grp       string `json:"group"`
	State     string `json:"state"`
	Reason    string `json:"reason"`
	CreatedAt string `json:"created_at"`
}

const findingCols = `card, num, cat, kind, origin, site, title, edits, note, footer, diff, base_diff, lines, grp, state, reason, created_at`

func scanFinding(sc interface{ Scan(...any) error }, f *Finding) error {
	return sc.Scan(&f.Card, &f.Num, &f.Cat, &f.Kind, &f.Origin, &f.Site, &f.Title, &f.Edits, &f.Note, &f.Footer,
		&f.Diff, &f.BaseDiff, &f.Lines, &f.Grp, &f.State, &f.Reason, &f.CreatedAt)
}

// ReviewStart records the review a card holds, replacing the base of one already there and keeping its findings.
func (s *Store) ReviewStart(card, base, mergeBase, files string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`INSERT INTO card_review (card, base, merge_base, files, created_at) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(card) DO UPDATE SET base = excluded.base, merge_base = excluded.merge_base, files = excluded.files`,
			card, base, mergeBase, files, ts(s.now()))
		return err
	})
}

// ReviewOf is the review a card holds. ErrNoReview when it holds none.
func (s *Store) ReviewOf(card string) (CardReview, error) {
	var r CardReview
	err := s.guard(func() error {
		err := s.db.QueryRow(`SELECT card, base, merge_base, files, created_at FROM card_review WHERE card = ?`, card).
			Scan(&r.Card, &r.Base, &r.MergeBase, &r.Files, &r.CreatedAt)
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNoReview
	}
	return r, err
}

// FindingAdd stores a finding under the next number of its card and answers that number. The first finding of a card
// with none waiting is waiting, any other is queued, unless the caller gave a state (a batch item is applied).
func (s *Store) FindingAdd(f Finding) (int, error) {
	num := 0
	err := s.guard(func() error {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err := tx.QueryRow(`SELECT COALESCE(MAX(num), 0) + 1 FROM card_finding WHERE card = ?`, f.Card).Scan(&num); err != nil {
			return err
		}
		if f.State == "" {
			var waiting int
			if err := tx.QueryRow(`SELECT COUNT(*) FROM card_finding WHERE card = ? AND state = ?`, f.Card, FindingWaiting).
				Scan(&waiting); err != nil {
				return err
			}
			f.State = FindingQueued
			if waiting == 0 {
				f.State = FindingWaiting
			}
		}
		_, err = tx.Exec(`INSERT INTO card_finding (`+findingCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			f.Card, num, f.Cat, f.Kind, f.Origin, f.Site, f.Title, f.Edits, f.Note, f.Footer, f.Diff, f.BaseDiff, f.Lines,
			f.Grp, f.State, f.Reason, ts(s.now()))
		if err != nil {
			return err
		}
		return tx.Commit()
	})
	return num, err
}

// FindingGet is one finding of a card by its number. ErrNoReview when there is none.
func (s *Store) FindingGet(card string, num int) (Finding, error) {
	var f Finding
	err := s.guard(func() error {
		err := scanFinding(s.db.QueryRow(`SELECT `+findingCols+` FROM card_finding WHERE card = ? AND num = ?`, card, num), &f)
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return f, ErrNoFinding
	}
	return f, err
}

// Findings is every finding of a card, in the order they were made.
func (s *Store) Findings(card string) ([]Finding, error) {
	var out []Finding
	err := s.guard(func() error {
		rows, err := s.db.Query(`SELECT `+findingCols+` FROM card_finding WHERE card = ? ORDER BY num`, card)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var f Finding
			if err := scanFinding(rows, &f); err != nil {
				return err
			}
			out = append(out, f)
		}
		return rows.Err()
	})
	return out, err
}

// FindingSetState moves a finding to a state with the reason for it. When the finding was the waiting one, the lowest
// queued finding of the card takes its place.
func (s *Store) FindingSetState(card string, num int, state, reason string) error {
	return s.guard(func() error {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err := tx.Exec(`UPDATE card_finding SET state = ?, reason = ? WHERE card = ? AND num = ?`,
			state, reason, card, num); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE card_finding SET state = ? WHERE card = ? AND num = (
				SELECT MIN(num) FROM card_finding WHERE card = ? AND state = ?)
			AND NOT EXISTS (SELECT 1 FROM card_finding WHERE card = ? AND state = ?)`,
			FindingWaiting, card, card, FindingQueued, card, FindingWaiting); err != nil {
			return err
		}
		return tx.Commit()
	})
}
