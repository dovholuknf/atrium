package daemon

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/store"
)

// A CLOSE'S STASH: the work in a worktree a close was told to stash, held on the hub instead of on any room. Design:
// docs/rnd/card-lifecycle-design.md Interview Q4 and Q5. The close itself is internal/api/close.go.
//
// A stash is atrium's word, not `git stash`. The dirty files go into one WIP commit on top of the branch, the branch
// is pushed to the hub as `stash/<card short id>/<branch>`, and the local name for it is dropped again. The push is
// the room's, with a token minted for the card for this one push, so a card whose session has ended can still be
// stashed. The rules of every other push hold: git.push has to be hub, and a card that runs outside code does not
// push.

// StashTo is api.Server.Stash.
func (d *Daemon) StashTo(ctx context.Context, card, dir, branch string) (string, string, error) {
	t, err := d.st.Get(card)
	if err != nil || t == nil {
		return "", "", fmt.Errorf("no card %s on this room", card)
	}
	if d.st.GitPush() != store.GitPushHub {
		return "", "", fmt.Errorf("this room does not let cards push to the hub (git.push is none)")
	}
	if isOutsideCode(LaunchRequest{}, t) {
		return "", "", fmt.Errorf("this card runs code that is not the room's own, so it does not push")
	}
	base := d.HubRemoteBase()
	if base == "" || d.hubGit.cards == nil {
		return "", "", fmt.Errorf("this room has no hub remote to stash to")
	}
	branch = strings.TrimSpace(branch)
	if branch == "" {
		branch = "detached"
	}
	name := "stash/" + cardShortID(card) + "/" + branch
	if why := gitsync.CheckPushBranch(name); why != "" {
		return "", "", fmt.Errorf("%s", why)
	}
	g := gitsync.Default
	dir = filepath.FromSlash(dir)

	if out, err := g.Git(ctx, dir, "status", "--porcelain"); err == nil && strings.TrimSpace(out) != "" {
		if _, err := g.Git(ctx, dir, "add", "-A"); err != nil {
			return "", "", fmt.Errorf("the dirty files did not stage: %w", err)
		}
		args := []string{}
		if who, _ := g.Git(ctx, dir, "config", "user.email"); strings.TrimSpace(who) == "" {
			args = append(args, "-c", "user.name=atrium", "-c", "user.email=atrium@localhost")
		}
		args = append(args, "commit", "--no-verify", "--quiet", "-m", "atrium stash: work in progress from "+card)
		if _, err := g.Git(ctx, dir, args...); err != nil {
			return "", "", fmt.Errorf("the WIP commit was not made: %w", err)
		}
	}
	if _, err := g.Git(ctx, dir, "branch", "-f", name, "HEAD"); err != nil {
		return "", "", fmt.Errorf("the stash branch was not made: %w", err)
	}
	defer g.Git(context.Background(), dir, "branch", "-D", name)

	// The token is good only while the card is live, and a card being closed is let push this once.
	d.hubGit.stashing.Store(card, true)
	defer d.hubGit.stashing.Delete(card)
	tok, err := d.hubGit.cards.Mint(card)
	if err != nil {
		return "", "", err
	}
	defer d.revokeHubGit(card, time.Time{})
	hubName := gitsync.HubNameOf(ctx, g, dir, d.cloneRoots()...)
	report, err := gitsync.PushToHub(ctx, g, dir, base, tok, name, hubName)
	if err != nil {
		if strings.TrimSpace(report) != "" {
			return "", "", fmt.Errorf("the push did not land:\n%s", report)
		}
		return "", "", err
	}
	return name, hubName, nil
}

// cardShortID is the first eight characters of a card's id, without the room's tag.
func cardShortID(id string) string {
	if i := strings.LastIndex(id, "~"); i >= 0 {
		id = id[i+1:]
	}
	if len(id) > 8 {
		id = id[:8]
	}
	return id
}
