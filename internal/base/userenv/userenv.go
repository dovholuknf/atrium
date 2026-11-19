// Package userenv brings a child's environment up to date with the user's own.
//
// A long-lived process holds the environment it started with, so a tool installed or a variable set afterwards is
// missing from everything it launches. On Windows the registry holds the current one.
package userenv

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Refresh returns base with the user's current PATH ahead of its own and any other user variable base lacks added.
// A variable base already has is kept, because it may have been set on purpose. Off Windows it returns base.
func Refresh(base []string) []string {
	return refresh(base, current())
}

func refresh(base []string, vars map[string]string) []string {
	if len(vars) == 0 {
		return base
	}
	have := make(map[string]bool, len(base))
	out := make([]string, 0, len(base)+len(vars))
	pathAt, pathName, oldPath := -1, "Path", ""
	for _, kv := range base {
		i := strings.Index(kv, "=")
		if i > 0 {
			name := strings.ToUpper(kv[:i])
			if name == "PATH" {
				pathAt, pathName, oldPath = len(out), kv[:i], kv[i+1:]
			}
			have[name] = true
		}
		out = append(out, kv)
	}
	if v, ok := vars["PATH"]; ok {
		kv := pathName + "=" + mergePath(v, oldPath)
		if pathAt >= 0 {
			out[pathAt] = kv
		} else {
			out = append(out, kv)
		}
	}
	for name, v := range vars {
		if name != "PATH" && !have[name] {
			out = append(out, name+"="+v)
		}
	}
	return out
}

// mergePath is fresh, then each entry of old it lacks: what the process was started with, a launcher's own tools
// among it, is never dropped.
func mergePath(fresh, old string) string {
	seen := map[string]bool{}
	var keep []string
	for _, p := range append(filepath.SplitList(fresh), filepath.SplitList(old)...) {
		k := strings.ToLower(strings.TrimRight(p, `\/`))
		if p == "" || seen[k] {
			continue
		}
		seen[k] = true
		keep = append(keep, p)
	}
	return strings.Join(keep, string(os.PathListSeparator))
}

// LookPath is exec.LookPath, then the user's current PATH and the folder installers put tools in, so a tool installed
// after this process started is still found.
func LookPath(name string) (string, error) {
	home, _ := os.UserHomeDir()
	return lookPath(name, freshPath, home)
}

func lookPath(name string, fresh func() string, home string) (string, error) {
	p, err := exec.LookPath(name)
	if err == nil || strings.ContainsAny(name, `/\`) {
		return p, err
	}
	dirs := filepath.SplitList(fresh())
	if home != "" {
		dirs = append(dirs, filepath.Join(home, ".local", "bin"))
	}
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		if q, e := exec.LookPath(filepath.Join(dir, name)); e == nil {
			return q, nil
		}
	}
	return "", err
}
