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
	// ModelArgs name a model for one launch. {model} is substituted with it.
	//
	// Per runner for the third time and for the same reason as the two above:
	// there is no common spelling. Claude takes `--model <name>`, a shell has
	// no model at all and would try to execute the flag. Empty means this
	// runner cannot be asked for a model, and a launch that names one is
	// REFUSED rather than started on the default, because a session quietly
	// running on the wrong model is not visible until the output or the bill
	// is wrong.
	//
	// This is not a list of models and must not become one. It is the shape of
	// the argument. Which models exist changes every few months, and a list
	// held here would ship out of date.
	ModelArgs []string `json:"model_args"`
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
	// BracketedPaste enables markers for runners that keep the mode on throughout
	// their session. The startup enable sequence can fall out of scrollback, so
	// the board also needs this configuration. Shells use stream detection
	// because they toggle the mode around each prompt.
	BracketedPaste bool `json:"bracketed_paste"`
	// Package is where this runner is installed from, so atrium can ask
	// whether a newer one is published without running the runner to find out.
	//
	// An npm package name today, because both runners that have one come from
	// npm. It is the NAME OF A THING TO ASK ABOUT and not a registry client:
	// atrium reads the version out of the installed package's own metadata and
	// asks the public registry what the latest is, and does neither if this is
	// empty. A bare shell has no version and ollama ships as a platform
	// installer, so empty is the right answer for both and means "do not ask".
	Package string `json:"package"`
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
// SEEDED ONCE, ON FIRST RUN. A harness is a row the operator may have edited,
// and rewriting it on every start would throw that away.
func DefaultHarnesses() []Harness {
	return []Harness{
		// Use a PTY by default so atrium can attach, terminate, and check liveness.
		// Window mode remains available for runners that need to outlive the daemon.
		//
		// Start Claude without MCP servers to avoid prompts blocking unattended launches.
		// --strict-mcp-config without --mcp-config does this without requiring a file.
		// Atrium communicates through hooks and the CLI. Set the flag in both argument
		// lists because resume_args replaces args.
		{
			ID: "claude", Label: "claude code", Enabled: true, Cmd: "claude",
			LaunchMode: LaunchPTY, Args: []string{"--strict-mcp-config"},
			ResumeArgs:  []string{"--resume", "{resume}", "--strict-mcp-config"},
			ExitKeys:    []string{"ctrl-d", "ctrl-d"},
			PromptArgs:  []string{"{prompt}"},
			ModelArgs:   []string{"--model", "{model}"},
			RulesSource: "claude", Sort: 10, BracketedPaste: true,
			Package: "@anthropic-ai/claude-code",
			Notes:   "resume needs a session id, which only a runner that reports one can supply",
		},
		{
			// `codex resume <SESSION_ID>` takes the same uuid codex puts in
			// `session_id` on every hook payload, so the id a card records is
			// the id that picks the conversation back up. Confirmed against
			// codex-cli 0.153.2, where `codex resume --help` names the
			// argument as a session uuid.
			//
			// A subcommand rather than a flag, which is why it is the whole
			// argument list and not something appended: `resume` displaces
			// the prompt, and `resume_args` replaces `args` by design.
			ID: "codex", Label: "codex", Enabled: false, Cmd: "codex",
			LaunchMode: LaunchPTY, Sort: 20, ExitKeys: []string{"ctrl-d"},
			ResumeArgs:     []string{"resume", "{resume}"},
			PromptArgs:     []string{"{prompt}"},
			ModelArgs:      []string{"--model", "{model}"},
			BracketedPaste: true, Package: "@openai/codex",
			RulesSource: "", Notes: "hooks live in $CODEX_HOME/hooks.json, not in atrium's " +
				"settings, and codex will not run one it has not been shown once",
		},
		{
			ID: "ollama", Label: "ollama", Enabled: false, Cmd: "ollama",
			Args: []string{"run", "llama3"}, LaunchMode: LaunchPTY, Sort: 30,
			ExitKeys: []string{"ctrl-d"},
			Notes:    "set the model in args. ollama has no permission config to import",
		},
		// THERE IS NO `shell` RUNNER, AND THAT IS THE POINT.
		//
		// There was one, and it was a second answer to a question the machine
		// had already answered. A shell is not something atrium can be
		// configured to start: it is a property of the machine, held in the
		// `shell_command` setting and found by `internal/shellpick` when that
		// is empty, and `shellFor` reads it FRESH every time a shell is
		// opened. The runner row was a copy of that taken on first run and
		// then frozen, so changing the setting moved one shell and left the
		// other one starting the program from months ago.
		//
		// It fitted the shape badly too. A runner is a command, a way to
		// resume, a way to be given a prompt and a model, a set of hooks and a
		// rules file. A shell has none of those. Half the row was blank and
		// the other half was ignored, and `isShellRunner` existed purely to
		// keep it out of the list of things that can be started as an agent.
		//
		// A shell is still one press away, on the terminal of any card that
		// has one, which is where somebody wants a shell: beside the agent, in
		// its directory. See `shellFor` in internal/daemon/shell.go.
	}
}

func (s *Store) scanHarness(sc interface{ Scan(...any) error }) (*Harness, error) {
	var (
		h                                      Harness
		args, env, resume, exit, prompt, model string
		created                                string
		enabled                                int
		bracketed                              int
	)
	if err := sc.Scan(&h.ID, &h.Label, &enabled, &h.Cmd, &args, &h.Cwd, &env,
		&h.LaunchMode, &resume, &exit, &h.Prepare, &h.RulesSource, &h.Notes,
		&h.Sort, &created, &prompt, &model, &bracketed, &h.Package); err != nil {
		return nil, err
	}
	h.Enabled = enabled != 0
	h.BracketedPaste = bracketed != 0
	if err := json.Unmarshal([]byte(orDefault(args, "[]")), &h.Args); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(orDefault(resume, "[]")), &h.ResumeArgs); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(orDefault(prompt, "[]")), &h.PromptArgs); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(orDefault(model, "[]")), &h.ModelArgs); err != nil {
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
	resume_args, exit_keys, prepare, rules_source, notes, sort, created_at, prompt_args,
	model_args, bracketed_paste, package`

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
	model, err := json.Marshal(orEmptySlice(h.ModelArgs))
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
		bracketed := 0
		if h.BracketedPaste {
			bracketed = 1
		}
		_, err = s.db.Exec(`INSERT INTO harness (`+harnessColumns+`)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET
				label = excluded.label, enabled = excluded.enabled, cmd = excluded.cmd,
				args = excluded.args, cwd = excluded.cwd, env = excluded.env,
				launch_mode = excluded.launch_mode, resume_args = excluded.resume_args,
				exit_keys = excluded.exit_keys, prepare = excluded.prepare,
				rules_source = excluded.rules_source, notes = excluded.notes,
				sort = excluded.sort, prompt_args = excluded.prompt_args,
				model_args = excluded.model_args,
				bracketed_paste = excluded.bracketed_paste,
				package = excluded.package`,
			h.ID, h.Label, enabled, h.Cmd, string(args), h.Cwd, string(env),
			h.LaunchMode, string(resume), string(exit), h.Prepare,
			h.RulesSource, h.Notes, h.Sort, created, string(prompt), string(model),
			bracketed, strings.TrimSpace(h.Package))
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
