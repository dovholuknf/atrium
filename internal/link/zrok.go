package link

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/openziti/zrok/v2/environment"
	"github.com/openziti/zrok/v2/environment/env_core"
	zroksdk "github.com/openziti/zrok/v2/sdk/golang/sdk"
)

// The zrok transport, private shares only.
//
// ── why private and not public ───────────────────────────
//
// A public zrok share is a URL anyone can open. Behind it here would be a
// room's whole API: every card, every terminal, the launch endpoint. The rule
// this repository already states for the board applies with more force to a
// link, so there is no public mode in this file and adding one would be a
// decision rather than a feature.
//
// A private share is the opposite shape and the right one. The share exists
// only for whoever holds its token, nothing is published to a frontend, and the
// token is exactly the kind of thing a join string already carries.
//
// ── the direction, which is the same as everywhere else ──
//
//	hub   creates the share and LISTENS on it
//	room  holds the token and DIALS it
//
// `zroksdk.NewListener` and `zroksdk.NewDialer` hand back a `net.Listener` and
// a `net.Conn`, which is the whole interface this package asks a transport for.
// That is why this file is short.
//
// ── what it costs to set up ──────────────────────────────
//
// BOTH MACHINES NEED `zrok enable` ALREADY RUN. zrok is an account and an
// environment before it is a share, and atrium does not create either: it holds
// the NAME of a command that has a credential and never somebody else's
// credential. That is the same line `docs/overlays.md` draws, and it is why the
// hub says what to run rather than trying to do it.

// Zrok reaches a hub over a private zrok share.
type Zrok struct {
	// Token is the private share token. The hub learns it by creating the
	// share; a room is given it in the join string.
	Token string

	once sync.Once
	root env_core.Root
	err  error
	// share is kept so the hub can release it on the way out. Empty on a room,
	// which created nothing and must delete nothing.
	share *zroksdk.Share
}

// Describe is what to log. THE TOKEN IS NOT PRINTED: on a private share it is
// the whole credential, and this string ends up in logs and on the board.
func (z *Zrok) Describe() string { return "a private zrok share" }

// environmentRoot loads this machine's zrok environment, once.
func (z *Zrok) environmentRoot() (env_core.Root, error) {
	z.once.Do(func() {
		root, err := environment.LoadRoot()
		if err != nil {
			z.err = fmt.Errorf("could not read this machine's zrok environment: %w", err)
			return
		}
		if !root.IsEnabled() {
			z.err = errors.New("this machine has no zrok environment yet. " +
				"run `zrok enable <your account token>` first")
			return
		}
		z.root = root
	})
	return z.root, z.err
}

// Share creates the private share and answers with its token.
//
// The hub's side, and the only thing in this file that reaches a network before
// anybody connects. Called before `Listen`.
func (z *Zrok) Share() (string, error) {
	root, err := z.environmentRoot()
	if err != nil {
		return "", err
	}
	shr, err := zroksdk.CreateShare(root, &zroksdk.ShareRequest{
		// TCP TUNNEL, NOT PROXY, and the difference is the point.
		//
		// `proxy` is zrok's HTTP backend: it terminates HTTP and forwards
		// requests, which would put a parser between the hub and the room and
		// break the one property this whole design rests on, that nothing is
		// translated. A tunnel carries bytes, so a websocket is a websocket.
		BackendMode: zroksdk.TcpTunnelBackendMode,
		ShareMode:   zroksdk.PrivateShareMode,
		// Names the share on the account. Nothing dials it: this process
		// answers the listener itself.
		Target: "atrium-hub",
	})
	if err != nil {
		return "", fmt.Errorf("could not create a private zrok share: %w", err)
	}
	z.share = shr
	z.Token = shr.Token
	return shr.Token, nil
}

// Listen answers the share, which is the hub's side.
func (z *Zrok) Listen() (net.Listener, error) {
	root, err := z.environmentRoot()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(z.Token) == "" {
		return nil, errors.New("no share to listen on. call Share first")
	}
	ln, err := zroksdk.NewListener(z.Token, root)
	if err != nil {
		// The share exists and nothing is answering it, so it goes rather than
		// lingering on the account as a dead entry somebody has to clean up.
		z.Release()
		return nil, fmt.Errorf("the share was created but nothing could answer it: %w", err)
	}
	return ln, nil
}

// Release deletes a share the hub created.
//
// A STOPPED HUB MUST NOT LEAVE ONE BEHIND. A private share nothing answers is a
// dead entry on the account, and starting again would make a second, so an
// evening of restarts would leave a list somebody has to tidy by hand.
func (z *Zrok) Release() {
	if z.share == nil || z.root == nil {
		return
	}
	_ = zroksdk.DeleteShare(z.root, z.share)
	z.share = nil
}

// Dial opens one connection, which is the room's side.
func (z *Zrok) Dial(ctx context.Context) (net.Conn, error) {
	root, err := z.environmentRoot()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(z.Token) == "" {
		return nil, errors.New("no share token. the join string carries it")
	}
	conn, err := zroksdk.NewDialer(z.Token, root)
	if err != nil {
		return nil, fmt.Errorf("could not reach that private share. "+
			"is the hub running, and has this account been granted access: %w", err)
	}
	return conn, nil
}

// ZrokAuthenticated is `Hub.Authenticated` for this transport.
//
// True for the same reason as ziti's: a private share is reachable only by an
// account holding its token, and zrok has already decided that before a byte
// arrives. The token is the credential, which is why `Describe` refuses to
// print it.
func ZrokAuthenticated(net.Conn) bool { return true }
