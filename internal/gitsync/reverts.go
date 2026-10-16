package gitsync

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// REVERTS ON THE MIRROR, for the item lifecycle's efficacy (docs/rnd/item-lifecycle-design.md section 12.1 measure 6): a
// commit whose message says `This reverts commit <sha>` is a revert of that sha, which is git's own words and so observed.
// One pass reads them all, and the caller matches the shas of its items, so a hub with many items asks git once.

// Revert is one commit that reverts another.
type Revert struct {
	// SHA is the reverting commit, Reverts the one it undoes, At when it was committed.
	SHA     string    `json:"sha"`
	Reverts string    `json:"reverts"`
	At      time.Time `json:"at"`
}

var revertsRe = regexp.MustCompile(`This reverts commit ([0-9a-f]{40}|[0-9a-f]{64})`)

// revertScanMax bounds how far back a scan reads: a revert of something that landed this far back is not within the days
// efficacy watches.
const revertScanMax = 3000

// RevertScan lists the reverts on claude/main and main of the repository `name`. A repository the hub does not hold is no
// reverts and no error.
func (s *Store) RevertScan(ctx context.Context, name string) ([]Revert, error) {
	dir, err := s.bareFor(name)
	if err != nil || dir == "" {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, readBound)
	defer cancel()
	seen := map[string]bool{}
	var out []Revert
	for _, ref := range []string{ClaudeMainRef, MainRef} {
		if _, err := s.git(ctx, dir, "rev-parse", "--verify", "-q", ref+"^{commit}"); err != nil {
			continue
		}
		raw, err := s.git(ctx, dir, "log", "-n", strconv.Itoa(revertScanMax), "--grep=This reverts commit", "--format=%H%x1f%cI%x1f%B%x1e", ref)
		if err != nil {
			return out, err
		}
		for _, rec := range strings.Split(raw, "\x1e") {
			f := strings.SplitN(strings.TrimLeft(rec, "\n"), "\x1f", 3)
			if len(f) < 3 || seen[f[0]] {
				continue
			}
			m := revertsRe.FindStringSubmatch(f[2])
			if m == nil {
				continue
			}
			at, err := time.Parse(time.RFC3339, strings.TrimSpace(f[1]))
			if err != nil {
				continue
			}
			seen[f[0]] = true
			out = append(out, Revert{SHA: f[0], Reverts: m[1], At: at.UTC()})
		}
	}
	return out, nil
}
