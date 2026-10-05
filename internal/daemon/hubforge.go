package daemon

import (
	"context"
	"net/http"
	"sync"

	"github.com/dovholuknf/atrium/internal/forge"
	"github.com/dovholuknf/atrium/internal/gitsync"
)

// A ROOM ATTACHED TO A HUB NEVER RUNS gh OR bb AND NEVER FETCHES FROM A FORGE HOST. Its pull requests, issues and
// clones come from the hub, which runs the forge and holds the repository in its store. See
// docs/rnd/scm-forge-design.md, Built.
//
// The room has a hub when it was started with a link, attached right now or not. A room with a link whose hub is down
// fails with the sentence the link gives and never falls back to a forge of its own: that would break the rule. A
// room with no link is its own hub and keeps the local forge.

type hubForge struct {
	mu     sync.Mutex
	remote *forge.Remote
}

// SetHubForge gives the room the hub's forge. Called once the link is built, as SetHubGit is. Nil is a room with no hub.
func (d *Daemon) SetHubForge(r *forge.Remote) {
	d.hubForge.mu.Lock()
	d.hubForge.remote = r
	d.hubForge.mu.Unlock()
}

// HubForge is the hub's forge, or nil for a room with no hub.
func (d *Daemon) HubForge() *forge.Remote {
	d.hubForge.mu.Lock()
	defer d.hubForge.mu.Unlock()
	return d.hubForge.remote
}

func (d *Daemon) hubTransport() (http.RoundTripper, error) {
	d.hubGit.mu.Lock()
	t := d.hubGit.transport
	d.hubGit.mu.Unlock()
	if t == nil {
		return nil, errNoHub
	}
	return t()
}

// hubSource is a loopback to one repository of the hub's store, for a git fetch the room makes of what the hub holds.
func (d *Daemon) hubSource(ctx context.Context, name string) (string, func(), error) {
	rt, err := d.hubTransport()
	if err != nil {
		return "", nil, err
	}
	return gitsync.HubLoopback(rt, name)
}

// hubClone is where a room with a hub clones a repository from: the hub holds it first, cloning it from the forge
// when it does not, and the room reads the hub's copy.
func (d *Daemon) hubClone(ctx context.Context, r gitsync.Ref) (string, func(), error) {
	hf := d.HubForge()
	if hf == nil {
		return "", nil, errNoHub
	}
	host := r.Host
	if host == gitsync.DefaultHost {
		host = "github.com"
	}
	name, err := hf.Repo(ctx, host, r.Owner, r.Repo)
	if err != nil {
		return "", nil, err
	}
	return d.hubSource(ctx, name)
}
