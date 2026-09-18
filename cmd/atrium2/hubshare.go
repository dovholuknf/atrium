package main

import (
	"fmt"
	"net"
	"strings"

	"github.com/openziti/zrok/v2/environment"
	"github.com/openziti/zrok/v2/environment/env_core"
	zroksdk "github.com/openziti/zrok/v2/sdk/golang/sdk"
)

// Serving the hub's board onto a zrok share, so it can be reached off this box.
//
// The hub's board is one http.Handler already, and both zrok and ziti hand back
// a net.Listener, so reaching it from elsewhere is `http.Serve(listener,
// handler)` on a second listener. This is the same move `internal/daemon/
// overlay_native.go` makes for the v1 daemon, kept here rather than shared
// because the hub is a different binary and the daemon's plumbing does not come
// with it.
//
// The link transport in `transport.go` already drives zrok, but for the ROOM
// link, which is a TCP tunnel carrying raw bytes. The board is HTTP, so this
// asks for a PROXY backend instead. Two different shares for two different
// jobs, on the same account.
//
// PRIVATE IS THE DEFAULT AND PUBLIC IS A DECISION. The hub has no login in
// front of its board. A private share is reachable only by an account holding
// its token, which is the accepted model. A public share is a URL anyone can
// open, in front of a board that reads files, answers permission prompts and
// types into terminals. Turning it on is the caller saying so out loud.

// boardShare is a zrok share the hub is answering its board on.
type boardShare struct {
	root  env_core.Root
	share *zroksdk.Share
	// Address is what the other end is given: an access command for a private
	// share, a frontend URL for a public one.
	Address string
	// Mode is "private" or "public", kept for the log line.
	Mode string
}

// openBoardShare creates a zrok share for the board and returns the listener to
// serve on. The caller serves the board handler on the listener and calls
// release on the way out.
func openBoardShare(mode string) (*boardShare, net.Listener, error) {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		mode = "private"
	}
	if mode != "private" && mode != "public" {
		return nil, nil, fmt.Errorf("board share mode must be private or public, not %q", mode)
	}

	root, err := environment.LoadRoot()
	if err != nil {
		return nil, nil, fmt.Errorf("could not read this machine's zrok environment: %w", err)
	}
	if !root.IsEnabled() {
		return nil, nil, fmt.Errorf("this machine has no zrok environment yet. " +
			"run `zrok enable <your account token>` first")
	}

	req := &zroksdk.ShareRequest{
		// PROXY, not a tunnel: the board is an HTTP server and zrok's proxy
		// backend is what serves one. The link transport uses a tunnel because
		// it carries raw bytes, and that difference is the whole reason this is
		// a second share rather than a reuse of the link one.
		BackendMode: zroksdk.ProxyBackendMode,
		ShareMode:   zroksdk.ShareMode(mode),
		// Names the share on the account. Nothing dials it: this process
		// answers the listener itself. The `atrium-` prefix is what makes a
		// leftover share identifiable on an account that also holds others.
		Target: "atrium-hub-board",
	}

	shr, err := zroksdk.CreateShare(root, req)
	if err != nil {
		return nil, nil, fmt.Errorf("could not create a %s zrok share for the board: %w", mode, err)
	}
	ln, err := zroksdk.NewListener(shr.Token, root)
	if err != nil {
		// The share exists and nothing is answering it, so it goes rather than
		// lingering on the account as a dead entry to clean up by hand.
		_ = zroksdk.DeleteShare(root, shr)
		return nil, nil, fmt.Errorf("the share was created but nothing could answer it: %w", err)
	}

	bs := &boardShare{root: root, share: shr, Mode: mode}
	// The address as data. A public share carries its frontend URLs, a private
	// one has none and the token is what the other end runs.
	if len(shr.FrontendEndpoints) > 0 {
		bs.Address = shr.FrontendEndpoints[0]
	} else {
		bs.Address = "zrok access private " + shr.Token
	}
	return bs, ln, nil
}

// release deletes the share the hub created.
//
// A STOPPED HUB MUST NOT LEAVE ONE BEHIND. A share nothing answers is a dead
// entry on the account, and starting again would make a second, so a night of
// restarts would leave a list somebody tidies by hand.
func (b *boardShare) release() {
	if b == nil || b.share == nil || b.root == nil {
		return
	}
	_ = zroksdk.DeleteShare(b.root, b.share)
	b.share = nil
}
