package land

import (
	"fmt"
	"os/exec"
	"strings"
)

// Duplicates returns one "sha subject" line per commit in upstream..tip whose patch id is already upstream.
func Duplicates(dir, upstream, tip string) ([]string, error) {
	cmd := exec.Command("git", "cherry", "-v", upstream, tip)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git cherry %s %s: %w", upstream, tip, err)
	}
	var dups []string
	for _, l := range strings.Split(string(out), "\n") {
		if rest, ok := strings.CutPrefix(l, "- "); ok {
			dups = append(dups, rest)
		}
	}
	return dups, nil
}
