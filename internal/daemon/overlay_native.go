package daemon

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/openziti/sdk-golang/ziti"
	"github.com/openziti/zrok/v2/environment"
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
type overlayKind string

const (
	OverlayZrok overlayKind = "zrok"
	OverlayZiti overlayKind = "ziti"
)

// OverlayState is what the board shows for one overlay.
type OverlayState struct {
	Kind overlayKind `json:"kind"`
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
	// Output is kept for the shape the board already draws. Nothing writes to
	// it now that no child process is involved.
	Output []string `json:"output"`
	// Err is why the last attempt failed, when it did.
	Err string `json:"err,omitempty"`
}

// nat returns the listener state for one overlay, creating it on first use.
func (d *Daemon) nat(kind overlayKind) *native {
	d.natsMu.Lock()
	defer d.natsMu.Unlock()
	if d.nats[kind] == nil {
		d.nats[kind] = &native{kind: kind}
	}
	return d.nats[kind]
}

// native is one listener atrium is serving the board on.
type native struct {
	mu       sync.Mutex
	kind     overlayKind
	ln       net.Listener
	srv      *http.Server
	since    time.Time
	address  string
	err      string
	stopping bool
	// shareToken is what zrok gave back, kept so the share can be deleted on
	// the way out rather than left behind on the account.
	shareToken string
}

// serveOn takes ownership of a listener and answers the board on it.
//
// The server is the same handler the local board uses. It is a second
// http.Server rather than a second handler, because a listener is what these
// SDKs return and http.Serve wants one.
func (n *native) serveOn(ln net.Listener, handler http.Handler, address, shareToken string) {
	srv := &http.Server{Handler: handler}

	n.mu.Lock()
	n.ln, n.srv, n.since = ln, srv, time.Now()
	n.address, n.shareToken, n.err, n.stopping = address, shareToken, "", false
	n.mu.Unlock()

	go func() {
		err := srv.Serve(ln)
		n.mu.Lock()
		defer n.mu.Unlock()
		if n.srv != srv {
			// Replaced by a later start.
			return
		}
		n.ln, n.srv = nil, nil
		if err != nil && err != http.ErrServerClosed && !n.stopping {
			n.err = err.Error()
			log.Printf("[atrium] the %s listener stopped: %v", n.kind, err)
			return
		}
		log.Printf("[atrium] the %s listener stopped", n.kind)
	}()
}

func (n *native) running() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.srv != nil
}

func (n *native) state(found string) OverlayState {
	n.mu.Lock()
	defer n.mu.Unlock()
	st := OverlayState{
		Kind: n.kind, Found: found, Running: n.srv != nil,
		Address: n.address, Err: n.err,
	}
	if n.srv != nil {
		st.Since = n.since.Format(time.RFC3339)
	}
	return st
}

// stop closes the listener and, for zrok, releases the share.
//
// Deleting the share matters: a reserved one lingers on the account and an
// ephemeral one holds a name. Leaving them behind turns a board somebody
// opened twice into a list of dead shares they have to clean up by hand.
func (n *native) stop(root env_core.Root) {
	n.mu.Lock()
	srv, token := n.srv, n.shareToken
	n.stopping = true
	// What it published died with it, and the error belonged to a listener
	// that is over.
	n.address, n.err, n.shareToken = "", "", ""
	n.mu.Unlock()

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
func (d *Daemon) startZrokNative(cfg ZrokConfig) error {
	root, err := environment.LoadRoot()
	if err != nil {
		return fmt.Errorf("could not read the zrok environment: %w", err)
	}
	if !root.IsEnabled() {
		return fmt.Errorf("this machine has no zrok environment yet")
	}

	mode := strings.TrimSpace(cfg.Mode)
	if mode != "public" && mode != "private" {
		return fmt.Errorf("share mode must be public or private, not %q", mode)
	}

	req := &zroksdk.ShareRequest{
		// proxy is what serves an HTTP backend, which is what the board is.
		BackendMode: zroksdk.ProxyBackendMode,
		ShareMode:   zroksdk.ShareMode(mode),
		// The target is only used to name the share. Nothing dials it: this
		// process answers the listener itself.
		Target: d.defaultBackend(),
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
	} else if n := strings.TrimSpace(cfg.Name); n != "" {
		sel, err := zroksdk.ParseNameSelection(n)
		if err != nil {
			return fmt.Errorf("that name selection is not one zrok understands: %w", err)
		}
		req.NameSelections = []zroksdk.NameSelection{sel}
	}

	shr, err := zroksdk.CreateShare(root, req)
	if err != nil {
		return fmt.Errorf("zrok refused the share: %w", err)
	}

	ln, err := zroksdk.NewListener(shr.Token, root)
	if err != nil {
		// The share exists and nothing is answering it, so it goes.
		_ = zroksdk.DeleteShare(root, shr)
		return fmt.Errorf("could not listen on that share: %w", err)
	}

	// The address as data. A public share carries its frontend URLs; a private
	// one has none, and the token is what the other end runs.
	address := "zrok access private " + shr.Token
	if len(shr.FrontendEndpoints) > 0 {
		address = shr.FrontendEndpoints[0]
	}

	log.Printf("[atrium] serving the board on a %s zrok share at %s", mode, address)
	d.nat(OverlayZrok).serveOn(ln, d.ap.Handler(), address, shr.Token)
	return nil
}

// startZitiNative serves the board on a ziti service, using the identity as
// configured.
//
// Nothing is hosted or forwarded. The board answers on the service itself, so
// there is no tunneler and no address on this machine for anything to reach.
func (d *Daemon) startZitiNative(cfg ZitiConfig) error {
	id := strings.TrimSpace(cfg.Identity)
	if id == "" {
		return fmt.Errorf("no identity: enroll one, or point atrium at an identity file")
	}
	service := strings.TrimSpace(cfg.Service)
	if service == "" {
		return fmt.Errorf("no service: name the ziti service this board answers")
	}

	zcfg, err := ziti.NewConfigFromFile(id)
	if err != nil {
		return fmt.Errorf("could not load that identity: %w", err)
	}
	ctx, err := ziti.NewContext(zcfg)
	if err != nil {
		return fmt.Errorf("could not use that identity: %w", err)
	}
	ln, err := ctx.Listen(service)
	if err != nil {
		return fmt.Errorf("could not bind %q: %w", service, err)
	}

	log.Printf("[atrium] serving the board on the ziti service %q", service)
	// A ziti service has no address. Who may reach it is a policy on the
	// network, and the service name is the whole identifier.
	// A ziti service is administered on the network and atrium never created
	// it, so there is nothing here to release.
	d.nat(OverlayZiti).serveOn(ln, d.ap.Handler(), "ziti service "+service, "")
	return nil
}
