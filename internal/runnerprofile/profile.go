// Package runnerprofile says, in one table, what each runner needs from
// atrium's terminal and hooks that atrium cannot read off the bytes.
//
// Atrium's terminal was tuned against Claude Code, and codex was the first
// runner to show that some of that tuning was claude's own habits rather than
// the terminal's rules. A fact like that belongs to the runner, so it lives
// here, keyed the way a harness row is recognised, instead of as an
// `if codex` wherever it is needed.
//
// What each field is for, and what was measured, is on the field. A runner
// with no row here gets the zero Profile, which is what claude needs: every
// default below is claude's behaviour.
package runnerprofile

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// Profile is one runner.
type Profile struct {
	// ID matches the harness id atrium seeds for this runner.
	ID string
	// Cmds are the command leaf names that are this runner.
	Cmds []string

	// CursorSettle is how long the board waits after the last output before
	// it shows the terminal cursor. Zero writes the cursor straight through.
	//
	// For a runner whose output shows the cursor at a cell it was only
	// drawing, and moves it to the prompt in a later write. Codex does this
	// through ConPTY: on a live card 43% of its cursor shows landed away from
	// the prompt, corrected by the next write in 2ms at the median and 13ms at
	// the 90th percentile. Claude moves the cursor to its prompt before every
	// show, so it needs none. See `js/termcursor.js`.
	CursorSettle time.Duration

	// Hooks is the `claudeconf` hook target id atrium writes this runner's
	// hooks with, or empty when atrium has none for it. Every command a target
	// writes has to parse under the real `atrium` binary, which the fake
	// runner in `internal/cli/fakerunner_test.go` checks for each one.
	Hooks string

	// MidTurnInput says a line typed while the runner is mid-turn is read at
	// its next step rather than lost or merged, so a message can be typed in
	// without waiting for the turn to end. It seeds the harness row's
	// `mid_turn_input`, which the operator can change on the runners page.
	//
	// Claude Code queues typed input mid-turn and reads it at its next step.
	// Codex steers it into the active turn: codex-cli 0.156.1 says "new user
	// input is steered into the active turn" and tells its model "the user may
	// send a new message while you are still working". Gemini and ollama are
	// unmeasured, and ollama has no hooks, so it is never seen mid-turn.
	MidTurnInput bool

	// Unmeasured lists what this runner probably needs and nobody has
	// confirmed against a live session, so the next person starts from it.
	Unmeasured []string
}

// Profiles are the runners atrium has a view on.
var Profiles = []Profile{
	{ID: "claude", Cmds: []string{"claude"}, Hooks: "claude", MidTurnInput: true},
	{ID: "codex", Cmds: []string{"codex"}, Hooks: "codex", MidTurnInput: true,
		// Three times the 90th percentile, and under half the 85ms a real
		// cursor position typically holds for, so the prompt's cursor still
		// shows between frames while codex animates.
		CursorSettle: 40 * time.Millisecond},
	{ID: "gemini", Cmds: []string{"gemini"},
		Unmeasured: []string{
			"hooks: atrium has no target for gemini, so a gemini card reports no activity. a target " +
				"needs gemini's hooks file, event names and payload fields measured, then a row in " +
				"claudeconf.Targets and in the fake runner's runnerShapes",
			"cursor: an Ink app like claude, so probably draws its own cursor and needs no settle; " +
				"trace one with scripts/replay-term-trace.js before setting CursorSettle",
		}},
	{ID: "ollama", Cmds: []string{"ollama"},
		Unmeasured: []string{
			"hooks: ollama has none, so a card shows its terminal and nothing else",
			"cursor: a line-mode prompt that owns the real cursor; its spinner has not been traced",
		}},
}

// For finds the profile for a harness row: by id, then by its command's leaf
// name, the same rule `runnersetup.For` uses. The zero Profile when neither
// matches, which is claude's defaults.
func For(h *store.Harness) Profile {
	if h == nil {
		return Profile{}
	}
	for _, p := range Profiles {
		if strings.EqualFold(h.ID, p.ID) {
			return p
		}
	}
	for _, cmd := range []string{h.Cmd, h.BinPath} {
		leaf := Leaf(cmd)
		if leaf == "" {
			continue
		}
		for _, p := range Profiles {
			for _, c := range p.Cmds {
				if leaf == c {
					return p
				}
			}
		}
	}
	return Profile{}
}

// Leaf is a command's program name, lowercased, with its directory and a
// Windows launcher extension removed.
func Leaf(cmd string) string {
	cmd = strings.ToLower(strings.TrimSpace(strings.ReplaceAll(cmd, `\`, "/")))
	cmd = filepath.Base(filepath.FromSlash(cmd))
	if cmd == "." {
		return ""
	}
	for _, ext := range []string{".exe", ".cmd", ".bat", ".ps1"} {
		cmd = strings.TrimSuffix(cmd, ext)
	}
	return cmd
}
