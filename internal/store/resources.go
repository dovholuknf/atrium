package store

import (
	"database/sql"
	"errors"
	"strings"
)

// A CARD'S INVENTORY. Design: docs/rnd/card-lifecycle-design.md section 6. Item r-card-inventory.
//
// One row per thing the room made for a card: a worktree, a branch, a fetched ref, a review row. A row is written
// BEFORE the thing is made when its name can be chosen up front, so a crash between the two leaves a row a sweep can
// check, never a thing nobody owns. Finish frees the rows in reverse (r-finish-pr-review).
//
// Rows made before the card exists (the open verb makes the worktree first) are held under a pending owner and handed
// to the card once it starts. See MoveResources.

// The kinds of resource. Each names how it is freed.
const (
	ResWorktree = "worktree" // ref: the path. freed by git worktree remove, then the directory
	ResBranch   = "branch"   // ref: the branch. detail: the repo path. freed by git branch -D, when finish is told to
	ResRef      = "ref"      // ref: the ref name. detail: the repo path. freed by git update-ref -d
	ResDir      = "dir"      // ref: a path under a root atrium owns
	ResReview   = "review"   // ref: the review row id. freed by archiving it
)

// CardResource is one row of a card's inventory.
type CardResource struct {
	Card     string `json:"card"`
	Seq      int    `json:"seq"`
	Kind     string `json:"kind"`
	Ref      string `json:"ref"`
	Detail   string `json:"detail,omitempty"`
	MadeAt   string `json:"made_at"`
	FreedAt  string `json:"freed_at,omitempty"`
	FreedErr string `json:"freed_err,omitempty"`
	// Bytes is the size last measured, -1 before any measure. Only a worktree or a dir has one.
	Bytes      int64  `json:"bytes"`
	MeasuredAt string `json:"measured_at,omitempty"`
}

// Live is whether the thing is still there as far as the inventory knows.
func (r *CardResource) Live() bool { return r.FreedAt == "" }

// AddResource records a resource of a card and answers the row. The seq is the card's next.
func (st *Store) AddResource(card, kind, ref, detail string) (*CardResource, error) {
	card, kind, ref = strings.TrimSpace(card), strings.TrimSpace(kind), strings.TrimSpace(ref)
	if card == "" || kind == "" || ref == "" {
		return nil, errors.New("a resource needs a card, a kind and a ref")
	}
	row := &CardResource{Card: card, Kind: kind, Ref: ref, Detail: detail, MadeAt: ts(now()), Bytes: -1}
	err := st.guard(func() error {
		tx, err := st.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err := tx.QueryRow(`SELECT COALESCE(MAX(seq), 0) + 1 FROM card_resources WHERE card = ?`, card).
			Scan(&row.Seq); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO card_resources (card, seq, kind, ref, detail, made_at) VALUES (?, ?, ?, ?, ?, ?)`,
			card, row.Seq, kind, ref, detail, row.MadeAt); err != nil {
			return err
		}
		return tx.Commit()
	})
	if err != nil {
		return nil, err
	}
	return row, nil
}

// Resources is a card's inventory in the order it was made, freed rows included.
func (st *Store) Resources(card string) ([]*CardResource, error) {
	var out []*CardResource
	err := st.guard(func() error {
		out = nil
		rows, err := st.db.Query(`SELECT card, seq, kind, ref, detail, made_at, freed_at, freed_err, bytes, measured_at
			FROM card_resources WHERE card = ? ORDER BY seq`, card)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r := &CardResource{}
			if err := rows.Scan(&r.Card, &r.Seq, &r.Kind, &r.Ref, &r.Detail, &r.MadeAt, &r.FreedAt, &r.FreedErr,
				&r.Bytes, &r.MeasuredAt); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}

// FreeResource marks a row freed, with what removing it said ("" when it went cleanly). A freed row stays, so the
// history says what the card held.
func (st *Store) FreeResource(card string, seq int, freedErr string) error {
	return st.guard(func() error {
		res, err := st.db.Exec(`UPDATE card_resources SET freed_at = ?, freed_err = ? WHERE card = ? AND seq = ?`,
			ts(now()), freedErr, card, seq)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return sql.ErrNoRows
		}
		return nil
	})
}

// MoveResources hands every row of one owner to another, renumbered after the new owner's own. The open verb holds
// what it makes under a pending owner until the card exists.
func (st *Store) MoveResources(from, to string) error {
	if strings.TrimSpace(from) == "" || strings.TrimSpace(to) == "" || from == to {
		return errors.New("moving resources needs two different owners")
	}
	return st.guard(func() error {
		tx, err := st.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		var base int
		if err := tx.QueryRow(`SELECT COALESCE(MAX(seq), 0) FROM card_resources WHERE card = ?`, to).Scan(&base); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE card_resources SET card = ?, seq = seq + ? WHERE card = ?`, to, base, from); err != nil {
			return err
		}
		return tx.Commit()
	})
}

// SetResourceBytes records a measured size.
func (st *Store) SetResourceBytes(card string, seq int, bytes int64) error {
	return st.guard(func() error {
		_, err := st.db.Exec(`UPDATE card_resources SET bytes = ?, measured_at = ? WHERE card = ? AND seq = ?`,
			bytes, ts(now()), card, seq)
		return err
	})
}

// CardDisk is the measured bytes of every card's live resources, by card. A card with nothing measured is absent.
func (st *Store) CardDisk() (map[string]int64, error) {
	out := map[string]int64{}
	err := st.guard(func() error {
		clear(out)
		rows, err := st.db.Query(`SELECT card, SUM(bytes) FROM card_resources
			WHERE freed_at = '' AND bytes >= 0 GROUP BY card`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var card string
			var n int64
			if err := rows.Scan(&card, &n); err != nil {
				return err
			}
			out[card] = n
		}
		return rows.Err()
	})
	return out, err
}
