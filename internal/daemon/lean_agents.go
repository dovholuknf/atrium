package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// A lean launch may keep the Agent tool and a NAMED list of the operator's
// agents (`lean_agents`), so a lean review manager can start the reviewers it
// exists to run. Everything else lean drops stays dropped.

// leanAgentTagPrefix marks each named agent a lean card asked to keep, as
// `atrium:agent:<name>`. Tags, like `atrium:mcp:`, so a restart, a resume and
// the keep-alive fork come back with the same list and no column is needed.
const leanAgentTagPrefix = "atrium:agent:"

// agentNameRE is what a `lean_agents` name may be. It is a file name under the
// operator's agents directory, so no separator or dot-dot gets through.
var agentNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// leanAgentsMaxBytes caps the `--agents` value. It rides on the command line,
// and Windows refuses one past 32767 characters, with the settings copy and the
// rest of the lean flags already on it.
const leanAgentsMaxBytes = 14000

// userAgentsDir is the operator's agent definitions, or empty. A variable so a
// test can say where.
var userAgentsDir = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "agents")
}

// cleanAgentNames is names trimmed, without blanks and repeats, sorted.
func cleanAgentNames(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, n := range in {
		n = strings.TrimSpace(n)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// withoutAgentTags is tags without the `atrium:agent:` marks, for a launch that
// names a new list.
func withoutAgentTags(tags []string) []string {
	out := []string{}
	for _, t := range tags {
		if !strings.HasPrefix(strings.TrimSpace(t), leanAgentTagPrefix) {
			out = append(out, t)
		}
	}
	return out
}

// leanDisallowedFor is leanDisallowed, less the Agent tool (and its old name,
// Task) for a launch that names agents to keep.
func leanDisallowedFor(agents []string) []string {
	if len(agents) == 0 {
		return leanDisallowed
	}
	var out []string
	for _, t := range leanDisallowed {
		if t != "Agent" && t != "Task" {
			out = append(out, t)
		}
	}
	return out
}

// leanAgentsFlag is the `--agents` value for the named agents: each one's
// definition file under the operator's ~/.claude/agents, and nothing else in
// that directory.
//
// THE MECHANISM. Claude Code loads agents from the user source, which lean cuts
// with `--setting-sources project,local`, and from `--agents`, a session-only
// source of exactly the JSON it is given. A plugin directory would carry the
// file untouched, but plugin agents are namespaced (`plugin:name`), so the
// launcher's `go-security-reviewer` would stop being that name. A repo's own
// .claude/agents still loads with the project source.
//
// A name with no file is REFUSED, like a missing MCP server: a review manager
// told it has reviewers and started without them finds out mid-task.
func leanAgentsFlag(names []string, readFile func(string) ([]byte, error)) (string, error) {
	dir := userAgentsDir()
	defs := map[string]any{}
	var missing []string
	for _, name := range names {
		if !agentNameRE.MatchString(name) {
			return "", fmt.Errorf("lean_agents name %q is not an agent's file name", name)
		}
		var raw []byte
		var err error
		if dir != "" {
			raw, err = readFile(filepath.Join(dir, name+".md"))
		}
		if dir == "" || err != nil {
			missing = append(missing, name)
			continue
		}
		def, err := agentDefinition(raw)
		if err != nil {
			return "", fmt.Errorf("agent %s: %w", name, err)
		}
		defs[name] = def
	}
	if len(missing) > 0 {
		return "", fmt.Errorf("no agent named %s in %s. lean_agents names a file there, without .md",
			strings.Join(missing, ", "), dir)
	}
	b, err := json.Marshal(defs)
	if err != nil {
		return "", err
	}
	if len(b) > leanAgentsMaxBytes {
		return "", fmt.Errorf("the named agents come to %d bytes, over the %d a launch's command line can carry. name fewer",
			len(b), leanAgentsMaxBytes)
	}
	return string(b), nil
}

// agentDefinition turns an agent file (YAML frontmatter, then the prompt) into
// the shape `--agents` takes. Only the fields that shape a subagent's run are
// carried: description, tools, disallowedTools, model and maxTurns. Skills,
// memory and the rest are what lean drops.
func agentDefinition(raw []byte) (map[string]any, error) {
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	text = strings.TrimPrefix(text, string([]byte{0xEF, 0xBB, 0xBF}))
	front, body := "", text
	if rest, ok := strings.CutPrefix(text, "---\n"); ok {
		if f, b, found := strings.Cut(rest, "\n---"); found {
			front = f
			// Drop the rest of the closing fence's line.
			if i := strings.Index(b, "\n"); i >= 0 {
				b = b[i+1:]
			} else {
				b = ""
			}
			body = b
		}
	}
	var meta map[string]any
	if strings.TrimSpace(front) != "" {
		if err := yaml.Unmarshal([]byte(front), &meta); err != nil {
			return nil, fmt.Errorf("frontmatter does not parse: %w", err)
		}
	}
	desc, _ := meta["description"].(string)
	if strings.TrimSpace(desc) == "" {
		return nil, fmt.Errorf("has no description in its frontmatter")
	}
	def := map[string]any{"prompt": strings.TrimSpace(body), "description": desc}
	for _, k := range []string{"tools", "disallowedTools"} {
		if list := agentList(meta[k]); len(list) > 0 {
			def[k] = list
		}
	}
	if m, ok := meta["model"].(string); ok && strings.TrimSpace(m) != "" {
		def["model"] = strings.TrimSpace(m)
	}
	if n, ok := meta["maxTurns"].(int); ok && n > 0 {
		def["maxTurns"] = n
	}
	return def, nil
}

// agentList is a frontmatter list given as YAML or as "A, B".
func agentList(v any) []string {
	var out []string
	switch x := v.(type) {
	case string:
		for _, p := range strings.Split(x, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
	case []any:
		for _, e := range x {
			if s, ok := e.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
	}
	return out
}
