package cli

import (
	"bufio"
	"context"
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
	// kind is the transport, and path is what rooms are told about it.
	kind   string
	path   link.Path
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
	// set, when not nil, is every path this side offers right now, asked again at each welcome and on a
	// timer. Direct has one: its addresses move with the machine. Nil means `path` alone.
	set func() []link.Path
	// enabled, when not nil, says whether the side is bound now. A side that is not is left out of the paths and of
	// overlay(). service, when not nil, is the service the side is bound to now.
	enabled func() bool
	service func() string
}

func (s *hubSide) on() bool { return s.enabled == nil || s.enabled() }

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
			kind: "direct", path: link.Path{Transport: "direct", Addr: adv},
			listen: d.Listen,
			enrol:  d.ServeEnrolment,
			auth:   link.DirectAuthenticated,
			joinString: func(name, secret string) (string, error) {
				return keys.MintToken(adv, name, secret)
			},
			release: func() {},
			says:    adv,
			set: func() []link.Path {
				addrs := advertiseSet(linkAddr, advertise, interfaceIPs)
				out := make([]link.Path, len(addrs))
				for i, a := range addrs {
					out[i] = link.Path{Transport: "direct", Addr: a}
				}
				return out
			},
		}, nil

	case "ziti":
		z := &link.Ziti{Identity: zitiIdentity, Service: service}
		d, err := overlayDirect(keys, spend)
		if err != nil {
			return nil, err
		}
		return &hubSide{
			kind: "ziti", path: link.Path{Transport: "ziti", Service: service},
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
			kind: "zrok", path: link.Path{Transport: "zrok"},
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
//
// AN AUTHORITY ALREADY THERE IS LEFT ALONE. `EnsureCA` re-mints the hub's own
// certificate against the hosts it is given, and a hub serving direct beside the
// overlay minted it with the hosts direct needs.
func overlayDirect(keys link.Keys, spend func(string) (string, error)) (link.Direct, error) {
	if _, _, err := keys.CA(); err != nil {
		if err := keys.EnsureCA(nil); err != nil {
			return link.Direct{}, fmt.Errorf("could not set this hub up: %w", err)
		}
	}
	return link.Direct{Keys: keys, Spend: spend}, nil
}

// hubSet is a hub on every transport `--transport` named, at once. The first is
// the primary: join strings are minted for it, and a hub on one transport is a
// set of one. See docs/rnd/room-transport-order.md.
type hubSet struct {
	sides []*hubSide
}

// openHubs opens a side for each transport in a comma list, `direct,ziti`.
func openHubs(kinds string, keys link.Keys, linkAddr, advertise, service string,
	spend func(string) (string, error)) (hubSet, error) {
	order, err := link.ParseOrder(kinds)
	if err != nil {
		return hubSet{}, err
	}
	if len(order) == 0 {
		order = []string{"direct"}
	}
	var set hubSet
	for _, k := range order {
		side, err := openHub(k, keys, linkAddr, advertise, service, spend)
		if err != nil {
			set.release()
			return hubSet{}, err
		}
		set.sides = append(set.sides, side)
	}
	return set, nil
}

func (s hubSet) primary() *hubSide { return s.sides[0] }

func (s hubSet) release() {
	for _, side := range s.sides {
		side.release()
	}
}

// says names where rooms dial in, every transport.
func (s hubSet) says() string {
	var out []string
	for _, side := range s.sides {
		if side.on() {
			out = append(out, side.says)
		}
	}
	return strings.Join(out, ", and ")
}

// overlay says whether any side is an overlay, which is when a room may arrive
// on the old, certificate-less path.
func (s hubSet) overlay() bool {
	for _, side := range s.sides {
		if side.kind != "direct" && side.on() {
			return true
		}
	}
	return false
}

// auth is `Hub.Authenticated` for the whole set. One side keeps its own. More
// than one with an overlay takes the overlay's, which asks the certificate of a
// direct connection exactly as direct does.
func (s hubSet) auth() func(net.Conn) bool {
	if len(s.sides) == 1 || !s.overlay() {
		return s.primary().auth
	}
	return link.OverlayAuthenticated
}

// paths is what a welcome tells rooms: every side but zrok, in `order`, and never
// a credential. zrok's share reaching an authenticated room is a later job.
func (s hubSet) paths(order []string) []link.Path {
	var out []link.Path
	for _, side := range s.sides {
		if side.kind == "zrok" || !side.on() {
			continue
		}
		if side.set != nil {
			if ps := side.set(); len(ps) > 0 {
				out = append(out, ps...)
				continue
			}
		}
		p := side.path
		if side.service != nil {
			p.Service = side.service()
		}
		p.Share, p.Identity, p.From = "", "", ""
		out = append(out, p)
	}
	return link.Arrange(out, order)
}

// hubOrder reads the hub's order from its store. A stored order that no longer
// parses is the default, because a typo must not leave rooms with no list.
func hubOrder(read func() (string, error)) []string {
	v, err := read()
	if err != nil {
		return nil
	}
	order, err := link.ParseOrder(v)
	if err != nil {
		return nil
	}
	return order
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

// roomZitiListen binds the room link's ziti service with a fresh context per call, for the hub's transport switch.
func roomZitiListen(keys link.Keys, spend func(string) (string, error)) func(service, identity string) (net.Listener, error) {
	return func(service, identity string) (net.Listener, error) {
		d, err := overlayDirect(keys, spend)
		if err != nil {
			return nil, err
		}
		z := &link.Ziti{Identity: identity, Service: service}
		return overlayListen(z.Listen, d, "ziti")()
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
// A ROOM WHOSE JOIN IS THE NEW FORM AND THAT HOLDS A CERTIFICATE DIALS AN OVERLAY
// INSIDE TLS, and every kind of connection goes through the one Dialer returned
// here, so none can skip it. An old-form join never wraps, whatever is in the keys
// directory: a stray room.crt from an earlier direct enrolment, maybe to another
// hub, would otherwise start a handshake against the wrong CA and drop the room.
func roomDialer(j link.Join, keys link.Keys, identity string) (link.Dialer, error) {
	d, err := rawRoomDialer(j, keys, identity)
	if err != nil {
		return nil, err
	}
	if j.Transport != "direct" && j.Proven() && keys.HasRoomCert() {
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

// roomLadder is every transport this room can reach its hub on, best first.
//
// Built from the join, the hub's list as last heard and what was told to this
// room on its own machine (`atrium room transports`). `skipped` names what could
// not be a rung and why, for the log. See docs/rnd/room-transport-order.md.
func roomLadder(keys link.Keys, saved link.Saved, tr link.RoomTransports) (*link.Ladder, []string, error) {
	rungs, skipped := roomRungs(keys, saved, tr, nil)
	if len(rungs) == 0 {
		return nil, skipped, fmt.Errorf("this room has no transport it can reach its hub on: %s",
			strings.Join(skipped, "; "))
	}
	return link.NewLadder(rungs), skipped, nil
}

// roomRungs builds the rungs, reusing a dialer from `had` for a path that has not
// changed, so learning the hub's list does not open a second ziti session.
func roomRungs(keys link.Keys, saved link.Saved, tr link.RoomTransports, had []link.Rung) ([]link.Rung, []string) {
	paths, skipped := link.Candidates(saved, tr)
	var rungs []link.Rung
	for _, p := range paths {
		if d := reuse(had, p); d != nil {
			rungs = append(rungs, link.Rung{Path: p, Dial: d})
			continue
		}
		d, err := rungDialer(keys, saved, p)
		if err != nil {
			skipped = append(skipped, p.Transport+": "+err.Error())
			continue
		}
		rungs = append(rungs, link.Rung{Path: p, Dial: d})
	}
	return rungs, skipped
}

func reuse(had []link.Rung, p link.Path) link.Dialer {
	for _, r := range had {
		q := r.Path
		if q.Transport == p.Transport && strings.EqualFold(q.Addr, p.Addr) && q.Service == p.Service &&
			q.Share == p.Share && q.Identity == p.Identity {
			return r.Dial
		}
	}
	return nil
}

// rungDialer is one rung's dialer.
//
// AN OVERLAY RUNG RUNS INSIDE THE ROOM'S CERTIFICATE when this room's certificate
// is this hub's: it joined over direct, or over an overlay with a proven string.
// The hub refuses a name that enrolled a certificate on the old path, so a
// direct room falling back to ziti without it would be turned away by name. An
// old-form overlay join never wraps, for the reason `roomDialer` gives.
func rungDialer(keys link.Keys, saved link.Saved, p link.Path) (link.Dialer, error) {
	d, err := rawRoomDialer(link.Join{
		Transport: p.Transport, Addr: p.Addr, Service: p.Service, ShareToken: p.Share,
	}, keys, p.Identity)
	if err != nil {
		return nil, err
	}
	if p.Transport != "direct" && (saved.Transport == "direct" || saved.Proven) && keys.HasRoomCert() {
		return link.Proven{Dialer: d, Keys: keys}, nil
	}
	return d, nil
}

// climbing is a ladder for a one-off connection, from a command rather than the
// running room: every dial climbs from the top, since nothing has climbed yet.
type climbing struct{ l *link.Ladder }

func (c climbing) Dial(ctx context.Context) (net.Conn, error) {
	return c.l.Climb(ctx, link.DefaultTimings().DialWait)
}

func (c climbing) Describe() string { return c.l.Describe() }

func (c climbing) Close() { c.l.Close() }

func closeDialer(d link.Dialer) {
	if c, ok := d.(interface{ Close() }); ok {
		c.Close()
	}
}

// ladderSays is the rungs in one line, best first, for the log and `room transports`.
func ladderSays(rungs []link.Rung) string {
	out := make([]string, len(rungs))
	for i, r := range rungs {
		out[i] = r.Path.Transport + " " + r.Dial.Describe()
		if r.Path.From != "" {
			out[i] += " (" + r.Path.From + ")"
		}
	}
	return strings.Join(out, ", then ")
}

// learnHubPaths writes the hub's list down when it changed and rebuilds the
// ladder from it, so the next start knows the fallback even when the hub cannot
// be reached. Best effort: a room that cannot write keeps what it has.
func learnHubPaths(keys link.Keys, saved link.Saved, l *link.Ladder, ps []link.Path) {
	tr, err := keys.Transports()
	if err != nil {
		log.Printf("[link] could not read %s: %v", keys.TransportsPath(), err)
		return
	}
	for i := range ps {
		ps[i].From = ""
	}
	if samePaths(tr.Hub, ps) {
		return
	}
	tr.Hub = ps
	if err := keys.SaveTransports(tr); err != nil {
		log.Printf("[link] could not write the hub's transports down: %v", err)
	}
	rungs, skipped := roomRungs(keys, saved, tr, l.Rungs())
	for _, s := range skipped {
		log.Printf("[link] not a way to the hub: %s", s)
	}
	if len(rungs) > 0 {
		l.Set(rungs)
		log.Printf("[link] the hub's transports changed. will try %s", ladderSays(rungs))
	}
}

func samePaths(a, b []link.Path) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := a[i], b[i]
		x.From, y.From = "", ""
		if x != y {
			return false
		}
	}
	return true
}

func onSeed(service, identity string) link.TransportSeed {
	on := true
	return link.TransportSeed{On: &on, Service: service, Identity: identity}
}

// roomZitiSeed is what --transport says of the room link's ziti: on when it names ziti.
func roomZitiSeed(transport, service, identity string) link.TransportSeed {
	order, _ := link.ParseOrder(transport)
	for _, k := range order {
		if k == "ziti" {
			return onSeed(service, identity)
		}
	}
	return link.TransportSeed{}
}

// boardZitiSeed is what --board-transport says of the board's ziti.
func boardZitiSeed(boardTransport, service string) link.TransportSeed {
	if strings.TrimSpace(boardTransport) == "ziti" {
		return onSeed(service, "")
	}
	return link.TransportSeed{}
}

// ciHookSeed is what --ci-hook-service says of the CI hook.
func ciHookSeed(service string) link.TransportSeed {
	if strings.TrimSpace(service) != "" {
		return onSeed(service, "")
	}
	return link.TransportSeed{}
}
