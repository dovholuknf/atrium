package gitsync

import (
	"context"
	"strings"
)

// What the room does to a clone's remotes when it syncs it. docs/rnd/hub-forge-design.md 5.1 and 5.2.
//
//   - `hub` is added, pointing at the room's stable forwarder. A `hub` the operator made that points ELSEWHERE is
//     left alone and reported, and `atrium-hub` is added in its place.
//   - On a clone atrium MADE, `remote.origin.pushurl` is set to OriginPushURL, so even a script that bypasses
//     every hook cannot push to the forge. On a clone the operator made it is not touched: that takes the
//     operator's yes, and there is no way to give one yet, so the sync says so.

// ensureRemotes makes the clone's remotes right and says, in a sentence for the operator, anything worth saying.
// forwarder is the stable URL for this repository. madeHere is a clone this sync just created.
func ensureRemotes(ctx context.Context, g *Runner, clone, forwarder string, madeHere bool) string {
	var notes []string
	if _, note, err := ensureHubRemote(ctx, g, clone, forwarder); err != nil {
		notes = append(notes, "the hub remote was not added: "+err.Error())
	} else if note != "" {
		notes = append(notes, note)
	}

	// A clone room-git.ps1 made by push has no origin at all, so there is no forge to push to and nothing to guard,
	// and it carries atrium.clone=made so it is not mistaken for the operator's.
	hasOrigin := false
	if u, err := g.Git(ctx, clone, "config", "--get", "remote.origin.url"); err == nil && strings.TrimSpace(u) != "" {
		hasOrigin = true
	}
	marked := false
	if !madeHere {
		if m, err := g.Git(ctx, clone, "config", "--get", cfgMade); err == nil && strings.TrimSpace(m) == "made" {
			marked, madeHere = true, true
		}
	}

	switch {
	case marked && !hasOrigin:
		// nothing to guard
	case madeHere:
		if _, err := g.Git(ctx, clone, "config", "remote.origin.pushurl", OriginPushURL); err != nil {
			notes = append(notes, "could not guard origin against pushes: "+firstLine(err))
		}
	default:
		if cur, err := g.Git(ctx, clone, "config", "--get", "remote.origin.pushurl"); err != nil || strings.TrimSpace(cur) != OriginPushURL {
			notes = append(notes, "origin is not guarded against pushes on this clone, which the operator made. that takes the operator's yes, and there is no way to give one yet")
		}
	}
	return strings.Join(notes, ". ")
}
