// Package claudeconf reads Claude Code's own permission settings and converts
// them into atrium standing rules.
//
// The point is to start from what you already trust rather than re-approving
// everything through the board. Claude Code writes its allow and deny lists to
// settings.json files, in a pattern language close enough to atrium's that the
// translation is mechanical.
package claudeconf

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Entry is one converted rule.
type Entry struct {
	Tool     string `json:"tool"`
	Pattern  string `json:"pattern"`
	Decision string `json:"decision"`
	Source   string `json:"source"`
	// Broad marks a rule that matches every request for its tool. Those are
	// legitimate (Claude Code writes a bare `WebSearch` to mean "always allow")
	// but the caller should be able to see and refuse them.
	Broad bool `json:"broad"`
}

// Skipped is an entry that could not be translated, kept so the caller can
// report it rather than lose it silently.
type Skipped struct {
	Raw    string `json:"raw"`
	Reason string `json:"reason"`
	Source string `json:"source"`
}

type settingsFile struct {
	Permissions struct {
		Allow []string `json:"allow"`
		Deny  []string `json:"deny"`
	} `json:"permissions"`
}

// SettingsPaths returns the settings files worth reading, narrowest last so a
// project setting can override a global one.
func SettingsPaths(projectDir string) []string {
	var out []string
	if home, err := os.UserHomeDir(); err == nil {
		out = append(out, filepath.Join(home, ".claude", "settings.json"))
	}
	if projectDir != "" {
		out = append(out,
			filepath.Join(projectDir, ".claude", "settings.json"),
			filepath.Join(projectDir, ".claude", "settings.local.json"))
	}
	return out
}

// Load reads every settings file that exists and converts its permission lists.
// A missing file is not an error: most setups have only some of them.
func Load(projectDir string) ([]Entry, []Skipped, error) {
	var (
		entries []Entry
		skipped []Skipped
	)
	for _, path := range SettingsPaths(projectDir) {
		raw, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		var sf settingsFile
		if err := json.Unmarshal(raw, &sf); err != nil {
			skipped = append(skipped, Skipped{Raw: path, Reason: "unreadable json: " + err.Error()})
			continue
		}
		label := filepath.ToSlash(path)
		for _, s := range sf.Permissions.Allow {
			convert(s, "approve", label, &entries, &skipped)
		}
		for _, s := range sf.Permissions.Deny {
			convert(s, "block", label, &entries, &skipped)
		}
	}
	return entries, skipped, nil
}

// toolsWeGate are the tools atrium's hook actually asks about. Converting an
// entry for anything else would add a rule that can never match.
var toolsWeGate = map[string]bool{
	"Bash": true, "Edit": true, "Write": true, "Read": true,
	"MultiEdit": true, "NotebookEdit": true, "Glob": true, "Grep": true,
}

func convert(raw, decision, source string, entries *[]Entry, skipped *[]Skipped) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}
	tool, arg := split(raw)

	if !toolsWeGate[tool] {
		*skipped = append(*skipped, Skipped{
			Raw: raw, Source: source,
			Reason: "atrium does not gate " + tool,
		})
		return
	}

	// A bare tool name with no argument means every use of that tool.
	if arg == "" {
		*entries = append(*entries, Entry{
			Tool: tool, Pattern: "*", Decision: decision, Source: source, Broad: true,
		})
		return
	}

	pattern := translate(tool, arg)
	if pattern == "" {
		*skipped = append(*skipped, Skipped{Raw: raw, Source: source, Reason: "no usable pattern"})
		return
	}
	*entries = append(*entries, Entry{
		Tool: tool, Pattern: pattern, Decision: decision, Source: source,
		Broad: strings.Trim(pattern, "*? ") == "",
	})
}

// split pulls `Tool(arg)` apart. A bare `Tool` yields an empty arg.
func split(raw string) (tool, arg string) {
	open := strings.Index(raw, "(")
	if open < 0 || !strings.HasSuffix(raw, ")") {
		return raw, ""
	}
	return raw[:open], raw[open+1 : len(raw)-1]
}

// translate converts one Claude Code argument into an atrium pattern.
//
//	go build:*        prefix form, the colon means "and anything after"
//	go *              already a glob
//	git branch*       already a glob
//	rm -rf ./build    exact, which prefix matching covers
//	//c/temp/**       a path glob in Claude Code's slash style
func translate(tool, arg string) string {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return ""
	}
	// `cmd:*` and `cmd:` both mean "starts with cmd". Atrium's plain patterns
	// are already prefixes, so the marker is simply dropped. The marker is the
	// LAST colon: an absolute command like `V:/work/.../go.exe build:*` has a
	// drive colon up front that must not be mistaken for it.
	if i := strings.LastIndex(arg, ":"); i >= 0 && isCommandTool(tool) {
		head, tail := arg[:i], strings.TrimPrefix(arg[i+1:], "*")
		if strings.TrimSpace(tail) == "" {
			return strings.TrimSpace(head)
		}
	}
	if isCommandTool(tool) {
		return arg
	}
	return translatePath(arg)
}

func isCommandTool(tool string) bool { return tool == "Bash" }

// translatePath rewrites a path pattern into the shape atrium sees on the
// wire. The hook reports file operations as `<path> <- (...)`, and paths
// arrive with Windows drive letters, so `//c/temp/**` has to become
// `C:/temp/*` to match anything.
func translatePath(arg string) string {
	p := strings.ReplaceAll(arg, `\`, "/")
	// `//c/temp` and `/c/temp` are both the MSYS spelling of `C:/temp`.
	if m := driveRe(p); m != "" {
		p = m
	}
	// A trailing `**` covers everything below, which is what a single atrium
	// star already does, since atrium stars cross separators.
	p = strings.ReplaceAll(p, "**", "*")
	if strings.HasSuffix(p, "/") {
		p += "*"
	}
	// Home-relative and project-relative patterns cannot be resolved to the
	// absolute paths the hook reports, so let them through as globs and accept
	// that they may not match.
	if strings.HasPrefix(p, "~") || strings.HasPrefix(p, "./") {
		return p
	}
	return p
}

// driveRe converts an MSYS style path to a Windows one. Returns "" when the
// path is not in that form.
func driveRe(p string) string {
	trimmed := strings.TrimPrefix(p, "//")
	if trimmed == p {
		trimmed = strings.TrimPrefix(p, "/")
		if trimmed == p {
			return ""
		}
	}
	// Expect a single letter followed by a separator: c/temp/...
	if len(trimmed) < 2 || trimmed[1] != '/' {
		return ""
	}
	letter := trimmed[0]
	isLetter := (letter >= 'a' && letter <= 'z') || (letter >= 'A' && letter <= 'Z')
	if !isLetter {
		return ""
	}
	return strings.ToUpper(string(letter)) + ":" + trimmed[1:]
}
