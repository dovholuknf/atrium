package daemon

import (
	"github.com/dovholuknf/atrium/internal/base/safepath"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// toolNames are the toolchains a worker is told the location of, so it does not report "no go on this room".
var toolNames = []string{"cc", "gcc", "c++", "cmake", "ninja", "make", "go", "node", "npm", "python", "git", "gh", "playwright"}

// findTool resolves name against a PATH value, returning "" when it is not there.
func findTool(name, path string) string {
	exts := []string{""}
	if runtime.GOOS == "windows" {
		exts = []string{".exe", ".cmd", ".bat"}
	}
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			continue
		}
		for _, e := range exts {
			p := filepath.Join(dir, name+e)
			// codeql[go/path-injection] a Stat of name under each PATH entry of the card's own launch env, the lookup its shell makes itself
			if st, err := os.Stat(safepath.NoNUL(p)); err == nil && !st.IsDir() && (runtime.GOOS == "windows" || st.Mode()&0o111 != 0) {
				return p
			}
		}
	}
	return ""
}

// toolLocations maps each tool found on path to where it is.
func toolLocations(path string) map[string]string {
	out := map[string]string{}
	for _, n := range toolNames {
		if p := findToolOrAlt(n, path); p != "" {
			out[n] = filepath.ToSlash(p)
		}
	}
	return out
}

// ToolLocations is toolLocations on this process's PATH, for the room's hello.
func ToolLocations() map[string]string {
	return toolLocations(os.Getenv("PATH"))
}

// toolsLine is the brief line naming the room's tool locations less the refused ones, "" when none are found.
func toolsLine(path string, refused ...string) string {
	var parts []string
	locs := toolLocations(path)
	for _, n := range toolNames {
		if p, ok := locs[n]; ok && !slices.Contains(refused, n) {
			parts = append(parts, n+"="+p)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	line := "Tools on this room, use them rather than reporting one missing: " + strings.Join(parts, ", ")
	env := toolEnv(path)
	for _, k := range []string{"CC", "CXX", "CGO_ENABLED"} {
		if env[k] != "" {
			line += ", " + k + "=" + env[k]
		}
	}
	return line + ". A missing tool is yours to install: put it in a folder on the room's PATH, such as one named above, never move to another room for it, never report it as a blocker."
}

// withToolsLine appends toolsLine to a brief or prompt, once.
func withToolsLine(text string) string {
	line := toolsLine(os.Getenv("PATH"))
	if line == "" || strings.Contains(text, line) {
		return text
	}
	return strings.TrimRight(text, "\n") + "\n\n" + line
}

// withToolDirs prepends to the runner's PATH the directory of every tool the room has and the runner's env lacks.
func withToolDirs(env []string, roomPath string) []string {
	idx, runnerPath := -1, ""
	for i, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && strings.EqualFold(k, "PATH") {
			idx, runnerPath = i, v
		}
	}
	var dirs []string
	seen := map[string]bool{}
	for n, p := range toolLocations(roomPath) {
		d := filepath.FromSlash(filepath.Dir(p))
		if findToolOrAlt(n, runnerPath) == "" && !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	out := append([]string(nil), env...)
	for k, v := range toolEnv(roomPath) {
		if !slices.ContainsFunc(out, func(kv string) bool { return strings.HasPrefix(strings.ToUpper(kv), k+"=") }) {
			out = append(out, k+"="+v)
		}
	}
	if len(dirs) == 0 {
		return out
	}
	added := strings.Join(dirs, string(os.PathListSeparator))
	if idx < 0 {
		return append(out, "PATH="+added)
	}
	out[idx] = env[idx][:strings.Index(env[idx], "=")+1] + added + string(os.PathListSeparator) + runnerPath
	return out
}
