package store

import (
	"database/sql"
	"time"
)

// A document shared publicly, remembered across restarts.
//
// `task_id` is not a foreign key on purpose: the row outlives the card, because the snapshot does not need it.

// DocShare is one shared snapshot.
type DocShare struct {
	ID        string `json:"id"`
	TaskID    string `json:"task_id"`
	Path      string `json:"path"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Token     string `json:"token,omitempty"`
	Address   string `json:"address"`
	Dir       string `json:"-"`
	Wanted    bool   `json:"wanted"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	ExpiresAt string `json:"expires_at"`
}

const docShareCols = `id, task_id, path, namespace, name, token, address, dir, wanted, created_at, updated_at, expires_at`

func scanDocShare(sc interface{ Scan(...any) error }) (DocShare, error) {
	var d DocShare
	var wanted int
	err := sc.Scan(&d.ID, &d.TaskID, &d.Path, &d.Namespace, &d.Name, &d.Token, &d.Address, &d.Dir,
		&wanted, &d.CreatedAt, &d.UpdatedAt, &d.ExpiresAt)
	d.Wanted = wanted == 1
	return d, err
}

// NewDocShareID is a fresh id for a share and for its folder.
func NewDocShareID() string { return newID() }

// PutDocShare inserts a share, or replaces the one with that id.
func (s *Store) PutDocShare(d DocShare) error {
	if d.ID == "" {
		d.ID = newID()
	}
	now := ts(s.now())
	if d.CreatedAt == "" {
		d.CreatedAt = now
	}
	if d.UpdatedAt == "" {
		d.UpdatedAt = now
	}
	wanted := 0
	if d.Wanted {
		wanted = 1
	}
	return s.guard(func() error {
		_, err := s.db.Exec(
			`INSERT INTO doc_share (`+docShareCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(id) DO UPDATE SET
			   token = excluded.token, address = excluded.address, dir = excluded.dir,
			   wanted = excluded.wanted, updated_at = excluded.updated_at, expires_at = excluded.expires_at`,
			d.ID, d.TaskID, d.Path, d.Namespace, d.Name, d.Token, d.Address, d.Dir,
			wanted, d.CreatedAt, d.UpdatedAt, d.ExpiresAt)
		return err
	})
}

// DocShareByID reads one share, or nil when there is none.
func (s *Store) DocShareByID(id string) (*DocShare, error) {
	var out *DocShare
	err := s.guard(func() error {
		d, err := scanDocShare(s.db.QueryRow(`SELECT `+docShareCols+` FROM doc_share WHERE id = ?`, id))
		if err == sql.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		out = &d
		return nil
	})
	return out, err
}

// DocShareFor is the live share of one document of one card, or nil.
func (s *Store) DocShareFor(taskID, path string) (*DocShare, error) {
	var out *DocShare
	err := s.guard(func() error {
		d, err := scanDocShare(s.db.QueryRow(
			`SELECT `+docShareCols+` FROM doc_share WHERE task_id = ? AND path = ? AND wanted = 1`, taskID, path))
		if err == sql.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		out = &d
		return nil
	})
	return out, err
}

// DocShares is every recorded share, oldest first.
func (s *Store) DocShares() ([]DocShare, error) {
	var out []DocShare
	err := s.guard(func() error {
		rows, err := s.db.Query(`SELECT ` + docShareCols + ` FROM doc_share ORDER BY created_at, id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			d, err := scanDocShare(rows)
			if err != nil {
				return err
			}
			out = append(out, d)
		}
		return rows.Err()
	})
	return out, err
}

// LiveDocShareCount is how many shares are wanted, for the per-room cap.
func (s *Store) LiveDocShareCount() (int, error) {
	var n int
	err := s.guard(func() error {
		return s.db.QueryRow(`SELECT COUNT(*) FROM doc_share WHERE wanted = 1`).Scan(&n)
	})
	return n, err
}

// DeleteDocShare drops the row, once its name has been released and its folder removed.
func (s *Store) DeleteDocShare(id string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`DELETE FROM doc_share WHERE id = ?`, id)
		return err
	})
}

// TaskExists is whether a card is still on this room.
func (s *Store) TaskExists(id string) bool {
	var n int
	_ = s.guard(func() error {
		return s.db.QueryRow(`SELECT COUNT(*) FROM task WHERE id = ?`, id).Scan(&n)
	})
	return n > 0
}

// DocShareExpired reports whether the share's expiry is at or before now.
func (s *Store) DocShareExpired(d DocShare) bool {
	t, err := time.Parse(TimeFormat, d.ExpiresAt)
	if err != nil {
		return false
	}
	return !t.After(s.now())
}
