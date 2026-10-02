package gitsync

import (
	"context"
	"fmt"
	"strings"
)

// The `hub` remote of a clone in the scm folder. See docs/rnd/hub-forge-design.md section 5.2.
//
// THIS FILE IS THE JOIN WITH r-hub-remote. That item builds the room's stable forwarder on the
// agent listener, and the helper that adds "hub (atrium-hub if hub points elsewhere)" to the
// clones the room syncs. When it lands, StableHubURL and addHubRemote here are replaced by calls
// to its helper, and nothing else in the scm clone changes.

// Remote names. atrium's own goes in as `hub`, and as `atrium-hub` where `hub` is taken.
const (
	HubRemote       = "hub"
	HubRemoteAtrium = "atrium-hub"
)

// StableHubURL is the hub remote of a repository on a room whose agent listener is at `agentAddr`
// (host:port): `http://<agent>/git/hub/<host>/<owner>/<repo>.git`.
func StableHubURL(agentAddr string, r Ref) string {
	return "http://" + agentAddr + "/git/hub/" + r.Name() + ".git"
}

// hub adds atrium's remote to a clone and records the outcome on the result. A failure here does
// not fail the clone, which is made and usable: the note says what was not done.
func (c *SCM) hub(ctx context.Context, ref Ref, dir string, res *SCMResult) {
	if c.HubURL == nil {
		res.Note = "the hub remote was not added: this room has no hub forwarder yet"
		return
	}
	url, err := c.HubURL(ref)
	if err != nil {
		res.Note = "the hub remote was not added: " + err.Error()
		return
	}
	name, note, err := c.addHubRemote(ctx, dir, url)
	if err != nil {
		res.Note = "the hub remote was not added: " + err.Error()
		return
	}
	res.Hub, res.Note = name, note
}

// addHubRemote adds `hub` pointing at url. A `hub` that already points at url is left as it is.
// A `hub` pointing anywhere else is left ALONE and reported, and atrium's remote goes in as
// `atrium-hub`.
func (c *SCM) addHubRemote(ctx context.Context, dir, url string) (name, note string, err error) {
	g := c.runner()
	for _, n := range []string{HubRemote, HubRemoteAtrium} {
		have, gerr := g.Git(ctx, dir, "config", "--local", "--get", "remote."+n+".url")
		have = strings.TrimSpace(have)
		switch {
		case gerr != nil || have == "":
			if _, err := g.Git(ctx, dir, "remote", "add", n, url); err != nil {
				return "", "", fmt.Errorf("%s", firstLine(err))
			}
			if n == HubRemoteAtrium {
				note = "this clone's own `hub` remote points somewhere else and was left alone, so atrium's is `atrium-hub`"
			}
			return n, note, nil
		case have == url:
			if n == HubRemoteAtrium {
				note = "this clone's own `hub` remote points somewhere else and was left alone, so atrium's is `atrium-hub`"
			}
			return n, note, nil
		}
	}
	return "", "", fmt.Errorf("both `hub` and `atrium-hub` exist and point elsewhere, so both were left alone")
}
