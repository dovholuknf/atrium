//go:build !windows

package api

// browseRoots lists what "up from everything" means. Everywhere but Windows
// there is exactly one root.
func browseRoots() []browseEntry {
	return []browseEntry{{Name: "/", Path: "/"}}
}
