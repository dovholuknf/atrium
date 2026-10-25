// Package userenv brings a child's environment up to date with the user's own.
//
// A long-lived process holds the environment it started with, so a tool installed or a variable set afterwards is
// missing from everything it launches. On Windows the registry holds the current one.
package userenv

import "strings"

// Refresh returns base with PATH replaced by the user's current one and any other user variable base lacks added.
// A variable base already has is kept, because it may have been set on purpose. Off Windows it returns base.
func Refresh(base []string) []string {
	vars := current()
	if len(vars) == 0 {
		return base
	}
	have := make(map[string]bool, len(base))
	out := make([]string, 0, len(base)+len(vars))
	for _, kv := range base {
		i := strings.Index(kv, "=")
		if i > 0 {
			name := strings.ToUpper(kv[:i])
			if _, isPath := vars["PATH"]; isPath && name == "PATH" {
				continue
			}
			have[name] = true
		}
		out = append(out, kv)
	}
	for name, v := range vars {
		if name == "PATH" {
			out = append(out, "Path="+v)
		} else if !have[name] {
			out = append(out, name+"="+v)
		}
	}
	return out
}
