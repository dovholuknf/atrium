package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/dovholuknf/atrium/internal/guard"
)

// `atrium hook --event pre-tool-use`: the guard. Rules as data, a real shell
// parser, and the hub remote this room already knows. See internal/guard and
// docs/changes/r-hooks-all-in-atrium.md.
//
// OFF unless ATRIUM_GUARD says on. Switching a machine over is the operator's
// call, made in settings.json, not something an upgrade does.
//
// Unlike every other atrium hook, a broken guard DENIES: a broken override
// file, or a check that panics. What fails open is what Claude Code decides
// for us: a hook that times out, and a payload that never arrived.

// preToolUseEvent is the --event value that selects the guard.
const preToolUseEvent = "pre-tool-use"

// guardRulesEnv names a rules file that replaces the built-in rules.
const guardRulesEnv = "ATRIUM_GUARD_RULES"

// guardOn reads ATRIUM_GUARD. Anything but on, 1, true or yes is off.
func guardOn(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "on", "1", "true", "yes":
		return true
	}
	return false
}

// guardBlockedWhenBroken are the tools refused while the override file is
// broken. Read, Grep and the rest stay usable so the file can be looked at.
var guardBlockedWhenBroken = map[string]bool{
	"Bash": true, "PowerShell": true, "Write": true, "Edit": true,
	"NotebookEdit": true, "Task": true, "Agent": true,
}

// guardHook is the bytes to print for a PreToolUse payload, or nil to print
// nothing and let the call through.
func guardHook(stdin []byte, env guard.Env) (out []byte) {
	defer func() {
		if p := recover(); p != nil {
			out = guardOutput(guard.Decision{Action: "deny",
				Reason: fmt.Sprintf("atrium's guard failed on this call (%v), so it is refused. Rephrase it, or have the user run it.", p)})
		}
	}()
	if !guardOn(env.Getenv("ATRIUM_GUARD")) {
		return nil
	}
	var in guard.Input
	if len(stdin) == 0 || json.Unmarshal(stdin, &in) != nil {
		return nil
	}
	rules, err := guardRules(env)
	if err != nil {
		path := strings.TrimSpace(env.Getenv(guardRulesEnv))
		if !guardBlockedWhenBroken[in.ToolName] || guardFixesRules(in, path) {
			return nil
		}
		if path == "" {
			return guardOutput(guard.Decision{Action: "deny",
				Reason: fmt.Sprintf("atrium's built-in guard rules do not load (%v), so this is refused. "+
					"Set ATRIUM_GUARD=off until atrium is rebuilt.", err)})
		}
		return guardOutput(guard.Decision{Action: "deny",
			Reason: fmt.Sprintf("atrium's guard rules in %s do not load (%v), so this is refused. "+
				"Fix that file with Write or Edit, or unset %s to use the built-in rules.", path, err, guardRulesEnv)})
	}
	return guardOutput(rules.Evaluate(in, env))
}

// guardRules is the override file when one is named, the built-in rules
// otherwise.
func guardRules(env guard.Env) (*guard.Rules, error) {
	path := strings.TrimSpace(env.Getenv(guardRulesEnv))
	if path == "" {
		return guard.Builtin()
	}
	raw, err := env.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return guard.Load(raw)
}

// guardFixesRules says whether this call is a Write or Edit of the broken
// rules file itself, which is the one edit that must get through.
func guardFixesRules(in guard.Input, path string) bool {
	if in.ToolName != "Write" && in.ToolName != "Edit" {
		return false
	}
	a, b := strings.TrimSpace(in.ToolInput.FilePath), strings.TrimSpace(path)
	if a == "" || b == "" {
		return false
	}
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		// Their file systems ignore case, so any spelling is the same file.
		return strings.EqualFold(a, b)
	}
	return a == b
}

func guardOutput(d guard.Decision) []byte {
	if d.Action == "" {
		return nil
	}
	b, err := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName":            "PreToolUse",
		"permissionDecision":       d.Action,
		"permissionDecisionReason": d.Reason,
	}})
	if err != nil {
		return nil
	}
	return b
}
