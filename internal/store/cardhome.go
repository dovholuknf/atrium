package store

import (
	"database/sql"
	"encoding/json"
)

// THE FOLDERS A CARD WORKS IN, one ledger row each, so a folder is removed only by what made it. See
// docs/runtime/card-home-design.md.

const (
	CardHomeLive    = "live"
	CardHomeKept    = "kept"
	CardHomeRemoved = "removed"

	CardHomeWorktree  = "worktree"
	CardHomeScratch   = "scratch"
	CardHomeTask      = "task"
	CardHomeThrowaway = "throwaway"
)

// CardHome is one folder a card works in. Owed is the event the hub has not acknowledged, `reaped` once the folder is
// gone. A task's folder carries the backlog, task and attempt, and a worktree's the clone, branch and base.
type CardHome struct {
	ID         string   `json:"id"`
	Path       string   `json:"path"`
	Kind       string   `json:"kind"`
	BacklogID  string   `json:"backlog_id"`
	TaskID     string   `json:"task_id"`
	Attempt    int      `json:"attempt"`
	Card       string   `json:"card"`
	Clone      string   `json:"clone"`
	Branch     string   `json:"branch"`
	Base       string   `json:"base"`
	CreatedAt  string   `json:"created_at"`
	State      string   `json:"state"`
	Reason     string   `json:"reason"`
	Owed       string   `json:"owed"`
	Outputs    []string `json:"outputs"`
	Bytes      int64    `json:"bytes"`
	Files      int64    `json:"files"`
	MeasuredAt string   `json:"measured_at"`
	RemovedAt  string   `json:"removed_at"`
}

const cardHomeCols = `id, path, kind, backlog_id, task_id, attempt, card, clone, branch, base, created_at, state, reason, owed, outputs, bytes, files, measured_at, removed_at`

func scanCardHome(sc interface{ Scan(...any) error }) (CardHome, error) {
	var d CardHome
	var outputs string
	err := sc.Scan(&d.ID, &d.Path, &d.Kind, &d.BacklogID, &d.TaskID, &d.Attempt, &d.Card, &d.Clone, &d.Branch, &d.Base,
		&d.CreatedAt, &d.State, &d.Reason, &d.Owed, &outputs, &d.Bytes, &d.Files, &d.MeasuredAt, &d.RemovedAt)
	_ = json.Unmarshal([]byte(outputs), &d.Outputs)
	return d, err
}

// CardHomeAdd records a task's folder before it is made, live.
func (s *Store) CardHomeAdd(path, backlogID, taskID string, attempt int, outputs []string) error {
	raw, _ := json.Marshal(outputs)
	return s.guard(func() error {
		_, err := s.db.Exec(`INSERT OR IGNORE INTO card_home (id, path, backlog_id, task_id, attempt, created_at, outputs, kind) VALUES (?, ?, ?, ?, ?, ?, ?, 'task')`,
			newID(), path, backlogID, taskID, attempt, ts(s.now()), string(raw))
		return err
	})
}

// CardHomeNew records a card's home before it is made, live. The path is the key: a path already in the ledger is
// refused, because it belongs to another card.
func (s *Store) CardHomeNew(h CardHome) (CardHome, error) {
	h.ID, h.CreatedAt, h.State = newID(), ts(s.now()), CardHomeLive
	err := s.guard(func() error {
		_, err := s.db.Exec(`INSERT INTO card_home (id, path, kind, card, clone, branch, base, created_at, state) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'live')`,
			h.ID, h.Path, h.Kind, h.Card, h.Clone, h.Branch, h.Base, h.CreatedAt)
		return err
	})
	return h, err
}

// CardHomeCard says which card runs in a folder.
func (s *Store) CardHomeCard(path, card string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE card_home SET card = ? WHERE path = ?`, card, path)
		return err
	})
}

// CardHomeSet moves a folder to a state, with the reason and the event still owed to the hub.
func (s *Store) CardHomeSet(path, state, reason, owed string) error {
	return s.guard(func() error {
		removed := ""
		if state == CardHomeRemoved {
			removed = ts(s.now())
		}
		_, err := s.db.Exec(`UPDATE card_home SET state = ?, reason = ?, owed = ?, removed_at = ? WHERE path = ?`,
			state, reason, owed, removed, path)
		return err
	})
}

// CardHomeOwedClear withdraws what was owed once the hub has it.
func (s *Store) CardHomeOwedClear(path string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE card_home SET owed = '' WHERE path = ?`, path)
		return err
	})
}

// CardHomeByCard is the folder a card runs in. False for a card with none.
func (s *Store) CardHomeByCard(card string) (CardHome, bool, error) {
	return s.cardHomeOne(`WHERE card = ? ORDER BY created_at DESC LIMIT 1`, card)
}

// CardHomeByPath is the ledger row for a folder.
func (s *Store) CardHomeByPath(path string) (CardHome, bool, error) {
	return s.cardHomeOne(`WHERE path = ?`, path)
}

func (s *Store) cardHomeOne(where string, args ...any) (CardHome, bool, error) {
	var d CardHome
	found := false
	err := s.guard(func() error {
		var err error
		d, err = scanCardHome(s.db.QueryRow(`SELECT `+cardHomeCols+` FROM card_home `+where, args...))
		if err == sql.ErrNoRows {
			return nil
		}
		found = err == nil
		return err
	})
	return d, found, err
}

// CardHomesOf lists a task's folders, oldest first.
func (s *Store) CardHomesOf(backlogID, taskID string) ([]CardHome, error) {
	return s.cardHomes(`WHERE backlog_id = ? AND task_id = ? ORDER BY created_at`, backlogID, taskID)
}

// CardHomesOwed lists the folders with an event the hub has not acknowledged.
func (s *Store) CardHomesOwed() ([]CardHome, error) {
	return s.cardHomes(`WHERE owed != '' ORDER BY created_at`)
}

// CardHomesOpen lists the folders not yet removed, live or kept, oldest first.
func (s *Store) CardHomesOpen() ([]CardHome, error) {
	return s.cardHomes(`WHERE state != 'removed' ORDER BY created_at`)
}

func (s *Store) cardHomes(where string, args ...any) ([]CardHome, error) {
	var out []CardHome
	err := s.guard(func() error {
		rows, err := s.db.Query(`SELECT `+cardHomeCols+` FROM card_home `+where, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			d, err := scanCardHome(rows)
			if err != nil {
				return err
			}
			out = append(out, d)
		}
		return rows.Err()
	})
	return out, err
}

// CardHomeWorktree records the clone, branch and start a worktree home was added from.
func (s *Store) CardHomeWorktree(path, clone, branch, base string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE card_home SET clone = ?, branch = ?, base = ? WHERE path = ?`, clone, branch, base, path)
		return err
	})
}
