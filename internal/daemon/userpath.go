package daemon

import "strings"

// withUserPath replaces the PATH in base with the user's current one, because the room's own copy is as old as
// the room.
func withUserPath(base []string) []string {
	path, ok := currentUserPath()
	if !ok {
		return base
	}
	out := make([]string, 0, len(base)+1)
	for _, kv := range base {
		if i := strings.Index(kv, "="); i > 0 && strings.EqualFold(kv[:i], "PATH") {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "Path="+path)
}
