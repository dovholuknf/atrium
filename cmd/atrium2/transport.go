package main

import (
	"bufio"
	"fmt"
	"net"
	"strings"

	"github.com/dovholuknf/atrium/internal/link"
)

// Choosing a transport, on both sides.
//
// The whole point of the shape `internal/link` asks for is that this file is
// the only place that knows there is more than one. A hub gets a
// `net.Listener`, a room gets something that returns a `net.Conn`, and nothing
// else in `atrium2` can tell them apart.

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
		return &hubSide{
			listen: z.Listen,
			// NIL, AND THAT IS THE POINT. There is nothing to enrol: the
			// network decided who may dial this service before atrium existed.
			// The hub says so plainly if a room tries.
			enrol: nil,
			auth:  link.ZitiAuthenticated,
			joinString: func(name, _ string) (string, error) {
				return link.MintOverlayToken("ziti", name, service, "")
			},
			release: z.Close,
			says:    "the ziti service " + service,
		}, nil

	case "zrok":
		z := &link.Zrok{}
		shareToken, err := z.Share()
		if err != nil {
			return nil, err
		}
		return &hubSide{
			listen: z.Listen,
			enrol:  nil,
			auth:   link.ZrokAuthenticated,
			joinString: func(name, _ string) (string, error) {
				return link.MintOverlayToken("zrok", name, "", shareToken)
			},
			release: z.Release,
			says:    "a private zrok share",
		}, nil
	}
	return nil, fmt.Errorf("no transport called %q. one of: %s",
		kind, strings.Join(transports, ", "))
}

// roomDialer is the room's half, chosen by what the join string says.
func roomDialer(j link.Join, keys link.Keys, identity string) (link.Dialer, error) {
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
