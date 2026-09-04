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
	ID      string            `json:"id"`
	Label   string            `json:"label"`
	Enabled bool              `json:"enabled"`
	Cmd     string            `json:"cmd"`
	Args    []string          `json:"args"`
	Cwd     string            `json:"cwd"`
	Env     map[string]string `json:"env"`
	// LaunchMode is "window" to open a real terminal, or "pty" for atrium to
	// own the process and stream it to the browser.
	LaunchMode string `json:"launch_mode"`
	// ResumeArgs replaces Args when picking a conversation back up. {resume}
	// is substituted with the runner's own session id.
	ResumeArgs []string `json:"resume_args"`
	// PromptArgs are appended to Args to hand the runner an opening
	// instruction. {prompt} is substituted with the text.
	//
	// Configured per runner for the same reason resuming is: there is no
	// common answer. Claude and codex read a bare argument as the first thing
	// to work on, and a shell would try to execute it. Empty means this runner
	// cannot be given an opening prompt, and a launch that supplies one is
	// refused rather than starting a session that will never read it.
	PromptArgs []string `json:"prompt_args"`
	// ExitKeys is what to send to ask this runner to exit, in order.
	//
	// There is no common answer: a shell takes `exit` and a newline, claude
	// takes control-d twice, ollama and codex take it once. Sending the wrong
	// one leaves the process running until its terminal is closed underneath
	// it, which is what an exit button exists to avoid.
	//
	// Tokens, not bytes, so the field can be written by hand. See ExitBytes.
	ExitKeys []string `json:"exit_keys"`
	// Prepare is a shell command run before the runner starts, whose resulting
	// environment the runner inherits.
	//
	// This is how a shell function that puts tools on PATH reaches an agent.
	// The habit it replaces is opening a terminal, running the function, and
	// starting the agent from that shell, which works and cannot be done from
	// a board.
	//
	// The environment is captured and handed over, rather than the runner
	// being started underneath a shell. Under a shell, the runner is no longer
	// the process atrium owns, which costs the exit keys, the liveness check
	// and the terminate button.
	Prepare string `json:"prepare"`
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
			ExitKeys:    []string{"ctrl-d", "ctrl-d"},
			PromptArgs:  []string{"{prompt}"},
			RulesSource: "claude", Sort: 10,
			Notes: "resume needs a session id, which only a runner that reports one can supply",
		},
		{
			ID: "codex", Label: "codex", Enabled: false, Cmd: "codex",
			LaunchMode: LaunchPTY, Sort: 20, ExitKeys: []string{"ctrl-d"},
			PromptArgs: []string{"{prompt}"},
			Notes:      "confirm the command and any resume flag before enabling",
		},
		{
			ID: "ollama", Label: "ollama", Enabled: false, Cmd: "ollama",
			Args: []string{"run", "llama3"}, LaunchMode: LaunchPTY, Sort: 30,
			ExitKeys: []string{"ctrl-d"},
			Notes:    "set the model in args. ollama has no permission config to import",
		},
		{
			// Not an agent. A plain shell in the chosen directory, running as
			// whoever started the daemon, shown in the browser like any other
			// supervised runner. Useful for the times the answer is a command
			// rather than a conversation, and for watching one from a phone.
			ID: "shell", Label: "shell", Enabled: false, Cmd: "pwsh",
			Args: []string{"-NoLogo"}, LaunchMode: LaunchPTY, Sort: 40,
			ExitKeys: []string{"exit"},
			Notes: "a plain shell, not an agent. runs as whoever started the daemon, " +
				"reports nothing about itself, and its card shows the terminal and nothing else",
		},
	}
}

func (s *Store) scanHarness(sc interface{ Scan(...any) error }) (*Harness, error) {
	var (
		h                               Harness
		args, env, resume, exit, prompt string
		created                         string
		enabled                         int
	)
	if err := sc.Scan(&h.ID, &h.Label, &enabled, &h.Cmd, &args, &h.Cwd, &env,
		&h.LaunchMode, &resume, &exit, &h.Prepare, &h.RulesSource, &h.Notes,
		&h.Sort, &created, &prompt); err != nil {
		return nil, err
	}
	h.Enabled = enabled != 0
	if err := json.Unmarshal([]byte(orDefault(args, "[]")), &h.Args); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(orDefault(resume, "[]")), &h.ResumeArgs); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(orDefault(prompt, "[]")), &h.PromptArgs); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(orDefault(exit, "[]")), &h.ExitKeys); err != nil {
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
	resume_args, exit_keys, prepare, rules_source, notes, sort, created_at, prompt_args`

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
	exit, err := json.Marshal(orEmptySlice(h.ExitKeys))
	if err != nil {
		return nil, err
	}
	prompt, err := json.Marshal(orEmptySlice(h.PromptArgs))
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
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET
				label = excluded.label, enabled = excluded.enabled, cmd = excluded.cmd,
				args = excluded.args, cwd = excluded.cwd, env = excluded.env,
				launch_mode = excluded.launch_mode, resume_args = excluded.resume_args,
				exit_keys = excluded.exit_keys, prepare = excluded.prepare,
				rules_source = excluded.rules_source, notes = excluded.notes,
				sort = excluded.sort, prompt_args = excluded.prompt_args`,
			h.ID, h.Label, enabled, h.Cmd, string(args), h.Cwd, string(env),
			h.LaunchMode, string(resume), string(exit), h.Prepare,
			h.RulesSource, h.Notes, h.Sort, created, string(prompt))
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.Harness(h.ID)
}

// ExitBytes turns the configured exit tokens into what to write to a terminal.
//
// A token is either a control key by name or literal text to type. Names
// rather than escape sequences, so the field is writable without knowing that
// control-d is 0x04.
//
// Returns one entry per token, because the sequence matters: claude wants two
// separate control-d presses, and sending them as one write is not the same
// thing to a program reading a terminal.
func (h *Harness) ExitBytes() [][]byte {
	var out [][]byte
	for _, k := range h.ExitKeys {
		key := strings.ToLower(strings.TrimSpace(k))
		switch key {
		case "":
			continue
		case "enter", "return", "cr":
			out = append(out, []byte("\r"))
		case "ctrl-c", "^c":
			out = append(out, []byte{0x03})
		case "ctrl-d", "^d", "eof":
			out = append(out, []byte{0x04})
		case "ctrl-z", "^z":
			out = append(out, []byte{0x1a})
		case "esc", "escape":
			out = append(out, []byte{0x1b})
		default:
			// Literal text. A shell's `exit` needs a newline after it, and
			// expecting the operator to add "enter" as a second token would be
			// a rule they discover by it not working.
			out = append(out, []byte(k+"\r"))
		}
	}
	return out
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
