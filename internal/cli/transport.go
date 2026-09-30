package cli

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/dovholuknf/atrium/internal/link"
)

// Choosing a transport, on both sides.
//
// The whole point of the shape `internal/link` asks for is that this file is
// the only place that knows there is more than one. A hub gets a
// `net.Listener`, a room gets something that returns a `net.Conn`, and nothing
// else in the atrium or the room can tell them apart.

// zitiIdentity is the identity file, set by the hub command's --identity flag.
// A package variable because both the hub and the room need it and cobra binds
// flags to addresses rather than returning them.
var zitiIdentity string

// transports is what `--transport` accepts, in the order they are worth using.
var transports = []string{"direct", "ziti", "zrok"}

// hubSide is everything a hub needs to start on one transport.
type hubSide struct {
	listen func() (net.Listener, error)
	// enrol is nil for a transport that carries its own identity.
	enrol func(conn net.Conn, br *bufio.Reader) (string, error)
	auth  func(net.Conn) bool
	// joinString wraps up a paste-able line for ONE NAMED ROOM.
	//
	// The name and the secret both come from the hub's store, which is the only
	// thing that can say which room a credential belongs to. This assembles
	// what the transport adds: an address and a fingerprint, or a service, or a
	// share.
	//
	// The secret is ignored by every transport that carries its own identity,
	// and the name never is. Under zrok private the hub knows a connection came
	// through its own share and not who sent it, so the name is the only thing
	// standing between two rooms on one share and either of them being the
	// other.
	joinString func(name, secret string) (string, error)
	// release is called on the way out. Only zrok has anything to release.
	release func()
	// says is the line describing where rooms dial in.
	says string
}

// openHub prepares one side of one transport.
//
// `spend` answers a join secret with the room the hub minted it for. It comes
// from the hub's store and is handed in here rather than reached for, because
// `internal/link` must not learn that the hub has a database.
func openHub(kind string, keys link.Keys, linkAddr, advertise, service string,
	spend func(string) (string, error)) (*hubSide, error) {
	switch kind {
	case "", "direct":
		// THE ADDRESS A ROOM DIALS, resolved once. A wide --link with no
		// --link-advertise is refused here rather than minting a loopback token
		// that fails for every remote room. This same address names the hub in
		// its certificate, so a room dialling it can pin what the hub proves.
		adv, err := advertiseFor(linkAddr, advertise)
		if err != nil {
			return nil, err
		}
		if err := keys.EnsureCA(link.Hosts(adv)); err != nil {
			return nil, fmt.Errorf("could not set this hub up: %w", err)
		}
		d := link.Direct{Addr: linkAddr, Keys: keys, Spend: spend}
		return &hubSide{
			listen: d.Listen,
			enrol:  d.ServeEnrolment,
			auth:   link.DirectAuthenticated,
			joinString: func(name, secret string) (string, error) {
				return keys.MintToken(adv, name, secret)
			},
			release: func() {},
			says:    adv,
		}, nil

	case "ziti":
		z := &link.Ziti{Identity: zitiIdentity, Service: service}
		d, err := overlayDirect(keys, spend)
		if err != nil {
			return nil, err
		}
		return &hubSide{
			listen: overlayListen(z.Listen, d, "ziti"),
			// THE CERTIFICATE NAMES THE ROOM, as it does over direct. The overlay
			// decides only who may reach this service, so enrolment runs inside it.
			enrol: d.ServeEnrolment,
			auth:  link.OverlayAuthenticated,
			joinString: func(name, secret string) (string, error) {
				return keys.MintProvenOverlayToken("ziti", name, service, "", secret)
			},
			release: z.Close,
			says:    "the ziti service " + service,
		}, nil

	case "zrok":
		z := &link.Zrok{}
		d, err := overlayDirect(keys, spend)
		if err != nil {
			return nil, err
		}
		shareToken, err := z.Share()
		if err != nil {
			return nil, err
		}
		// WRITTEN DOWN FOR `atrium rooms token`, which runs beside this hub and
		// cannot reserve the share itself. See zrokShareFile.
		if err := writeZrokShare(keys, shareToken); err != nil {
			log.Printf("[hub] could not write the share down for `rooms token`: %v", err)
		}
		return &hubSide{
			listen: overlayListen(z.Listen, d, "zrok"),
			enrol:  d.ServeEnrolment,
			auth:   link.OverlayAuthenticated,
			joinString: func(name, secret string) (string, error) {
				return keys.MintProvenOverlayToken("zrok", name, "", shareToken, secret)
			},
			release: func() {
				clearZrokShare(keys)
				z.Release()
			},
			says: "a private zrok share",
		}, nil
	}
	return nil, fmt.Errorf("no transport called %q. one of: %s",
		kind, strings.Join(transports, ", "))
}

