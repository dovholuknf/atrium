package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/dovholuknf/atrium/internal/claudeconf"
	"github.com/dovholuknf/atrium/internal/store"
)

// A LEAN WORKER is a claude session started with only what a worker needs: its
// brief, the repo, the tools, atrium's hooks and two MCP servers, atrium-control
// and mercurius.
//
// A worker inherits none of its launcher's conversation, yet by default it boots
// with the operator's whole setup: the global CLAUDE.md, auto-memory, every user
// and claude.ai skill, every agent type, the operator's prompt-time hook
// reminders and every MCP server in the harness's config. Measured on 2.1.283,
// that is ~40k tokens on the first request of a worker in a worktree, and ~27k
// of it is setup the worker never reads. See docs/lean-workers-design.md.
//
// What lean changes, and nothing else:
//
//   - `--setting-sources project,local`. The user source is dropped, and with it
//     the global CLAUDE.md, user skills and agents, claude.ai-synced skills and
//     the output style. The project source stays, so a repo that checks in a
//     CLAUDE.md or .claude/settings.json keeps it.
//   - `--settings`, a copy of the user settings.json that keeps what a worker
//     needs from it: permissions, env (the gate switch lives there) and every
//     hook, except the operator's own SessionStart and UserPromptSubmit hooks,
//     which print into the context. Atrium's own reporters on those two events
//     stay. The Stop hook is added when the operator has none.
//   - `--strict-mcp-config --mcp-config`, the harness's own MCP config cut down
//     to atrium-control and mercurius plus whatever the launch named.
//   - `--disallowedTools`, the tools a worker has no use for.
//   - `--append-system-prompt`, the worker rules the dropped CLAUDE.md files
//     used to carry.
//   - CLAUDE_CODE_DISABLE_AUTO_MEMORY=1, so the operator's memory index is not
//     loaded. The brief carries what the worker needs to know.

// LeanTag marks a card launched lean, so a reopen or a resume starts it lean
// again. A tag rather than a column, the way OriginAgentTag is.
const LeanTag = "atrium:lean"

// leanMCPTagPrefix marks each extra MCP server a lean card asked for, as
// `atrium:mcp:<name>`.
const leanMCPTagPrefix = "atrium:mcp:"

// leanDefaultServers are the MCP servers every lean worker keeps when the
// runner's config has them: atrium-control to report, and mercurius for
// reviews. One missing from the config is left out, not refused, because
// nobody asked for it by name.
var leanDefaultServers = []string{"atrium-control", "mercurius"}

func isLeanDefault(name string) bool {
	for _, d := range leanDefaultServers {
		if name == d {
			return true
		}
	}
	return false
}

// leanDisallowed are the tools a lean worker is started without. Each is either
// the orchestrator's job (Agent, Workflow, the cron and remote tools), a thing
// a worker in its own worktree must not do (EnterWorktree, plan mode), or a
// channel that goes to the human instead of the launcher (AskUserQuestion,
// PushNotification). A worker asks with atrium_say.
var leanDisallowed = []string{
	"Agent", "Task", "Workflow", "Skill", "NotebookEdit",
	"CronCreate", "CronDelete", "CronList", "RemoteTrigger", "ScheduleWakeup",
	"PushNotification", "DesignSync", "ShareOnboardingGuide", "SendFeedback", "ReportFindings",
	"SendMessage", "ListAgents", "EnterWorktree", "ExitWorktree",
	"EnterPlanMode", "ExitPlanMode", "AskUserQuestion",
}

// leanSystemPrompt is what a lean worker is told in place of the CLAUDE.md files
// it no longer loads. Only rules a worker breaks without being told.
const leanSystemPrompt = `You are a worker launched by another agent through atrium. Your BRIEF.md is your whole task.
- Report with atrium_report when you finish, get blocked, or need an answer. atrium_say your launcher for a question. A plain reply in the terminal reaches nobody.
- Never commit on claude/main or main. Commit on your own branch.
- Never restart atrium, the hub or a room, and never deploy, unless the brief says to.
- Commit messages: one short subject line. No body unless asked, no Co-Authored-By or other trailer.
- Go builds go to build.claude/.
- Do not edit CLAUDE.md files.
- Ask with atrium_say rather than guessing on anything consequential.`

