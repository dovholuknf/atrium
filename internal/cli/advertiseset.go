package cli

import (
	"context"
	"log"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/link/wire"
)

// pathsEvery is how often a running hub recomputes the addresses it tells rooms about. Shorter than
// advertiseEvery, which only warns, because this one fixes the problem for every attached room.
const pathsEvery = 30 * time.Second

// advertiseSet is every direct address a room may dial this hub at, best first: the explicit
// --link-advertise values (a comma list, a host name welcome), then, when the hub is bound wide, this
// machine's own interface addresses on the same port.
//
// A SET, NOT A STRING. A hub that moves from one address to another is still the hub: a room pins the
// issuer and ignores the address, so every address is just one more rung. See
// docs/fabric/multi-transport-design.md, "The stale address".
//
// A loopback bind adds nothing, because an interface address is not where it listens. `local` is passed in
// so a test needs no network.
func advertiseSet(bind, advertise string, local func() ([]net.IP, error)) []string {
	var out []string
	seen := map[string]bool{}
	add := func(a string) {
		a = strings.TrimSpace(a)
		if a == "" || seen[strings.ToLower(a)] {
			return
		}
		seen[strings.ToLower(a)] = true
		out = append(out, a)
	}
	for _, a := range strings.Split(advertise, ",") {
		add(a)
	}
	host, port, err := net.SplitHostPort(bind)
	if err != nil || (host != "0.0.0.0" && host != "::") {
		if len(out) == 0 {
			if a, err := advertiseFor(bind, ""); err == nil {
				add(a)
			}
		}
		return out
	}
	ips, err := local()
	if err != nil {
		return out
	}
	var found []string
	for _, ip := range ips {
		if dialableInterface(ip) {
			found = append(found, net.JoinHostPort(ip.String(), port))
		}
	}
	sort.Strings(found)
	for _, a := range found {
		add(a)
	}
	return out
}

// tunnelNet is the carrier grade NAT range the ziti-edge-tunnel adapter lives in. An address there is a
// tunnel to somewhere else, not a way in on the LAN.
var tunnelNet = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// dialableInterface is whether an interface address is one a room could be told to dial: not loopback, not
// link local, not unspecified, not multicast and not a tunnel adapter.
func dialableInterface(ip net.IP) bool {
	switch {
	case ip == nil, ip.IsLoopback(), ip.IsUnspecified(), ip.IsMulticast(),
		ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast():
		return false
	}
	return !tunnelNet.Contains(ip)
}

// watchPaths asks for the hub's paths every `every` and calls push when they differ from the last it saw,
// so a hub that moved tells the rooms already attached on another rung. The first answer is remembered and
// not pushed: every welcome already carries it.
func watchPaths(ctx context.Context, every time.Duration, paths func() []wire.Path, push func()) {
	last := paths()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			now := paths()
			if samePaths(last, now) {
				continue
			}
			last = now
			log.Printf("[hub] the addresses rooms are told to dial changed. telling attached rooms")
			push()
		}
	}
}