// overlayDirect is the direct transport's half a hub over an overlay borrows: its
// certificate authority and its enrolment. No address, because the overlay
// supplies the listener. A hub that was ziti-only now keeps the same key directory
// a direct hub keeps.
//
// The hub certificate names no host: a room's `clientTLS` checks the issuer and
// ignores the address, so no name matters over an overlay.
func overlayDirect(keys link.Keys, spend func(string) (string, error)) (link.Direct, error) {
	if err := keys.EnsureCA(nil); err != nil {
		return link.Direct{}, fmt.Errorf("could not set this hub up: %w", err)
	}
	return link.Direct{Keys: keys, Spend: spend}, nil
}

// overlayListen wraps an overlay's ROOM-LINK listener so it serves certificate
// rooms and, until the operator says otherwise, rooms on the old path.
//
// ONLY THIS LISTENER. The board is served over an overlay by a different
// listener, built under internal/daemon and by the board share in atrium_run.go,
// and neither goes through here: a browser has no client certificate.
func overlayListen(inner func() (net.Listener, error), d link.Direct, transport string) func() (net.Listener, error) {
	return func() (net.Listener, error) {
		cfg, err := d.ServerTLS()
		if err != nil {
			return nil, err
		}
		ln, err := inner()
		if err != nil {
			return nil, err
		}
		return link.MixedListener(ln, cfg, transport), nil
	}
}

// zrokShareFile is where a hub running over zrok writes the private share it
// reserved, so `atrium rooms token` can mint a zrok join string from beside it.
//
// THE HUB'S OWN SHARE, not somebody else's credential. The hub made it and
// releases it on the way out, and this file goes with it. It sits in the hub's
// key directory, 0600, beside the CA key that is already the more valuable
// secret there. A hub that was killed rather than stopped leaves it naming a
// share that may be gone, and a join string minted from that fails at dial.
func zrokShareFile(keys link.Keys) string { return filepath.Join(keys.Dir, "zrok-share") }

func writeZrokShare(keys link.Keys, token string) error {
	if err := os.MkdirAll(keys.Dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(zrokShareFile(keys), []byte(token), 0o600)
}

func clearZrokShare(keys link.Keys) { _ = os.Remove(zrokShareFile(keys)) }

// readZrokShare is the share a running zrok hub wrote down, or an error saying
// there is none.
func readZrokShare(keys link.Keys) (string, error) {
	raw, err := os.ReadFile(zrokShareFile(keys))
	if err != nil || strings.TrimSpace(string(raw)) == "" {
		return "", errors.New("no hub is running over zrok from " + keys.Dir +
			". a zrok join string carries the share the running hub reserved, so start the hub " +
			"with --transport zrok first")
	}
	return strings.TrimSpace(string(raw)), nil
}

// roomDialer is the room's half, chosen by what the join string says.
//
// A ROOM THAT HOLDS A CERTIFICATE DIALS AN OVERLAY INSIDE TLS, and every kind of
// connection goes through the one Dialer returned here, so none can skip it. A
// room with no certificate is one that joined before certificates reached the
// overlays, and it dials exactly as it always did.
func roomDialer(j link.Join, keys link.Keys, identity string) (link.Dialer, error) {
	d, err := rawRoomDialer(j, keys, identity)
	if err != nil {
		return nil, err
	}
	if j.Transport != "direct" && keys.HasRoomCert() {
		return link.Proven{Dialer: d, Keys: keys}, nil
	}
	return d, nil
}

// rawRoomDialer is the transport with nothing wrapped round it. Enrolment dials
// this, because the pinned handshake it runs is not the room's own.
func rawRoomDialer(j link.Join, keys link.Keys, identity string) (link.Dialer, error) {
	switch j.Transport {
	case "direct":
		return link.Direct{Addr: j.Addr, Keys: keys, Pin: j.Pin}, nil
	case "ziti":
		if strings.TrimSpace(identity) == "" {
			return nil, fmt.Errorf(
				"this join string is for the ziti service %q, so this room needs an enrolled "+
					"identity. pass --identity <file>", j.Service)
		}
		return &link.Ziti{Identity: identity, Service: j.Service}, nil
	case "zrok":
		return &link.Zrok{Token: j.ShareToken}, nil
	}
	return nil, fmt.Errorf("no transport called %q", j.Transport)
}
