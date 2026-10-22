package gitsync

import (
	"context"
	"strings"
)

// What the room does to a clone's remotes when it syncs it. docs/fabric/hub-forge-design.md 5.1 and 5.2.
//
//   - `hub` is added, pointing at the room's stable forwarder. A `hub` the operator made that points ELSEWHERE is
//     left alone and reported, and `atrium-hub` is added in its place.
//   - `origin` is not touched, except that the retired guard (retiredOriginPushURL) is taken off. It lived in the
//     shared .git/config, so it broke the operator's own `git push` in every worktree of the clone.

// retiredOriginPushURL is the `remote.origin.pushurl` atrium once set on clones it made. Only this exact value is
// removed, never one the operator set.
const retiredOriginPushURL = "atrium-refused://origin-push"

// ensureRemotes makes the clone's remotes right and says, in a sentence for the operator, anything worth saying.
// forwarder is the stable URL for this repository.
func ensureRemotes(ctx context.Context, g *Runner, clone, forwarder string) string {
	var notes []string
	if _, note, err := ensureHubRemote(ctx, g, clone, forwarder); err != nil {
		notes = append(notes, "the hub remote was not added: "+err.Error())
	} else if note != "" {
		notes = append(notes, note)
	}
	if err := dropRetiredOriginPushURL(ctx, g, clone); err != nil {
		notes = append(notes, "could not remove atrium's old guard from origin: "+firstLine(err))
	}
	return strings.Join(notes, ". ")
}

// dropRetiredOriginPushURL unsets the `remote.origin.pushurl` values that are exactly retiredOriginPushURL and leaves
// every other value where it is.
func dropRetiredOriginPushURL(ctx context.Context, g *Runner, clone string) error {
	out, err := g.Git(ctx, clone, "config", "--local", "--get-all", "remote.origin.pushurl")
	if err != nil {
		return nil
	}
	found := false
	for _, v := range strings.Split(out, "\n") {
		if strings.TrimSpace(v) == retiredOriginPushURL {
			found = true
		}
	}
	if !found {
		return nil
	}
	_, err = g.Git(ctx, clone, "config", "--local", "--unset-all", "remote.origin.pushurl", "^atrium-refused://origin-push$")
	return err
}
