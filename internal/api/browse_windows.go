//go:build windows

package api

import (
	"os"
	"path/filepath"
)

// browseRoots lists what "up from everything" means.
//
// Windows has no single root, so the top of the tree is the set of drives that
// answer. Probing A through Z is crude, instant, and stays correct when a stick
// is plugged in.
func browseRoots() []browseEntry {
	var out []browseEntry
	for c := 'A'; c <= 'Z'; c++ {
		path := string(c) + ":/"
		if _, err := os.Stat(path); err != nil {
			continue
		}
		out = append(out, browseEntry{Name: string(c) + ":", Path: filepath.ToSlash(path)})
	}
	return out
}
