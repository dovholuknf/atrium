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
	// ResumeMode is WHICH conversation, which is a different question and the
	// one that was getting the wrong answer.
	//
	//   ""/"latest"  the newest conversation in that directory
	//   "card"       the id recorded on the card this fixture started last
	//
	// `latest` is the default because it is what a fixture means: give me back
	// the terminal I had. `card` pins it to one conversation, which is right
	// only when that card is the only thing ever run in the directory.
	ResumeMode string `json:"resume_mode,omitempty"`
	// Enabled is whether this one starts. Off keeps the definition without
	// the behavior, so a fixture can be parked without being retyped.
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
	// LastError is why the last start failed, or empty when it worked.
	//
	// Fixtures start in the background, so before this a failure had nowhere
	// to go but the daemon's log and the only symptom was a terminal that was
	// not there. The row that failed carries its own reason, which is what
	// makes the page listing them the page that answers why one is missing.
	LastError string `json:"last_error,omitempty"`
	// LastRunAt is when it was last asked to start, whether or not that
	// worked. Without it an empty LastError cannot be told apart from a
	// fixture that has never been tried.
	LastRunAt string `json:"last_run_at,omitempty"`
}

const fixtureColumns = `id, label, harness, cwd, resume, enabled, sort, theme, task_id, created_at,
	last_error, last_run_at, resume_mode`

func scanFixture(sc interface{ Scan(...any) error }) (*Fixture, error) {
	var (
		f               Fixture
		resume, enabled int
	)
	if err := sc.Scan(&f.ID, &f.Label, &f.Harness, &f.Cwd, &resume, &enabled,
		&f.Sort, &f.Theme, &f.TaskID, &f.CreatedAt, &f.LastError, &f.LastRunAt,
		&f.ResumeMode); err != nil {
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
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET
				label = excluded.label, harness = excluded.harness, cwd = excluded.cwd,
				resume = excluded.resume, enabled = excluded.enabled, sort = excluded.sort,
				theme = excluded.theme, resume_mode = excluded.resume_mode`,
			f.ID, f.Label, f.Harness, f.Cwd, resume, enabled, f.Sort, f.Theme,
			f.TaskID, f.CreatedAt, f.LastError, f.LastRunAt, f.ResumeMode)
		return err
	})
	if err != nil {
		return nil, err
	}
	return f, nil
}

// NoteFixtureRun records how the last start went. Empty reason means it
// worked, which is what clears a failure that has since been fixed.
//
// Deliberately NOT part of SaveFixture. Same split as NoteFixtureTask: this is
// the daemon reporting what happened, and an edit made in the board while a
// fixture was starting must not overwrite it with a stale copy of the row.
func (s *Store) NoteFixtureRun(id, reason string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE fixture SET last_error = ?, last_run_at = ? WHERE id = ?`,
			strings.TrimSpace(reason), ts(now()), id)
		return err
	})
}

// NoteAsked records that this session has put a question to the operator.
//
// Written straight to the column rather than through SetStatus, because the
// card is still RUNNING when this happens: the model calls the asking tool
// mid-turn, and the wait it causes arrives afterwards from a different hook
// that knows nothing about it. SetStatus carries the mark forward from here.
//
// Cleared by the same rule everything else in that column follows: leaving a
// waiting state empties it, so a question that has been answered stops being
// reported the moment the session gets back to work.
func (s *Store) NoteAsked(id string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE task SET waiting_reason = ? WHERE id = ?`, WaitingAsked, id)
		return err
	})
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

// SetSound records which tone a card rings with, on the card for the same
// reason its theme is there: knowing a session by its sound only works if the
// answer is the same tomorrow and in another browser.
//
// The name is not checked against a list. The board owns the set of tones and
// gains new ones without a migration, and a name the board no longer knows
// falls back to the default rather than failing.
func (s *Store) SetSound(id, sound string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE task SET sound = ? WHERE id = ?`, strings.TrimSpace(sound), id)
		return err
	})
}

// IconMax is how much of an icon is kept.
//
// One glyph is the point, but a glyph is not one rune: a flag is two, and an
// emoji with a skin tone or a zero width joiner sequence is several. Counted in
// runes rather than bytes so the limit means the same thing whatever alphabet
// it is written in, and generous enough that no ordinary emoji is cut in half,
// which would leave a replacement box on every notification that card sent.
const IconMax = 16

// SetIcon records the mark a card wears on a desktop notification.
//
// Not checked against a list, for the same reason the tone is not: the board
// owns what it can draw, and a fixed set here would be the database deciding
// what a project may look like.
func (s *Store) SetIcon(id, icon string) error {
	return s.guard(func() error {
		icon = strings.TrimSpace(icon)
		if r := []rune(icon); len(r) > IconMax {
			icon = string(r[:IconMax])
		}
		_, err := s.db.Exec(`UPDATE task SET icon = ? WHERE id = ?`, icon, id)
		return err
	})
}
