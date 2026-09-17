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
	// token is the join string to print.
	token func() (string, error)
	// release is called on the way out. Only zrok has anything to release.
	release func()
	// says is the line describing where rooms dial in.
	says string
}

// openHub prepares one side of one transport.
func openHub(kind string, keys link.Keys, linkAddr, service string) (*hubSide, error) {
	switch kind {
	case "", "direct":
		if err := keys.EnsureCA(link.Hosts(advertised(linkAddr))); err != nil {
			return nil, fmt.Errorf("could not set this hub up: %w", err)
		}
		d := link.Direct{Addr: linkAddr, Keys: keys}
		return &hubSide{
			listen:  d.Listen,
			enrol:   d.ServeEnrolment,
			auth:    link.DirectAuthenticated,
			token:   func() (string, error) { return keys.MintToken(advertised(linkAddr)) },
			release: func() {},
			says:    advertised(linkAddr),
		}, nil

	case "ziti":
		z := &link.Ziti{Identity: zitiIdentity, Service: service}
		return &hubSide{
			listen: z.Listen,
			// NIL, AND THAT IS THE POINT. There is nothing to enrol: the
			// network decided who may dial this service before atrium existed.
			// The hub says so plainly if a room tries.
			enrol:   nil,
			auth:    link.ZitiAuthenticated,
			token:   func() (string, error) { return link.MintOverlayToken("ziti", service, "") },
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
			listen:  z.Listen,
			enrol:   nil,
			auth:    link.ZrokAuthenticated,
			token:   func() (string, error) { return link.MintOverlayToken("zrok", "", shareToken) },
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
