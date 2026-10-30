package daemon

import (
	"context"
	"log"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/small"
)

// scopeHubRemote leaves the launch cwd with exactly one effective `remote.hub.url`, this room's forwarder.
//
// Rooms on one machine share a main checkout, whose .git/config has one `hub` url: one room's forwarder. git reads
// config.worktree on top of it and remote.hub.url is multi-valued, so a second url in a worktree does not replace the
// first: git fetches from the first and pushes to both, and the push guard refuses both. A worktree cannot override
// the shared value, so the shared value has to go. When the shared url is another room's forwarder it is moved, not
// lost: every worktree of the repo (the main one included) that has no url of its own gets it in its config.worktree
// (extensions.worktreeConfig + `git config --worktree`), then it is unset from .git/config, and the launch cwd gets
// this room's url, the path kept and the scheme, host and port swapped for this room's agent listener. The other
// room's sessions still see exactly their url. The same pass repairs a worktree that already has the double url.
// A `hub` that is not an atrium forwarder (its path is not /git/hub/) is left alone.
//
// Best effort: a failure logs and never fails the launch.
func (d *Daemon) scopeHubRemote(cwd string) {
	agent := d.agentAddr()
	if cwd == "" || agent == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitsync.CommandBound)
	defer cancel()
	g := gitsync.Default
	fail := func(err error) {
		log.Printf("[atrium] hub remote for %s not scoped: %s", cwd, small.FirstLine(err.Error()))
	}
	have, err := g.Git(ctx, cwd, "config", "--get", "remote.hub.url")
	have = strings.TrimSpace(have)
	if err != nil || have == "" {
		// After a move the url lives in the main worktree's config.worktree, so a worktree added later sees none.
		list, lerr := g.Git(ctx, cwd, "worktree", "list", "--porcelain")
		if lerr != nil {
			return // not a git directory
		}
		main, _ := strings.CutPrefix(strings.TrimSpace(strings.SplitN(list, "\n", 2)[0]), "worktree ")
		if main == "" || sameDir(main, cwd) {
			return
		}
		have, _ = g.Git(ctx, main, "config", "--worktree", "--get", "remote.hub.url")
		have = strings.TrimSpace(have)
		if have == "" {
			return // no hub remote
		}
	}
	// The shared url is the one in .git/config. Two values there is not a state we made, so fall back to the
	// effective one.
	shared, _ := g.Git(ctx, cwd, "config", "--local", "--get-all", "remote.hub.url")
	shared = strings.TrimSpace(shared)
	inLocal := shared != ""
	if shared == "" || strings.Contains(shared, "\n") {
		shared = have
	}
	u, err := url.Parse(shared)
	if err != nil || !strings.HasPrefix(u.Path, "/git/hub/") {
		return
	}
	base := gitsync.StableHubURL(agent, gitsync.Ref{})
	base = base[:strings.Index(base, "/git/")]
	want := base + u.EscapedPath()
	if _, err := g.Git(ctx, cwd, "config", "extensions.worktreeConfig", "true"); err != nil {
		fail(err)
		return
	}
	if want != shared {
		// Park the shared url in every worktree that has none, then drop it from the shared config.
		list, err := g.Git(ctx, cwd, "worktree", "list", "--porcelain")
		if err != nil {
			fail(err)
			return
		}
		for _, line := range strings.Split(list, "\n") {
			wt, ok := strings.CutPrefix(strings.TrimSpace(line), "worktree ")
			if !ok || wt == "" || sameDir(wt, cwd) {
				continue
			}
			if own, _ := g.Git(ctx, wt, "config", "--worktree", "--get", "remote.hub.url"); strings.TrimSpace(own) != "" {
				continue
			}
			if _, err := g.Git(ctx, wt, "config", "--worktree", "remote.hub.url", shared); err != nil {
				fail(err)
				return // never unset the shared url while a worktree still depends on it
			}
		}
		if _, err := g.Git(ctx, cwd, "config", "--local", "--unset-all", "remote.hub.url"); inLocal && err != nil {
			fail(err)
			return
		}
	}
	if _, err := g.Git(ctx, cwd, "config", "--worktree", "remote.hub.url", want); err != nil {
		fail(err)
	}
}

func sameDir(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}
