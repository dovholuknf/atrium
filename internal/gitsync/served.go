package gitsync

import (
	"sort"
	"strings"
)

// What a room offers to a reader the hub passes through. See docs/rnd/hub-forge-design.md 3.3.
//
// THE SERVED SET IS THE ROOM'S OWN CONFIGURATION, NOT THE HUB'S MANNERS. A want outside it is refused by the
// room's git, so a reader cannot ask for refs/stash, refs/notes/* or a private branch by going through the hub:
// upload-pack in protocol v0 refuses a want that is not an advertised tip (no tip-sha, no reachable-sha), and
// what it advertises is what these hideRefs leave.

// ServedHide is the uploadpack.hideRefs values for one request, in order, given the branches of the room's LIVE
// cards for that repository. It is a pure function: nothing is read from the repository or written to it.
//
//	HEAD, refs                 hide everything, HEAD too (the clone's HEAD may be any branch at all)
//	!refs/heads/claude/        then claude/* back: what the room's workers made
//	refs/heads/claude/main     then claude/main out again: it came from the hub in the first place
//	!refs/heads/<b>            then one per live card's branch, under the name it has on the room
//
// A live branch that is not a plain branch name is left out rather than written into the config, and so is
// claude/main, which stays unserved whatever a card is on. Duplicates are written once and the order is sorted,
// so the same set always gives the same configuration.
func ServedHide(live []string) []string {
	out := []string{"HEAD", "refs", "!refs/heads/claude/", "refs/heads/claude/main"}
	seen := map[string]bool{}
	var add []string
	for _, b := range live {
		b = strings.TrimSpace(b)
		if b == "claude/main" || !servableBranch(b) || seen[b] {
			continue
		}
		seen[b] = true
		add = append(add, b)
	}
	sort.Strings(add)
	for _, b := range add {
		out = append(out, "!refs/heads/"+b)
	}
	return out
}

// servableBranch says whether a card's branch name may become a hideRefs value. A name git would not take as a
// branch, one that starts with `refs/`, or one carrying anything that means something to hideRefs or to a config
// line is not written into it.
func servableBranch(b string) bool {
	if b == "" || len(b) > 200 || strings.HasPrefix(b, "refs/") || strings.HasPrefix(b, "-") ||
		strings.HasPrefix(b, "/") || strings.HasSuffix(b, "/") || strings.HasSuffix(b, ".lock") ||
		strings.Contains(b, "//") || strings.Contains(b, "..") || strings.Contains(b, "@{") {
		return false
	}
	for _, c := range b {
		switch {
		case c < 0x21 || c == 0x7f:
			return false
		case strings.ContainsRune("~^:?*[\\\"'`$%!", c):
			return false
		}
	}
	for _, seg := range strings.Split(b, "/") {
		if seg == "" || seg == "." || strings.HasPrefix(seg, ".") {
			return false
		}
	}
	return true
}
