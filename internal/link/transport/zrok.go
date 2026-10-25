package transport

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/zitictx"
	"github.com/openziti/sdk-golang/ziti"
	"github.com/openziti/zrok/v2/environment"
	"github.com/openziti/zrok/v2/environment/env_core"
	"github.com/openziti/zrok/v2/rest_client_zrok/metadata"
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
// credential. That is the same line `docs/fabric/overlays.md` draws, and it is why the
// hub says what to run rather than trying to do it.

// ZrokAPI is what this file asks of zrok. The real one is the SDK on this machine's environment, and a test hands in
// a fake so no controller is needed.
type ZrokAPI interface {
	// Exists says whether a share is still on the environment. An error is "could not tell", never "gone".
	Exists(token string) (bool, error)
	Create() (string, error)
	Delete(token string) error
	Listen(token string) (net.Listener, error)
	Dial(token string) (net.Conn, error)
	// Access is `zrok access private`: a private share is not in this environment's network until it is asked for.
	Access(token string) error
}

// Zrok reaches a hub over a private zrok share.
type Zrok struct {
	// Token is the private share token. The hub learns it by creating the
	// share; a room is given it in the join string.
	Token string
	// API, when set, stands in for the SDK.
	API ZrokAPI

	once sync.Once
	root env_core.Root
	err  error
	// gate backs dials off after a failure. See dialgate.go.
	gate dialGate
	mu   sync.Mutex
	accessed string
}

// One shared ziti context, since zrok's NewDialer builds one per call, never closes it, and leaks :443 sockets.
type sdkZrok struct {
	root     env_core.Root
	identity string
}

func (s sdkZrok) context() (ziti.Context, error) { return zitictx.SharedFile("zrok", s.identity) }

func (s sdkZrok) Exists(token string) (bool, error) {
	_, err := zroksdk.GetShareDetail(s.root, token)
	if err == nil {
		return true, nil
	}
	var gone *metadata.GetShareDetailNotFound
	if errors.As(err, &gone) {
		return false, nil
	}
	return false, err
}

func (s sdkZrok) Create() (string, error) {
	shr, err := zroksdk.CreateShare(s.root, &zroksdk.ShareRequest{
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
		return "", err
	}
	return shr.Token, nil
}

func (s sdkZrok) Access(token string) error {
	_, err := zroksdk.CreateAccess(s.root, &zroksdk.AccessRequest{ShareToken: token})
	return err
}

func (s sdkZrok) Delete(token string) error {
	return zroksdk.DeleteShare(s.root, &zroksdk.Share{Token: token})
}

func (s sdkZrok) Listen(token string) (net.Listener, error) {
	zc, err := s.context()
	if err != nil {
		return nil, err
	}
	return zc.ListenWithOptions(token, &ziti.ListenOptions{ConnectTimeout: 30 * time.Second, WaitForNEstablishedListeners: 1})
}

func (s sdkZrok) Dial(token string) (net.Conn, error) {
	zc, err := s.context()
	if err != nil {
		return nil, err
	}
	return zc.DialWithOptions(token, &ziti.DialOptions{ConnectTimeout: 30 * time.Second})
}

func (z *Zrok) api() (ZrokAPI, error) {
	if z.API != nil {
		return z.API, nil
	}
	root, err := z.environmentRoot()
	if err != nil {
		return nil, err
	}
	id, err := root.ZitiIdentityNamed(root.EnvironmentIdentityName())
	if err != nil {
		return nil, fmt.Errorf("could not find this machine's zrok identity: %w", err)
	}
	return sdkZrok{root: root, identity: id}, nil
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

// Share answers with the private share's token: the saved one when it is still on the environment, a new one only
// when it is gone.
//
// The hub's side, and the only thing in this file that reaches a network before anybody connects. Called before
// `Listen`. A SHARE IS REUSED BECAUSE A ROOM'S JOIN STRING CARRIES ITS TOKEN: a new share on every hub start would cut
// every zrok room off at each deploy. When zrok cannot say whether the saved share is there, that is an error and
// nothing is reserved, since a second share would orphan the first.
func (z *Zrok) Share(saved string) (string, error) {
	api, err := z.api()
	if err != nil {
		return "", err
	}
	saved = strings.TrimSpace(saved)
	if saved != "" {
		ok, err := api.Exists(saved)
		if err != nil {
			return "", fmt.Errorf("could not ask zrok whether the share this hub saved is still there: %w", err)
		}
		if ok {
			z.setToken(saved)
			return saved, nil
		}
	}
	token, err := api.Create()
	if err != nil {
		return "", fmt.Errorf("could not create a private zrok share: %w", err)
	}
	z.setToken(token)
	return token, nil
}

func (z *Zrok) setToken(t string) {
	z.mu.Lock()
	z.Token = t
	z.mu.Unlock()
}

// Reserved is the share this hub is on, empty before Share has answered.
func (z *Zrok) Reserved() string {
	z.mu.Lock()
	defer z.mu.Unlock()
	return z.Token
}

// Listen answers the share, which is the hub's side.
func (z *Zrok) Listen() (net.Listener, error) {
	api, err := z.api()
	if err != nil {
		return nil, err
	}
	token := z.Reserved()
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("no share to listen on. call Share first")
	}
	ln, err := api.Listen(token)
	if err != nil {
		return nil, fmt.Errorf("the share exists and nothing could answer it: %w", err)
	}
	return ln, nil
}

// Retire deletes a share from the environment. It is the only way a hub's share ends: a stopped hub leaves it for the
// next start to reuse. See `atrium rooms retire-share`.
func (z *Zrok) Retire(token string) error {
	api, err := z.api()
	if err != nil {
		return err
	}
	return api.Delete(token)
}

// Dial opens one connection, which is the room's side.
func (z *Zrok) Dial(ctx context.Context) (net.Conn, error) {
	if wait, ok := z.gate.allow(); !ok {
		return nil, fmt.Errorf("the zrok controller is not answering. not dialling again for %s",
			wait.Round(time.Second))
	}
	api, err := z.api()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(z.Token) == "" {
		return nil, errors.New("no share token. the join string carries it")
	}
	if err := z.access(api); err != nil {
		z.gate.failed()
		return nil, fmt.Errorf("could not get access to that private share: %w", err)
	}
	conn, err := api.Dial(z.Token)
	if err != nil {
		z.gate.failed()
		return nil, fmt.Errorf("could not reach that private share. "+
			"is the hub running, and has this account been granted access: %w", err)
	}
	z.gate.worked()
	return conn, nil
}

func (z *Zrok) access(api ZrokAPI) error {
	z.mu.Lock()
	done := z.accessed == z.Token
	z.mu.Unlock()
	if done {
		return nil
	}
	if err := api.Access(z.Token); err != nil {
		return err
	}
	z.mu.Lock()
	z.accessed = z.Token
	z.mu.Unlock()
	return nil
}

// Hammering says dials to zrok are failing fast. Shown on the room's row.
func (z *Zrok) Hammering() bool { return z.gate.hammering() }

// Close does nothing: a share outlives the process that answers it.
func (z *Zrok) Close() {}

// ZrokAuthenticated is `Hub.Authenticated` for this transport.
//
// The legacy path's word, as `ZitiAuthenticated` is: `OverlayAuthenticated` is
// what the hub uses now.
//
// True for the same reason as ziti's: a private share is reachable only by an
// account holding its token, and zrok has already decided that before a byte
// arrives. The token is the credential, which is why `Describe` refuses to
// print it.
func ZrokAuthenticated(net.Conn) bool { return true }