// leanOptions says whether a launch is lean and which extra MCP servers it
// keeps, from the request for a new launch and from the card's tags for a
// reopen.
func leanOptions(req LaunchRequest, task *store.Task) (lean bool, mcp []string) {
	lean = req.Lean
	mcp = append(mcp, req.MCP...)
	if task != nil && hasTag(task.Tags, LeanTag) {
		lean = true
		for _, t := range task.Tags {
			if name, ok := strings.CutPrefix(strings.TrimSpace(t), leanMCPTagPrefix); ok {
				mcp = append(mcp, name)
			}
		}
	}
	return lean, cleanNames(mcp)
}

// leanTags are the tags that record a lean launch on its card.
func leanTags(mcp []string) []string {
	out := []string{LeanTag}
	for _, m := range mcp {
		out = append(out, leanMCPTagPrefix+m)
	}
	return out
}

// mergeTags is base with add appended, dropping repeats and keeping order.
func mergeTags(base, add []string) []string {
	out := append([]string{}, base...)
	for _, t := range add {
		if !hasTag(out, t) {
			out = append(out, t)
		}
	}
	return out
}

func cleanNames(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, n := range in {
		n = strings.TrimSpace(n)
		if n == "" || isLeanDefault(n) || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// leanArgs rewrites a claude launch's arguments to start lean. userSettings is
// the operator's settings.json, read by the caller, and stopHook is the Stop
// hook command to add when those settings have none.
//
// The harness's own MCP, settings-source and settings flags are taken out and
// replaced, and the lean flags go IN FRONT, so the positional prompt stays last.
func leanArgs(args []string, userSettings []byte, stopHook string, mcp []string,
	readFile func(string) ([]byte, error)) ([]string, error) {

	var kept, configs []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--mcp-config":
			// Takes one or more values, up to the next flag.
			for i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				configs = append(configs, args[i])
			}
		case "--setting-sources", "--settings":
			i++
		case "--strict-mcp-config":
		default:
			kept = append(kept, args[i])
		}
	}
	servers, err := leanServers(configs, mcp, readFile)
	if err != nil {
		return nil, err
	}
	settings, err := leanSettings(userSettings, stopHook)
	if err != nil {
		return nil, err
	}
	out := []string{
		"--setting-sources", "project,local",
		"--settings", settings,
		"--strict-mcp-config", "--mcp-config", servers,
		"--disallowedTools", strings.Join(leanDisallowed, ","),
		"--append-system-prompt", leanSystemPrompt,
	}
	return append(out, kept...), nil
}

