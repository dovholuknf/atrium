package store

import (
	"database/sql"
	"encoding/json"
)

// THE FOLDERS A TASK OF AN ITEM RAN IN.

const (
	TaskDirLive    = "live"
	TaskDirKept    = "kept"
	TaskDirRemoved = "removed"
)

// TaskDir is one folder a task ran in. Owed is the event the hub has not acknowledged, `reaped` once the folder is gone.
type TaskDir struct {
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

const taskDirCols = `path, backlog_id, task_id, attempt, card, created_at, state, reason, owed, outputs`

func scanTaskDir(sc interface{ Scan(...any) error }) (TaskDir, error) {
	var d TaskDir
	var outputs string
	err := sc.Scan(&d.Path, &d.BacklogID, &d.TaskID, &d.Attempt, &d.Card, &d.CreatedAt, &d.State, &d.Reason, &d.Owed, &outputs)
	_ = json.Unmarshal([]byte(outputs), &d.Outputs)
	return d, err
}

// TaskDirAdd records a folder before it is made, live.
func (s *Store) TaskDirAdd(path, backlogID, taskID string, attempt int, outputs []string) error {
	raw, _ := json.Marshal(outputs)
	return s.guard(func() error {
		_, err := s.db.Exec(`INSERT OR IGNORE INTO task_dir (path, backlog_id, task_id, attempt, created_at, outputs) VALUES (?, ?, ?, ?, ?, ?)`,
			path, backlogID, taskID, attempt, ts(s.now()), string(raw))
		return err
	})
}

// TaskDirCard says which card runs in a folder.
func (s *Store) TaskDirCard(path, card string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE task_dir SET card = ? WHERE path = ?`, card, path)
		return err
	})
}

// TaskDirSet moves a folder to a state, with the reason and the event still owed to the hub.
func (s *Store) TaskDirSet(path, state, reason, owed string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE task_dir SET state = ?, reason = ?, owed = ? WHERE path = ?`, state, reason, owed, path)
		return err
	})
}

// TaskDirOwedClear withdraws what was owed once the hub has it.
func (s *Store) TaskDirOwedClear(path string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE task_dir SET owed = '' WHERE path = ?`, path)
		return err
	})
}

// TaskDirByCard is the folder a card runs in. False for a card that is not a task's.
func (s *Store) TaskDirByCard(card string) (TaskDir, bool, error) {
	var d TaskDir
	found := false
	err := s.guard(func() error {
		var err error
		d, err = scanTaskDir(s.db.QueryRow(`SELECT `+taskDirCols+` FROM task_dir WHERE card = ? ORDER BY created_at DESC LIMIT 1`, card))
		if err == sql.ErrNoRows {
			return nil
		}
		found = err == nil
		return err
	})
	return d, found, err
}

// TaskDirsOf lists a task's folders, oldest first.
func (s *Store) TaskDirsOf(backlogID, taskID string) ([]TaskDir, error) {
	return s.taskDirs(`WHERE backlog_id = ? AND task_id = ? ORDER BY created_at`, backlogID, taskID)
}

// TaskDirsOwed lists the folders with an event the hub has not acknowledged.
func (s *Store) TaskDirsOwed() ([]TaskDir, error) {
	return s.taskDirs(`WHERE owed != '' ORDER BY created_at`)
}

func (s *Store) taskDirs(where string, args ...any) ([]TaskDir, error) {
	var out []TaskDir
	err := s.guard(func() error {
		rows, err := s.db.Query(`SELECT `+taskDirCols+` FROM task_dir `+where, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			d, err := scanTaskDir(rows)
			if err != nil {
				return err
			}
			out = append(out, d)
		}
		return rows.Err()
	})
	return out, err
}
