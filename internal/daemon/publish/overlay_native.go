package publish

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/base/edge"
	"github.com/dovholuknf/atrium/internal/zitictx"
	"github.com/openziti/zrok/v2/environment/env_core"
	zroksdk "github.com/openziti/zrok/v2/sdk/golang/sdk"
)

// Serving the board onto an overlay directly, with no child process.
//
// The first version of this drove the CLIs: start `zrok share`, scrape the
// address out of its log lines, and let it proxy back to the board's own port.
// That worked and it was three guesses deep. Which flags exist is a moving
// target, the address is not printed in any documented format, and the traffic
// took an extra hop through a proxy to reach a handler already running in this
// process.
//
// Both SDKs hand back a net.Listener. The board is one http.Handler
// (`d.ap.Handler()`), so the whole thing is `http.Serve(listener, handler)`:
// atrium answers on the overlay itself. No process to supervise, no output to
// parse, the address comes back as data, and nothing is proxied anywhere.
//
// The two SDKs agree on one `openziti/sdk-golang`, so embedding both pulls one
// copy rather than two that disagree.

// overlayKind names a way out. Both are configured the same way and differ
// only in how a listener is obtained.
type OverlayKind string

const (
	OverlayZrok OverlayKind = "zrok"
	OverlayZiti OverlayKind = "ziti"
)

// OverlayState is what the board shows for one overlay.
type OverlayState struct {
	Kind OverlayKind `json:"kind"`
	// Found is the resolved path of the CLI, which is still needed for the
	// setup step. Empty means enabling or enrolling cannot be done from here,
	// though an environment or identity set up elsewhere still serves.
	Found string `json:"found"`
	// Running is whether the board is being answered on this overlay.
	Running bool `json:"running"`
	// Since is when it started, RFC3339, empty when it is not running.
	Since string `json:"since"`
	// Address is whatever somebody on the other end has to be given. A public
	// zrok share has a URL, a private one has a token to access, and a ziti
	// service has neither because reaching it is a matter of policy.
	Address string `json:"address,omitempty"`
	// Share is what the zrok account calls this share, which is the name it is
	// listed under in the console and the one `zrok2 overview` prints.
	//
	// NOT THE SAME AS THE ADDRESS, and that is why it has to be shown. A public
	// share's address is a URL on a frontend and the share itself is a token,
	// so somebody looking at their account to find what atrium made had the
	// address, the console had the token, and nothing on the board connected
	// the two. The operator asked for it by name: "show me the share name so i
	// can find it".
	//
	// Empty for ziti, which makes no share: reaching that service is a matter
	// of policy on the network rather than anything atrium holds.
	Share string `json:"share,omitempty"`
	// Output is kept for the shape the board already draws. Nothing writes to
	// it now that no child process is involved.
	Output []string `json:"output"`
	// Err is why the last attempt failed, when it did.
	Err string `json:"err,omitempty"`
}

// nat returns the listener state for one overlay, creating it on first use.
func (d *Service) Nat(kind OverlayKind) *Native {
	d.NatsMu.Lock()
	defer d.NatsMu.Unlock()
	if d.Nats[kind] == nil {
		d.Nats[kind] = &Native{Kind: kind}
	}
	return d.Nats[kind]
}

// native is one listener atrium is serving the board on.
type Native struct {
	Mu       sync.Mutex
	Kind     OverlayKind
	Ln       net.Listener
	Srv      *http.Server
	Since    time.Time
	Address  string
	Err      string
	Stopping bool
	// shareToken is what zrok gave back, kept so the share can be deleted on
	// the way out rather than left behind on the account.
	ShareToken string
}

