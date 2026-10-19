package store

import (
	"database/sql"
	"encoding/json"
)

// THE FOLDERS A TASK OF AN ITEM RAN IN.

const (
	CardHomeLive    = "live"
	CardHomeKept    = "kept"
	CardHomeRemoved = "removed"
)

// CardHome is one folder a task ran in. Owed is the event the hub has not acknowledged, `reaped` once the folder is gone.
type CardHome struct {
	Path      string   `json:"path"`
	BacklogID string   `json:"backlog_id"`
	TaskID    string   `json:"task_id"`
	Attempt   int      `json:"attempt"`
	Card      string   `json:"card"`
	CreatedAt string   `json:"created_at"`
	State     string   `json:"state"`
	Reason    string   `json:"reason"`
	Owed      string   `json:"owed"`
	Outputs   []string `json:"outputs"`
}

const cardHomeCols = `path, backlog_id, task_id, attempt, card, created_at, state, reason, owed, outputs`

func scanCardHome(sc interface{ Scan(...any) error }) (CardHome, error) {
	var d CardHome
	var outputs string
	err := sc.Scan(&d.Path, &d.BacklogID, &d.TaskID, &d.Attempt, &d.Card, &d.CreatedAt, &d.State, &d.Reason, &d.Owed, &outputs)
	_ = json.Unmarshal([]byte(outputs), &d.Outputs)
	return d, err
}

// CardHomeAdd records a folder before it is made, live.
func (s *Store) CardHomeAdd(path, backlogID, taskID string, attempt int, outputs []string) error {
	raw, _ := json.Marshal(outputs)
	return s.guard(func() error {
		_, err := s.db.Exec(`INSERT OR IGNORE INTO card_home (path, backlog_id, task_id, attempt, created_at, outputs, kind) VALUES (?, ?, ?, ?, ?, ?, 'task')`,
			path, backlogID, taskID, attempt, ts(s.now()), string(raw))
		return err
	})
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
		_, err := s.db.Exec(`UPDATE card_home SET state = ?, reason = ?, owed = ? WHERE path = ?`, state, reason, owed, path)
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

// CardHomeByCard is the folder a card runs in. False for a card that is not a task's.
func (s *Store) CardHomeByCard(card string) (CardHome, bool, error) {
	var d CardHome
	found := false
	err := s.guard(func() error {
		var err error
		d, err = scanCardHome(s.db.QueryRow(`SELECT `+cardHomeCols+` FROM card_home WHERE card = ? ORDER BY created_at DESC LIMIT 1`, card))
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
