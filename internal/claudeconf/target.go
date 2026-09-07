package claudeconf

import (
	"os"
	"path/filepath"
)

// Which runner's hook configuration is being read or written.
//
// Claude Code and Codex use the SAME file format: a `hooks` object keyed by
// event name, each holding matcher entries, each holding a list of
// `{type, command}`. The output fields a hook may print are the same too. What
// differs is the file it lives in, the set of events, and one extra step for
// Codex.
//
// So this is a target rather than a second package. Everything below the file
// shape is identical, and two copies of it would be two places for the
// "preserve what the operator put here" rules to drift apart.
//
// Verified rather than assumed, against codex-cli 0.153.2 with a probe hook in
// a scratch CODEX_HOME. Codex read the file, matched the events, and its
// payloads carry the same field names claude's do: `session_id`, `cwd`,
// `transcript_path`, `source`, `reason`, `tool_name`, `tool_input`,
// `tool_use_id`, `stop_hook_active`, `agent_id`, `agent_type`. Two differ and
// neither is read by atrium: the prompt arrives as `prompt` rather than
// `user_input`, and a tool result as `tool_response` rather than
// `tool_result`. `docs/other-runners.md` has the payloads.
type Target struct {
	// ID is what atrium calls this runner, matching the harness id.
	ID string `json:"id"`
	// Label is for reading.
	Label string `json:"label"`
	// Path answers where the file lives.
	Path func() (string, error) `json:"-"`
	// Wanted is the set of hooks atrium can register for this runner.
	Wanted []HookEvent `json:"-"`
	// Trust is what has to happen after the file is written, when the runner
	// requires more than the file existing. Empty when writing is enough.
	//
	// Codex refuses to run a hook it has not been shown, which is what
	// `--dangerously-bypass-hook-trust` overrides. Atrium writes the file and
	// says this; it does not reach into the trust store, because that is the
	// one step whose whole purpose is that a human took it.
	Trust string `json:"trust,omitempty"`
	// ProgramUnquoted says this runner reads the FIRST word of a command as
	// the program and does no quote handling on it.
	//
	// Codex does exactly that. `"C:/tools/atrium.exe" hook --event tool-start`
	// fails and `C:/tools/atrium.exe hook --event tool-start` runs, with the
	// only difference being the two quotes, and the failure is a line saying
	// `hook: SessionStart Failed` in a runner nobody is watching. Quotes on
	// the ARGUMENTS are honored, which is why this is about the program alone.
	//
	// Claude Code hands the whole string to a shell, so it needs the quotes
	// and gets them. There is no spelling that suits both, which is why this
	// is a fact about the runner rather than a rule in the writer.
	ProgramUnquoted bool `json:"-"`
	// Named says the hook command should say which runner is reporting.
	//
	// Every hook subcommand assumes claude when nothing says otherwise, which
	// is what every caller meant before there was a second runner. A codex
	// session that did not say so would put a card on the board wearing the
	// wrong colour and offering `claude --resume` for an id claude has never
	// heard of.
	Named bool `json:"-"`
}

// Claude Code, in ~/.claude/settings.json.
var Claude = Target{
	ID:     "claude",
	Label:  "claude code",
	Path:   UserSettingsPath,
	Wanted: WantedHooks,
}

// Codex, in $CODEX_HOME/hooks.json.
//
// The events are Codex's own names and they line up with Claude Code's almost
// exactly, which is why the atrium subcommands behind them are unchanged: a
// SessionStart is a session starting whoever asked.
//
// Codex fires twelve: PreCompact, PostCompact, SessionStart, SessionEnd,
// SubagentStart, SubagentStop, PreToolUse, PostToolUse, UserPromptSubmit,
// Stop, PermissionRequest and Interrupt. Four are not offered here.
//
// `PermissionRequest` is the gate, and atrium's gate is a script in the
// operator's own dotfiles rather than anything atrium writes. It works on
// codex unchanged, on `PreToolUse`, which is where it already sits: codex
// reads the same `hookSpecificOutput.permissionDecision` shape and blocks the
// call. See `docs/other-runners.md`.
//
// `PostCompact` and `Interrupt` have no atrium subcommand behind them, and
// offering a hook atrium does nothing with is a switch that reports success
// and changes nothing.
//
// Codex has no `Notification` and no `PostToolUseFailure`, so those two lines
// of the claude set have no codex row and the board says so rather than
// showing eight of ten.
var Codex = Target{
	ID:    "codex",
	Label: "codex",
	Path:  CodexHooksPath,
	Trust: "codex will not run a hook it has not been shown. start a codex session and " +
		"approve it once, or pass --dangerously-bypass-hook-trust.",
	ProgramUnquoted: true,
	Named:           true,
	Wanted: []HookEvent{
		{Hook: "SessionStart", Event: "session-start", Sub: "session", Arg: "start",
			Why: "a card appears when a session opens, before it does anything"},
		{Hook: "SessionEnd", Event: "session-end", Sub: "session", Arg: "end",
			Why: "the card goes to finished when the session closes"},
		{Hook: "PreToolUse", Event: "tool-start", Sub: "hook", Arg: "tool-start",
			Why: "which tool a session is running right now"},
		{Hook: "PostToolUse", Event: "tool-end", Sub: "hook", Arg: "tool-end",
			Why: "when that tool finished, so the card stops claiming it"},
		{Hook: "UserPromptSubmit", Event: "prompt", Sub: "hook", Arg: "prompt",
			Why: "you answered, so the card leaves ready"},
		{Hook: "SubagentStart", Event: "subagent-start", Sub: "hook", Arg: "subagent-start",
			Why: "the subagent count going up"},
		{Hook: "SubagentStop", Event: "subagent-end", Sub: "hook", Arg: "subagent-end",
			Why: "and coming back down"},
		// Codex fires this one and no probe here has made it fire, because
		// filling a context to make it happen costs more than the fact is
		// worth. The subcommand behind it reads `trigger` and records the
		// moment either way: a compaction with no trigger word is still a
		// compaction, and every atrium hook already treats a field it did not
		// get as a thing nobody claimed.
		{Hook: "PreCompact", Event: "compacting", Sub: "session", Arg: "compact",
			Why: "the card records the moment this session forgot something"},
		{Hook: "Stop", Event: "turn-end", Sub: "turn", Arg: "end",
			Why:      "a message reaches a session that is sitting idle, and the card moves to ready",
			Optional: true,
			Warn: "this is the only hook that can change what a session does. it answers the " +
				"end of a turn, and answering with a message makes the session keep working on " +
				"it. that is how an idle session is reached at all. codex honors the same block " +
				"and the same stop_hook_active guard claude does, both confirmed against a live " +
				"session, so the loop that guard prevents is prevented here too."},
	},
}

// Targets are the runners atrium knows how to wire, in the order they read.
var Targets = []Target{Claude, Codex}

// TargetFor finds one by id. An unknown id answers Claude, which is what every
// caller meant before there was more than one.
func TargetFor(id string) Target {
	for _, t := range Targets {
		if t.ID == id {
			return t
		}
	}
	return Claude
}

// CodexHooksPath is where Codex keeps its hooks.
//
// $CODEX_HOME when it is set, which is how a second Codex install is kept
// apart, and ~/.codex otherwise. Codex resolves it the same way and prints the
// answer in `codex doctor`.
func CodexHooksPath() (string, error) {
	if home := os.Getenv("CODEX_HOME"); home != "" {
		return filepath.Join(home, "hooks.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex", "hooks.json"), nil
}