// serveOn takes ownership of a listener and answers the board on it.
//
// The server is the same handler the local board uses. It is a second
// http.Server rather than a second handler, because a listener is what these
// SDKs return and http.Serve wants one.
func (n *Native) ServeOn(ln net.Listener, handler http.Handler, address, shareToken string) {
	srv := &http.Server{Handler: handler}

	n.Mu.Lock()
	n.Ln, n.Srv, n.Since = ln, srv, time.Now()
	n.Address, n.ShareToken, n.Err, n.Stopping = address, shareToken, "", false
	n.Mu.Unlock()

	go func() {
		err := srv.Serve(ln)
		n.Mu.Lock()
		defer n.Mu.Unlock()
		if n.Srv != srv {
			// Replaced by a later start.
			return
		}
		n.Ln, n.Srv = nil, nil
		if err != nil && err != http.ErrServerClosed && !n.Stopping {
			n.Err = err.Error()
			log.Printf("[atrium] the %s listener stopped: %v", n.Kind, err)
			return
		}
		log.Printf("[atrium] the %s listener stopped", n.Kind)
	}()
}

func (n *Native) Running() bool {
	n.Mu.Lock()
	defer n.Mu.Unlock()
	return n.Srv != nil
}

func (n *Native) State(found string) OverlayState {
	n.Mu.Lock()
	defer n.Mu.Unlock()
	st := OverlayState{
		Kind: n.Kind, Found: found, Running: n.Srv != nil,
		Address: n.Address, Share: n.ShareToken, Err: n.Err,
	}
	if n.Srv != nil {
		st.Since = n.Since.Format(time.RFC3339)
	}
	return st
}

// stop closes the listener and, for zrok, releases the share.
//
// Deleting the share matters: a reserved one lingers on the account and an
// ephemeral one holds a name. Leaving them behind turns a board somebody
// opened twice into a list of dead shares they have to clean up by hand.
func (n *Native) Stop(root env_core.Root) {
	n.Mu.Lock()
	srv, token := n.Srv, n.ShareToken
	n.Stopping = true
	// What it published died with it, and the error belonged to a listener
	// that is over.
	n.Address, n.Err, n.ShareToken = "", "", ""
	n.Mu.Unlock()

	if srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
	// The share is always released, including one with a chosen address.
	//
	// Reserving does not mean "do not unshare". In zrok v2 it is a property of
	// a NAME, held by the controller, and unshare checks it: an ephemeral name
	// is deleted with the share and a reserved one is kept. So releasing here
	// is what frees the ziti resources and leaves the reservation intact.
	//
	// A private share is the same story by a different route. The token is
	// requested rather than owned, and deleting the share puts it back on the
	// shelf, so the next start asks for the same one and gets it.
	//
	// Keeping the share alive instead would leak: a stopped atrium would leave
	// a share nothing is answering, and starting again would try to create a
	// second one on an address already taken.
	if token != "" && root != nil {
		if err := zroksdk.DeleteShare(root, &zroksdk.Share{Token: token}); err != nil {
			log.Printf("[atrium] could not release the zrok share %s: %v", token, err)
		}
	}
}

