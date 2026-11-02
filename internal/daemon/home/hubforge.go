package home

import (
	"context"
	"net/http"
	"sync"

	"github.com/dovholuknf/atrium/internal/base/forge"
	"github.com/dovholuknf/atrium/internal/gitsync"
)

// A ROOM ATTACHED TO A HUB NEVER RUNS gh OR bb AND NEVER FETCHES FROM A FORGE HOST. Its pull requests, issues and
// clones come from the hub, which runs the forge and holds the repository in its store. See
// docs/rnd/scm-forge-design.md, Built.
//
// The room has a hub when it was started with a link, attached right now or not. A room with a link whose hub is down
// fails with the sentence the link gives and never falls back to a forge of its own: that would break the rule. A
// room with no link is its own hub and keeps the local forge.

type HubForge struct {
	Mu     sync.Mutex
	Remote *forge.Remote
}

// SetHubForge gives the room the hub's forge. Called once the link is built, as SetHubGit is. Nil is a room with no hub.
func (d *Service) SetHubForge(r *forge.Remote) {
	d.ForgeState.Mu.Lock()
	d.ForgeState.Remote = r
	d.ForgeState.Mu.Unlock()
}

// HubForge is the hub's forge, or nil for a room with no hub.
func (d *Service) HubForge() *forge.Remote {
	d.ForgeState.Mu.Lock()
	defer d.ForgeState.Mu.Unlock()
	return d.ForgeState.Remote
}

func (d *Service) HubTransport() (http.RoundTripper, error) {
	d.HubGit.Mu.Lock()
	t := d.HubGit.Transport
	d.HubGit.Mu.Unlock()
	if t == nil {
		return nil, ErrNoHub
	}
	return t()
}

// hubSource is a loopback to one repository of the hub's store, for a git fetch the room makes of what the hub holds.
func (d *Service) HubSource(ctx context.Context, name string) (string, func(), error) {
	rt, err := d.HubTransport()
	if err != nil {
		return "", nil, err
	}
	return gitsync.HubLoopback(rt, name)
}

// hubClone is where a room with a hub clones a repository from: the hub holds it first, cloning it from the forge
// when it does not, and the room reads the hub's copy.
func (d *Service) HubClone(ctx context.Context, r gitsync.Ref) (string, func(), error) {
	hf := d.HubForge()
	if hf == nil {
		return "", nil, ErrNoHub
	}
	host := r.Host
	if host == gitsync.DefaultHost {
		host = "github.com"
	}
	name, err := hf.Repo(ctx, host, r.Owner, r.Repo)
	if err != nil {
		return "", nil, err
	}
	return d.HubSource(ctx, name)
}
