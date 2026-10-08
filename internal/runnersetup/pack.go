package runnersetup

import "path/filepath"

// PackLayout is where one runner reads the operator's agents and skills from, for `atrium room setup`'s agent pack. Measured,
// not assumed:
//
//   - claude reads ~/.claude/agents/*.md and ~/.claude/skills/<name>/SKILL.md. CLAUDE_CONFIG_DIR moves the folder.
//   - gemini (gemini-cli 0.60.0, `Storage.getUserAgentsDir` and `getUserSkillsDir` in its bundle) reads ~/.gemini/agents/*.md
//     and ~/.gemini/skills/<name>/SKILL.md. GEMINI_CLI_HOME moves the folder that holds .gemini.
//   - codex reads ~/.codex/skills/<name>/SKILL.md, and has no markdown agents (its own are TOML in config), so a codex pack is
//     skills alone. CODEX_HOME moves the folder.
type PackLayout struct {
	// Dir is the runner's folder, forward slashes.
	Dir string
	// Agents and Skills say which of the pack's two kinds of file the runner reads.
	Agents, Skills bool
}

// PackLayoutFor is the layout of a runner's pack, and false for a runner with no pack adapter. Only env's Home and its variables
// are read, never the disk.
func PackLayoutFor(id string, env Env) (PackLayout, bool) {
	switch id {
	case "claude":
		dir := filepath.Join(env.Home, ".claude")
		if d := env.lookup("CLAUDE_CONFIG_DIR"); d != "" {
			dir = d
		}
		return PackLayout{Dir: filepath.ToSlash(dir), Agents: true, Skills: true}, true
	case "gemini":
		return PackLayout{Dir: filepath.ToSlash(geminiDir(env)), Agents: true, Skills: true}, true
	case "codex":
		dir := filepath.Join(env.Home, ".codex")
		if d := env.lookup("CODEX_HOME"); d != "" {
			dir = d
		}
		return PackLayout{Dir: filepath.ToSlash(dir), Skills: true}, true
	}
	return PackLayout{}, false
}
