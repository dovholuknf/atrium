package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// A lean launch may keep the Agent tool and a NAMED list of the operator's
// agents (`lean_agents`), and the Skill tool and a named list of skills
// (`lean_skills`), so a lean review manager can start the reviewers it exists to
// run. Everything else lean drops stays dropped.

// leanAgentTagPrefix and leanSkillTagPrefix mark each named agent and skill a
// lean card asked to keep, as `atrium:agent:<name>` and `atrium:skill:<name>`.
// Tags, like `atrium:mcp:`, so a restart, a resume and the keep-alive fork come
// back with the same lists and no column is needed.
const (
	leanAgentTagPrefix = "atrium:agent:"
	leanSkillTagPrefix = "atrium:skill:"
)

// leanPluginName is the session-only plugin the named files ride in. Claude Code
// namespaces a plugin's agents and skills, so they are started as
// `atrium:<name>`.
const leanPluginName = "atrium"

// leanKit is what a lean launch keeps beyond the default: the operator's agents
// and skills, by name.
type leanKit struct {
	Agents, Skills []string
}

func (k leanKit) empty() bool { return len(k.Agents) == 0 && len(k.Skills) == 0 }

// nameRE is what a `lean_agents` or `lean_skills` name may be. It is a file or
// directory name under the operator's ~/.claude, so no separator or dot-dot gets
// through.
var nameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// The operator's definitions. Variables so a test can say where.
var (
	userAgentsDir = func() string { return userClaudeSub("agents") }
	userSkillsDir = func() string { return userClaudeSub("skills") }
)

func userClaudeSub(sub string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", sub)
}

// leanPluginRoot is where the plugin directories are written: in atrium's own
// state, set from the database's directory when a daemon starts. Never a
// worktree, which has to stay clean for the cull.
var leanPluginRoot string

// cleanKitNames is names trimmed, without blanks and repeats, sorted.
func cleanKitNames(in []string) []string {
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

// withoutKitTags is tags without the agent and skill marks, for a launch that
// names new lists.
func withoutKitTags(tags []string) []string {
	out := []string{}
	for _, t := range tags {
		c := strings.TrimSpace(t)
		if !strings.HasPrefix(c, leanAgentTagPrefix) && !strings.HasPrefix(c, leanSkillTagPrefix) {
			out = append(out, t)
		}
	}
	return out
}

// leanDisallowedFor is leanDisallowed, less the Agent tool (and its old name,
// Task) for a launch that names agents, and less Skill for one that names skills.
func leanDisallowedFor(k leanKit) []string {
	var out []string
	for _, t := range leanDisallowed {
		if (t == "Agent" || t == "Task") && len(k.Agents) > 0 {
			continue
		}
		if t == "Skill" && len(k.Skills) > 0 {
			continue
		}
		out = append(out, t)
	}
	return out
}

// leanPluginDir writes the session-only plugin for kit and returns its
// directory, for `--plugin-dir`.
//
// THE MECHANISM. Claude Code loads agents and skills from the user source, which
// lean cuts with `--setting-sources project,local`. `--agents` takes them inline
// only, and the real agents are tens of KB where a Windows command line takes
// 32 KB in all. `--plugin-dir` loads a directory for one session, so the named
// files are copied into one, byte for byte and following links, and nothing else
// of ~/.claude comes with them. The price is the namespace: a plugin's agents
// and skills are `atrium:<name>`.
//
// The directory is named by a hash of what it holds and is written once. Two
// cards with the same kit share one, and a live session's directory is never
// rewritten under it. A restart, a resume and the keep-alive fork all come here
// again from the card's tags and find or rebuild it.
//
// A name with no file is REFUSED, like a missing MCP server: a review manager
// told it has reviewers and started without them finds out mid-task.
func leanPluginDir(k leanKit) (string, error) {
	if leanPluginRoot == "" {
		return "", fmt.Errorf("atrium has no state directory to write the lean plugin in")
	}
	files := map[string][]byte{
		".claude-plugin/plugin.json": []byte(`{"name":"` + leanPluginName +
			`","description":"named agents and skills for a lean atrium session","version":"1.0.0"}` + "\n"),
	}
	var missing []string
	for _, name := range k.Agents {
		if !nameRE.MatchString(name) {
			return "", fmt.Errorf("lean_agents name %q is not an agent's file name", name)
		}
		b, err := os.ReadFile(filepath.Join(userAgentsDir(), name+".md"))
		if err != nil {
			missing = append(missing, "agent "+name)
			continue
		}
		files["agents/"+name+".md"] = b
	}
	for _, name := range k.Skills {
		if !nameRE.MatchString(name) {
			return "", fmt.Errorf("lean_skills name %q is not a skill's directory name", name)
		}
		if err := readTree(filepath.Join(userSkillsDir(), name), "skills/"+name, files); err != nil {
			missing = append(missing, "skill "+name)
		}
	}
	if len(missing) > 0 {
		return "", fmt.Errorf("no %s in %s or %s. lean_agents names a file there without .md, lean_skills a directory",
			strings.Join(missing, ", no "), userAgentsDir(), userSkillsDir())
	}
	dir := filepath.Join(leanPluginRoot, pluginHash(files))
	manifest := filepath.Join(dir, ".claude-plugin", "plugin.json")
	if _, err := os.Stat(manifest); err == nil {
		return dir, nil
	}
	if err := os.MkdirAll(leanPluginRoot, 0o755); err != nil {
		return "", fmt.Errorf("could not write the lean plugin: %w", err)
	}
	tmp, err := os.MkdirTemp(leanPluginRoot, "tmp-")
	if err != nil {
		return "", fmt.Errorf("could not write the lean plugin: %w", err)
	}
	defer os.RemoveAll(tmp)
	for rel, b := range files {
		p := filepath.Join(tmp, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			return "", err
		}
	}
	if err := os.Rename(tmp, dir); err != nil {
		// Another launch wrote the same directory first, which holds the same bytes.
		if _, serr := os.Stat(manifest); serr != nil {
			return "", fmt.Errorf("could not write the lean plugin: %w", err)
		}
	}
	return dir, nil
}

// readTree reads every file under src into files, under rel, following links.
// A skill is a directory with a SKILL.md, so the top of one without it is an error.
func readTree(src, rel string, files map[string][]byte) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", src)
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		p := filepath.Join(src, e.Name())
		st, err := os.Stat(p)
		if err != nil {
			return err
		}
		if st.IsDir() {
			if err := readTree(p, path.Join(rel, e.Name()), files); err != nil {
				return err
			}
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		files[path.Join(rel, e.Name())] = b
	}
	if _, ok := files[path.Join(rel, "SKILL.md")]; !ok && strings.Count(rel, "/") == 1 {
		return fmt.Errorf("%s has no SKILL.md", src)
	}
	return nil
}

// pluginHash names a plugin directory by its contents.
func pluginHash(files map[string][]byte) string {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		fmt.Fprintf(h, "%s\x00%d\x00", n, len(files[n]))
		h.Write(files[n])
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}
