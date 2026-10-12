package daemon

import (
	"context"
	"log"
	"net/url"
	"strings"

	"github.com/dovholuknf/atrium/internal/gitsync"
)

// scopeHubRemote points the `hub` remote of the launch cwd at THIS room's forwarder, for that worktree only.
//
// Rooms on one machine share a main checkout, whose .git/config has one `hub` url: one room's forwarder. A worker
// launched by another room in a worktree of it would inherit that url and the push guard would refuse it. So the
// worktree gets its own `remote.hub.url` (extensions.worktreeConfig + `git config --worktree`), the path of the
// existing url kept and the scheme, host and port swapped for this room's agent listener. The shared url is never
// touched. A `hub` that is not an atrium forwarder (its path is not /git/hub/) is left alone.
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
	have, err := g.Git(ctx, cwd, "config", "--get", "remote.hub.url")
	have = strings.TrimSpace(have)
	if err != nil || have == "" {
		return // not a git directory, or no hub remote
	}
	u, err := url.Parse(have)
	if err != nil || !strings.HasPrefix(u.Path, "/git/hub/") {
		return
	}
	base := gitsync.StableHubURL(agent, gitsync.Ref{})
	base = base[:strings.Index(base, "/git/")]
	want := base + u.EscapedPath()
	if want == have {
		return
	}
	if _, err := g.Git(ctx, cwd, "config", "extensions.worktreeConfig", "true"); err != nil {
		log.Printf("[atrium] hub remote for %s not scoped: %s", cwd, firstLine(err.Error()))
		return
	}
	if _, err := g.Git(ctx, cwd, "config", "--worktree", "remote.hub.url", want); err != nil {
		log.Printf("[atrium] hub remote for %s not scoped: %s", cwd, firstLine(err.Error()))
	}
}
