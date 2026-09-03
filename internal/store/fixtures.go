package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Terminals that come up with the daemon.
//
// The habit this replaces: open a terminal, cd somewhere, resume the same
// claude, every single morning. Some sessions are permanent, and starting them
// by hand is a ritual rather than a decision.
//
// A fixture describes what to start, not a running thing. The card it started
// last time is remembered so the same conversation is resumed rather than a
// new one begun beside it, but that link is soft: the card can be swept and
// the fixture survives.

// Fixture is one terminal that starts with the daemon.
type Fixture struct {
	ID string `json:"id"`
	// Label is what to call it. Empty falls back to the directory name, the
	// same way a card's title does.
	Label string `json:"label"`
	// Harness is the runner row to start. A plain shell is a harness like any
	// other, which is how "always give me a terminal on this machine" is
	// expressed without a special case.
	Harness string `json:"harness"`
	Cwd     string `json:"cwd"`
	// Resume picks the conversation back up where a resume id is known. Off
	// means start fresh every time, which is what a plain shell wants.
	Resume bool `json:"resume"`
	// Enabled is whether this one starts. Off keeps the definition without
	// the behaviour, so a fixture can be parked without being retyped.
	Enabled bool `json:"enabled"`
	// Sort is the order they start and the order they appear in. Lower first.
	Sort float64 `json:"sort"`
	// Theme names a terminal palette. Empty means the board decides, which it
	// does from the project name.
	Theme string `json:"theme"`
	// TaskID is the card this fixture started last time, so the next start
	// lands on the same one rather than making another.
	TaskID    string `json:"task_id"`
	CreatedAt string `json:"created_at"`
}

const fixtureColumns = `id, label, harness, cwd, resume, enabled, sort, theme, task_id, created_at`

func scanFixture(sc interface{ Scan(...any) error }) (*Fixture, error) {
	var (
		f               Fixture
		resume, enabled int
	)
	if err := sc.Scan(&f.ID, &f.Label, &f.Harness, &f.Cwd, &resume, &enabled,
		&f.Sort, &f.Theme, &f.TaskID, &f.CreatedAt); err != nil {
		return nil, err
	}
	f.Resume = resume != 0
	f.Enabled = enabled != 0
	return &f, nil
}

// Fixtures returns every fixture in start order.
func (s *Store) Fixtures() ([]*Fixture, error) {
	var out []*Fixture
	err := s.guard(func() error {
		rows, err := s.db.Query(`SELECT ` + fixtureColumns + ` FROM fixture ORDER BY sort ASC, created_at ASC`)
		if err != nil {
			return err
		}
		defer rows.Close()
		out = nil
		for rows.Next() {
			f, err := scanFixture(rows)
			if err != nil {
				return err
			}
			out = append(out, f)
		}
		return rows.Err()
	})
	return out, err
}

// SaveFixture creates or replaces one.
//
// The id is the caller's, so the board can edit a row it is already holding
// without a round trip to find out what it was called.
func (s *Store) SaveFixture(f *Fixture) (*Fixture, error) {
	if strings.TrimSpace(f.Harness) == "" {
		return nil, errors.New("a fixture needs a runner to start")
	}
	if f.ID == "" {
		f.ID = newID()
	}
	if f.CreatedAt == "" {
		f.CreatedAt = ts(now())
	}
	resume, enabled := 0, 0
	if f.Resume {
		resume = 1
	}
	if f.Enabled {
		enabled = 1
	}
	err := s.guard(func() error {
		_, err := s.db.Exec(`INSERT INTO fixture (`+fixtureColumns+`)
			VALUES (?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET
				label = excluded.label, harness = excluded.harness, cwd = excluded.cwd,
				resume = excluded.resume, enabled = excluded.enabled, sort = excluded.sort,
				theme = excluded.theme`,
			f.ID, f.Label, f.Harness, f.Cwd, resume, enabled, f.Sort, f.Theme,
			f.TaskID, f.CreatedAt)
		return err
	})
	if err != nil {
		return nil, err
	}
	return f, nil
}

// DeleteFixture forgets one. The card it last started is left alone: the
// fixture is the instruction, not the work.
func (s *Store) DeleteFixture(id string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`DELETE FROM fixture WHERE id = ?`, id)
		return err
	})
}

// NoteFixtureTask records which card a fixture started, so the next start
// resumes that conversation instead of opening another beside it.
//
// Written separately from SaveFixture because it is the daemon reporting what
// happened rather than the operator changing what should happen, and an edit
// in the board must not clear it.
func (s *Store) NoteFixtureTask(id, taskID string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE fixture SET task_id = ? WHERE id = ?`, taskID, id)
		return err
	})
}

// AdoptableTask finds a live card already sitting in a directory, so a fixture
// created for somewhere that already has one takes it over instead of opening
// a second card with the same name beside it.
//
// Only a card that is still alive. A finished or dead one is history, and
// resuming onto it would rewrite what happened.
//
// Empty id and no error when there is nothing to adopt, which is the ordinary
// case and not a failure.
func (s *Store) AdoptableTask(worktree string) (string, error) {
	worktree = strings.TrimSpace(worktree)
	if worktree == "" {
		return "", nil
	}
	var id string
	err := s.guard(func() error {
		row := s.db.QueryRow(
			`SELECT id FROM task
			 WHERE worktree = ? AND status NOT IN ('done','dead')
			 ORDER BY last_activity_at DESC LIMIT 1`, worktree)
		err := row.Scan(&id)
		if err == sql.ErrNoRows {
			id = ""
			return nil
		}
		return err
	})
	return id, err
}

// GetFixture reads one.
func (s *Store) GetFixture(id string) (*Fixture, error) {
	var f *Fixture
	err := s.guard(func() error {
		row := s.db.QueryRow(`SELECT `+fixtureColumns+` FROM fixture WHERE id = ?`, id)
		got, err := scanFixture(row)
		if err == sql.ErrNoRows {
			return fmt.Errorf("no fixture %s", id)
		}
		if err != nil {
			return err
		}
		f = got
		return nil
	})
	return f, err
}

// SetTheme records what a terminal looks like, on the card rather than in the
// browser, so it survives a restart and follows the session to another
// machine's browser.
func (s *Store) SetTheme(id, theme string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE task SET theme = ? WHERE id = ?`, strings.TrimSpace(theme), id)
		return err
	})
}
