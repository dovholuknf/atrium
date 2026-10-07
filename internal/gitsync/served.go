package gitsync

import (
	"context"
	"sort"
	"strings"
	"time"
)

// What a room offers to a reader the hub passes through. See docs/fabric/hub-forge-design.md 3.3.
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
// A live branch that is not a plain branch name is left out rather than written into the config. So is each of
// the clone's own integration branches (see neverServed, and `defaults`, the clone's default branch whatever it is
// called, from DefaultBranches): a card working in the clone's checkout on `main` is
// NOT served `main`, because that is where the operator's unpushed work may be, and the card's own work is
// reached on a branch of its own. Duplicates are written once and the order is sorted, so the same set always
// gives the same configuration.
func ServedHide(live []string, defaults ...string) []string {
	isDefault := map[string]bool{}
	for _, d := range defaults {
		isDefault[strings.TrimSpace(d)] = true
	}
	out := []string{"HEAD", "refs", "!refs/heads/claude/", "refs/heads/claude/main"}
	seen := map[string]bool{}
	var add []string
	for _, b := range live {
		b = strings.TrimSpace(b)
		if neverServed[b] || isDefault[b] || !servableBranch(b) || seen[b] {
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

// neverServed is the branches a live card never makes servable: the hub's own (claude/main, and the room's copy
// of it, hub-main) and the names a clone's default branch usually has.
var neverServed = map[string]bool{"claude/main": true, "hub-main": true, "main": true, "master": true}

// DefaultBranches is the clone's own default branch names: where `origin/HEAD` points (what the forge calls its
// default, `develop` or `trunk` as well as `main`), and `init.defaultBranch`, which is the name a clone with no origin
// made its first branch. A name that cannot be read is simply missing, and neverServed still holds. It runs two
// short git commands and is read on every fetch, like the live branches.
func DefaultBranches(g *Runner, gitDir string) []string {
	if g == nil {
		g = Default
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var out []string
	if ref, err := g.Git(ctx, gitDir, "symbolic-ref", "-q", "refs/remotes/origin/HEAD"); err == nil {
		if b, ok := strings.CutPrefix(strings.TrimSpace(ref), "refs/remotes/origin/"); ok && b != "" {
			out = append(out, b)
		}
	}
	if b, err := g.Git(ctx, gitDir, "config", "--get", "init.defaultBranch"); err == nil {
		if b = strings.TrimSpace(b); b != "" {
			out = append(out, b)
		}
	}
	return out
}

// RepoMatches says whether a card's place is the repository `name` (`<host>/<owner>/<repo>`). The card's repo is
// the folder name its work is in. Its org and host are matched as well when the card recorded them, and a card
// that did not (the launcher did not know) matches on the folder name alone, which is looser: two repositories
// with one folder name can then each serve a branch name of the other's card. Stage 2's scm path records the full
// name, and a card with it is matched exactly.
func RepoMatches(name, host, org, repo string) bool {
	ref, err := ParseName(name)
	if err != nil || repo == "" || !strings.EqualFold(repo, ref.Repo) {
		return false
	}
	if org = strings.TrimSpace(org); org != "" && !strings.EqualFold(org, ref.Owner) {
		return false
	}
	if host = strings.TrimSpace(host); host != "" && canonicalHost(host) != ref.Host {
		return false
	}
	return true
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
