package land

import (
	"context"
	"fmt"
	"strings"

	"github.com/dovholuknf/atrium/internal/base/gitexec"
)

// Duplicates returns one "sha subject" line per commit in upstream..tip whose patch id is already upstream.
func Duplicates(dir, upstream, tip string) ([]string, error) {
	out, err := gitexec.Run(context.Background(), gitexec.Opts{Dir: dir}, gitexec.Cherry{Verbose: true, Upstream: upstream, Head: tip})
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
