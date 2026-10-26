package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// toolNames are the toolchains a worker is told the location of, so it does not report "no go on this room".
var toolNames = []string{"go", "node", "python", "git", "gh"}

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
			if st, err := os.Stat(p); err == nil && !st.IsDir() && (runtime.GOOS == "windows" || st.Mode()&0o111 != 0) {
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
		if p := findTool(n, path); p != "" {
			out[n] = filepath.ToSlash(p)
		}
	}
	return out
}

// ToolLocations is toolLocations on this process's PATH, for the room's hello.
func ToolLocations() map[string]string {
	return toolLocations(os.Getenv("PATH"))
}

// toolsLine is the brief line naming the room's tool locations, "" when none are found.
func toolsLine(path string) string {
	var parts []string
	for _, n := range toolNames {
		if p, ok := toolLocations(path)[n]; ok {
			parts = append(parts, n+"="+p)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "Tools on this room, use them rather than reporting one missing: " + strings.Join(parts, ", ")
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
		if findTool(n, runnerPath) == "" && !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	if len(dirs) == 0 {
		return env
	}
	added := strings.Join(dirs, string(os.PathListSeparator))
	if idx < 0 {
		return append(env, "PATH="+added)
	}
	out := append([]string(nil), env...)
	out[idx] = env[idx][:strings.Index(env[idx], "=")+1] + added + string(os.PathListSeparator) + runnerPath
	return out
}
