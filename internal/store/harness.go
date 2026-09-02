package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Harness is a runner atrium knows how to start: claude, codex, ollama, a bare
// shell, or anything else you add. Nothing about a harness is special-cased in
// code. It is a command line, a working directory, an environment, and a way to
// resume, which is why adding a new one is configuration rather than a change
// here.
type Harness struct {
	ID       string            `json:"id"`
	Label    string            `json:"label"`
	Enabled  bool              `json:"enabled"`
	Cmd      string            `json:"cmd"`
	Args     []string          `json:"args"`
	Cwd      string            `json:"cwd"`
	Env      map[string]string `json:"env"`
	// LaunchMode is "window" to open a real terminal, or "pty" for atrium to
	// own the process and stream it to the browser.
	LaunchMode string `json:"launch_mode"`
	// ResumeArgs replaces Args when picking a conversation back up. {resume}
	// is substituted with the runner's own session id.
	ResumeArgs []string `json:"resume_args"`
	// RulesSource names the importer that can read this runner's own
	// permission config. Empty means atrium's JSON is the only exchange format.
	RulesSource string    `json:"rules_source"`
	Notes       string    `json:"notes"`
	Sort        int       `json:"sort"`
	CreatedAt   time.Time `json:"created_at"`
}

// LaunchWindow opens a real terminal window. LaunchPTY has atrium own the
// process instead.
const (
	LaunchWindow = "window"
	LaunchPTY    = "pty"
)

// DefaultHarnesses are seeded on first run. They are ordinary rows: edit,
// disable, or delete any of them, and add your own.
//
// Only claude is enabled, because it is the only one whose invocation is known
// to work on this machine. The rest are scaffolding with a plausible command,
// left off until their command line is confirmed.
func DefaultHarnesses() []Harness {
	return []Harness{
		// pty by default. Something started from atrium should be something
		// atrium can attach to, terminate and check the liveness of. Window
		// mode remains for the one thing it does better: a runner that
		// outlives the daemon.
		{
			ID: "claude", Label: "claude code", Enabled: true, Cmd: "claude",
			LaunchMode: LaunchPTY, ResumeArgs: []string{"--resume", "{resume}"},
			RulesSource: "claude", Sort: 10,
			Notes:       "resume needs a session id, which only a runner that reports one can supply",
		},
		{
			ID: "codex", Label: "codex", Enabled: false, Cmd: "codex",
			LaunchMode: LaunchPTY, Sort: 20,
			Notes:      "confirm the command and any resume flag before enabling",
		},
		{
			ID: "ollama", Label: "ollama", Enabled: false, Cmd: "ollama",
			Args: []string{"run", "llama3"}, LaunchMode: LaunchPTY, Sort: 30,
			Notes: "set the model in args. ollama has no permission config to import",
		},
		{
			ID: "shell", Label: "shell", Enabled: false, Cmd: "pwsh",
			Args: []string{"-NoLogo"}, LaunchMode: LaunchPTY, Sort: 40,
			Notes: "a plain shell, supervised like any other runner",
		},
	}
}

func (s *Store) scanHarness(sc interface{ Scan(...any) error }) (*Harness, error) {
	var (
		h                     Harness
		args, env, resume     string
		created               string
		enabled               int
	)
	if err := sc.Scan(&h.ID, &h.Label, &enabled, &h.Cmd, &args, &h.Cwd, &env,
		&h.LaunchMode, &resume, &h.RulesSource, &h.Notes, &h.Sort, &created); err != nil {
		return nil, err
	}
	h.Enabled = enabled != 0
	if err := json.Unmarshal([]byte(orDefault(args, "[]")), &h.Args); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(orDefault(resume, "[]")), &h.ResumeArgs); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(orDefault(env, "{}")), &h.Env); err != nil {
		return nil, err
	}
	var err error
	if h.CreatedAt, err = parseTS(created); err != nil {
		return nil, err
	}
	return &h, nil
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

const harnessColumns = `id, label, enabled, cmd, args, cwd, env, launch_mode,
	resume_args, rules_source, notes, sort, created_at`

// Harnesses lists every configured runner.
func (s *Store) Harnesses() ([]*Harness, error) {
	var out []*Harness
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(`SELECT ` + harnessColumns + ` FROM harness ORDER BY sort ASC, id ASC`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			h, err := s.scanHarness(rows)
			if err != nil {
				return err
			}
			out = append(out, h)
		}
		return rows.Err()
	})
	return out, err
}

// Harness returns one runner by id.
func (s *Store) Harness(id string) (*Harness, error) {
	var h *Harness
	err := s.guard(func() error {
		row := s.db.QueryRow(`SELECT `+harnessColumns+` FROM harness WHERE id = ?`, id)
		got, err := s.scanHarness(row)
		if err != nil {
			return err
		}
		h = got
		return nil
	})
	return h, err
}

// SaveHarness creates or replaces a runner.
func (s *Store) SaveHarness(h Harness) (*Harness, error) {
	h.ID = strings.TrimSpace(h.ID)
	if h.ID == "" {
		return nil, errors.New("a harness needs an id")
	}
	if strings.TrimSpace(h.Cmd) == "" {
		return nil, errors.New("a harness needs a command to run")
	}
	if h.LaunchMode != LaunchWindow && h.LaunchMode != LaunchPTY {
		h.LaunchMode = LaunchWindow
	}
	if h.Label == "" {
		h.Label = h.ID
	}
	args, err := json.Marshal(orEmptySlice(h.Args))
	if err != nil {
		return nil, err
	}
	resume, err := json.Marshal(orEmptySlice(h.ResumeArgs))
	if err != nil {
		return nil, err
	}
	if h.Env == nil {
		h.Env = map[string]string{}
	}
	env, err := json.Marshal(h.Env)
	if err != nil {
		return nil, err
	}

	err = s.guard(func() error {
		existing, err := s.db.Query(`SELECT created_at FROM harness WHERE id = ?`, h.ID)
		if err != nil {
			return err
		}
		created := ts(now())
		if existing.Next() {
			if err := existing.Scan(&created); err != nil {
				existing.Close()
				return err
			}
		}
		existing.Close()

		enabled := 0
		if h.Enabled {
			enabled = 1
		}
		_, err = s.db.Exec(`INSERT INTO harness (`+harnessColumns+`)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET
				label = excluded.label, enabled = excluded.enabled, cmd = excluded.cmd,
				args = excluded.args, cwd = excluded.cwd, env = excluded.env,
				launch_mode = excluded.launch_mode, resume_args = excluded.resume_args,
				rules_source = excluded.rules_source, notes = excluded.notes,
				sort = excluded.sort`,
			h.ID, h.Label, enabled, h.Cmd, string(args), h.Cwd, string(env),
			h.LaunchMode, string(resume), h.RulesSource, h.Notes, h.Sort, created)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.Harness(h.ID)
}

func orEmptySlice(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// DeleteHarness removes a runner.
func (s *Store) DeleteHarness(id string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`DELETE FROM harness WHERE id = ?`, id)
		return err
	})
}

// SeedHarnesses inserts the defaults, leaving any the operator already has
// alone. Run on every open so a new default appears without wiping edits.
func (s *Store) SeedHarnesses() error {
	for _, h := range DefaultHarnesses() {
		var existing string
		err := s.db.QueryRow(`SELECT id FROM harness WHERE id = ?`, h.ID).Scan(&existing)
		if err == nil {
			continue
		}
		if err != sql.ErrNoRows {
			return err
		}
		if _, err := s.SaveHarness(h); err != nil {
			return err
		}
	}
	return nil
}