// leanServers is the `--mcp-config` value for a lean launch: the default
// servers and the named extras, taken from the harness's own config files.
//
// A named server that no config holds is REFUSED, the way a model a runner
// cannot take is: a worker told it has a server and started without it finds
// out mid-task.
func leanServers(configs, extra []string, readFile func(string) ([]byte, error)) (string, error) {
	all := map[string]json.RawMessage{}
	for _, c := range configs {
		raw := []byte(c)
		if !strings.HasPrefix(strings.TrimSpace(c), "{") {
			b, err := readFile(c)
			if err != nil {
				return "", fmt.Errorf("lean launch could not read the runner's MCP config %s: %w", c, err)
			}
			raw = b
		}
		var doc struct {
			MCPServers map[string]json.RawMessage `json:"mcpServers"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			return "", fmt.Errorf("lean launch could not parse the runner's MCP config %s: %w", c, err)
		}
		for k, v := range doc.MCPServers {
			all[k] = v
		}
	}
	keep := map[string]json.RawMessage{}
	for _, name := range leanDefaultServers {
		if v, ok := all[name]; ok {
			keep[name] = v
		}
	}
	var missing []string
	for _, name := range extra {
		v, ok := all[name]
		if !ok {
			missing = append(missing, name)
			continue
		}
		keep[name] = v
	}
	if len(missing) > 0 {
		have := make([]string, 0, len(all))
		for k := range all {
			have = append(have, k)
		}
		sort.Strings(have)
		return "", fmt.Errorf("no MCP server named %s in this runner's config. it has: %s",
			strings.Join(missing, ", "), strings.Join(have, ", "))
	}
	b, err := json.Marshal(map[string]any{"mcpServers": keep})
	return string(b), err
}

// leanContextHooks are the hook events whose output lands in the context.
var leanContextHooks = []string{"SessionStart", "UserPromptSubmit"}

// leanSettings is the `--settings` value for a lean launch, built from the
// operator's settings.json. Unknown keys are dropped, which is the point: the
// status line, the output style and the attribution text cost tokens or do
// nothing for a worker.
func leanSettings(user []byte, stopHook string) (string, error) {
	doc := map[string]json.RawMessage{}
	if len(strings.TrimSpace(string(user))) > 0 {
		if err := json.Unmarshal(user, &doc); err != nil {
			return "", fmt.Errorf("lean launch could not parse the user settings: %w", err)
		}
	}
	out := map[string]any{
		// Empty attribution: no Co-Authored-By trailer and no reminder about one.
		"attribution": map[string]string{"commit": "", "pr": ""},
	}
	for _, k := range []string{"permissions", "env", "model", "effortLevel"} {
		if v, ok := doc[k]; ok {
			out[k] = v
		}
	}
	hooks := map[string]any{}
	if raw, ok := doc["hooks"]; ok {
		if err := json.Unmarshal(raw, &hooks); err != nil {
			return "", fmt.Errorf("lean launch could not parse the user hooks: %w", err)
		}
	}
	for _, ev := range leanContextHooks {
		if entries, ok := hooks[ev].([]any); ok {
			if kept := atriumOnly(entries); len(kept) > 0 {
				hooks[ev] = kept
			} else {
				delete(hooks, ev)
			}
		}
	}
	// stopHookCommand is empty exactly when the operator's settings already
	// register atrium's Stop hook, and that one was copied above.
	if stopHook != "" {
		stop, _ := hooks["Stop"].([]any)
		hooks["Stop"] = append(stop, map[string]any{
			"matcher": "",
			"hooks":   []any{map[string]any{"type": "command", "command": stopHook}},
		})
	}
	out["hooks"] = hooks
	b, err := json.Marshal(out)
	return string(b), err
}

// atriumOnly keeps the matcher entries' hooks that are atrium's own reporters.
func atriumOnly(entries []any) []any {
	var out []any
	for _, e := range entries {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		list, _ := m["hooks"].([]any)
		var kept []any
		for _, h := range list {
			hm, _ := h.(map[string]any)
			cmd, _ := hm["command"].(string)
			if isAtriumReporter(cmd) {
				kept = append(kept, h)
			}
		}
		if len(kept) == 0 {
			continue
		}
		c := map[string]any{}
		for k, v := range m {
			c[k] = v
		}
		c["hooks"] = kept
		out = append(out, c)
	}
	return out
}

// isAtriumReporter recognises atrium's own hook commands by their subcommand,
// the same spellings stopHookCommand looks for.
func isAtriumReporter(cmd string) bool {
	for _, sub := range []string{" session --event ", " hook --event ", " turn --event "} {
		if strings.Contains(cmd, sub) {
			return true
		}
	}
	return false
}

// leanEnv is what a lean launch adds to the runner's environment.
func leanEnv(atrium map[string]string) {
	atrium["CLAUDE_CODE_DISABLE_AUTO_MEMORY"] = "1"
}

// readUserSettings is the operator's settings.json, or nothing. A variable so
// a test can say what it wants.
var readUserSettings = func() []byte {
	p, err := claudeconf.UserSettingsPath()
	if err != nil {
		return nil
	}
	b, _ := os.ReadFile(p)
	return b
}