// startZrokNative creates a share and serves the board on it.
func (d *Service) StartZrokNative(cfg ZrokConfig) error {
	d.OverlayStep("zrok", "env", "reading this machine's zrok environment")
	root, err := d.ZrokRoot()
	if err != nil {
		return d.OverlayFailed("zrok", fmt.Errorf("could not read the zrok environment: %w", err))
	}
	if !root.IsEnabled() {
		return d.OverlayFailed("zrok", fmt.Errorf("this machine has no zrok environment yet"))
	}

	mode := strings.TrimSpace(cfg.Mode)
	if mode != "public" && mode != "private" {
		return d.OverlayFailed("zrok",
			fmt.Errorf("share mode must be public or private, not %q", mode))
	}

	req := &zroksdk.ShareRequest{
		// proxy is what serves an HTTP backend, which is what the board is.
		BackendMode: zroksdk.ProxyBackendMode,
		ShareMode:   zroksdk.ShareMode(mode),
		// The target is only used to name the share. Nothing dials it: this
		// process answers the listener itself.
		Target: d.DefaultBackend(),
	}
	// A stable address, expressed the way each mode expresses it.
	//
	// `ShareRequest.Reserved` is NOT set, and setting it did nothing: the
	// field exists on the struct and no code in zrok reads it. Reserving in v2
	// is a property of a NAME held by the controller, set with
	// `zrok2 modify name <name> --reserved`, and unshare consults it to decide
	// whether to keep the name. Asking for the address here is a separate
	// thing from that name being reserved, and both are needed for an address
	// that survives.
	if mode == "private" {
		if t := strings.TrimSpace(cfg.ShareToken); t != "" {
			req.PrivateShareToken = t
		}
	} else {
		sel, err := d.BoardShareName(&cfg)
		if err != nil {
			return d.OverlayFailed("zrok", err)
		}
		req.NameSelections = []zroksdk.NameSelection{sel}
	}

	// The slow one, and the one that fails. Everything above this is local.
	d.OverlayStep("zrok", "create", "asking the zrok instance for a "+mode+" share")
	shr, err := zroksdk.CreateShare(root, req)
	if err != nil {
		return d.OverlayFailed("zrok", ZrokSays("could not put the board on a zrok share", err))
	}

	d.OverlayStep("zrok", "listen", "opening the listener that answers it")
	ln, err := zroksdk.NewListener(shr.Token, root)
	if err != nil {
		// The share exists and nothing is answering it, so it goes.
		_ = zroksdk.DeleteShare(root, shr)
		return d.OverlayFailed("zrok",
			ZrokSays("the share was created but nothing could answer it", err))
	}

	// The address as data. A public share carries its frontend URLs; a private
	// one has none, and the token is what the other end runs.
	address := "zrok access private " + shr.Token
	if len(shr.FrontendEndpoints) > 0 {
		address = shr.FrontendEndpoints[0]
	}

	d.OverlayStep("zrok", "done", address)
	log.Printf("[atrium] serving the board on a %s zrok share at %s", mode, address)
	// The share's own frontend is the one name it answers besides loopback, which is
	// where a private share's `zrok access private` proxy listens.
	d.Nat(OverlayZrok).ServeOn(ln, edge.Named(d.AuthGuard(d.ap.Handler()), shr.FrontendEndpoints...), address, shr.Token)
	return nil
}

// startZitiNative serves the board on a ziti service, using the identity as
// configured.
//
// Nothing is hosted or forwarded. The board answers on the service itself, so
// there is no tunneler and no address on this machine for anything to reach.
func (d *Service) StartZitiNative(cfg ZitiConfig) error {
	id := strings.TrimSpace(cfg.Identity)
	if id == "" {
		return fmt.Errorf("no identity: enroll one, or point atrium at an identity file")
	}
	service := strings.TrimSpace(cfg.Service)
	if service == "" {
		return fmt.Errorf("no service: name the ziti service this board answers")
	}

	// Two steps rather than three, because a ziti listener is bound rather than
	// created: there is nothing to ask a controller for and nothing to release
	// afterwards. The authenticate is the one that reaches the network.
	d.OverlayStep("ziti", "identity", "loading the ziti identity")
	ctx, err := zitictx.SharedFile("native", id)
	if err != nil {
		return d.OverlayFailed("ziti", fmt.Errorf("could not use that identity: %w", err))
	}
	d.OverlayStep("ziti", "bind", "binding the service "+service)
	ln, err := ctx.Listen(service)
	if err != nil {
		return d.OverlayFailed("ziti", fmt.Errorf("could not bind %q: %w", service, err))
	}

	d.OverlayStep("ziti", "done", "ziti service "+service)
	log.Printf("[atrium] serving the board on the ziti service %q", service)
	// A ziti service has no address. Who may reach it is a policy on the
	// network, and the service name is the whole identifier.
	// A ziti service is administered on the network and atrium never created
	// it, so there is nothing here to release.
	d.Nat(OverlayZiti).ServeOn(ln, ZitiEdge(d.AuthGuard(d.ap.Handler()), service), "ziti service "+service, "")
	return nil
}

// zitiEdge is the browser edge for a ziti service. The name a browser uses is
// the service's intercept address, which is configured on the network and not
// here, so it answers the names in $ATRIUM_HOSTS. With none it checks origins
// and not hosts, and says so, because that leaves DNS rebinding open on it.
func ZitiEdge(h http.Handler, service string) http.Handler {
	if len(edge.EnvNames()) == 0 {
		log.Printf("[atrium] the ziti service %q answers any Host. set %s to its intercept address "+
			"to refuse a rebound one", service, edge.EnvHosts)
		return edge.Unnamed(h)
	}
	return edge.Named(h)
}
