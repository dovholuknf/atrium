package link

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
)

// The ways a room can reach its hub, and the order it tries them in.
//
// The hub holds an ordered list and says it in every welcome. A room writes what
// it heard down, because a room that cannot reach its hub on the first transport
// is exactly the room that needs to know the second one on its next start. A
// person on the room's machine can add a path the hub cannot know (the room's own
// ziti identity) and can reorder the lot. See docs/rnd/room-transport-order.md.

// DefaultOrder is the order with nothing said: openziti, then direct, then zrok.
// clint 2026-10-14: ziti first, direct second.
var DefaultOrder = []string{"ziti", "direct", "zrok"}

// DefaultZitiService is the service a ziti rung dials when nothing names one. The
// same default `atrium run --atrium-service` has.
const DefaultZitiService = "atrium-hub"

// ParseOrder reads a comma list of transports. `openziti` is ziti, a repeat is
// dropped, and an unknown kind is refused rather than quietly ignored.
func ParseOrder(s string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, f := range strings.Split(s, ",") {
		k := strings.ToLower(strings.TrimSpace(f))
		if k == "" {
			continue
		}
		if k == "openziti" {
			k = "ziti"
		}
		if !knownKind(k) {
			return nil, fmt.Errorf("no transport called %q. one of: %s", k, strings.Join(DefaultOrder, ", "))
		}
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out, nil
}

func knownKind(k string) bool {
	for _, d := range DefaultOrder {
		if d == k {
			return true
		}
	}
	return false
}

// rank is where a kind sits in an order, with anything the order leaves out after
// it, in the default order.
func rank(order []string, kind string) int {
	for i, k := range order {
		if k == kind {
			return i
		}
	}
	for i, k := range DefaultOrder {
		if k == kind {
			return len(order) + i
		}
	}
	return len(order) + len(DefaultOrder)
}

// Arrange sorts paths by an order. Nil is the default order.
func Arrange(paths []Path, order []string) []Path {
	out := append([]Path(nil), paths...)
	sort.SliceStable(out, func(i, j int) bool {
		return rank(order, out[i].Transport) < rank(order, out[j].Transport)
	})
	return out
}

// RoomTransports is a room's `transports.json`.
type RoomTransports struct {
	// Order is this room's own order. Empty follows the hub's.
	Order []string `json:"order,omitempty"`
	// Paths were told to this room on its own machine, and win over the hub's word.
	Paths []Path `json:"paths,omitempty"`
	// Hub is the hub's list as last heard, in the hub's order.
	Hub []Path `json:"hub,omitempty"`
	// Off is the kinds turned off from the hub or the board, ziti or zrok. Kept across a restart.
	Off []string `json:"off,omitempty"`
}

// Local returns the room-local path for a kind, or a fresh one.
func (t RoomTransports) Local(kind string) Path {
	for _, p := range t.Paths {
		if p.Transport == kind {
			return p
		}
	}
	return Path{Transport: kind}
}

// SetLocal replaces the room-local path of that kind.
func (t *RoomTransports) SetLocal(p Path) {
	for i := range t.Paths {
		if t.Paths[i].Transport == p.Transport {
			t.Paths[i] = p
			return
		}
	}
	t.Paths = append(t.Paths, p)
}

// transportsFile is beside room.json and not in it, because `room join` rewrites
// room.json and a re-join must not drop an identity somebody configured by hand.
const transportsFile = "transports.json"

// TransportsPath is where a room keeps its transport settings.
func (k Keys) TransportsPath() string { return k.path(transportsFile) }

// Transports reads a room's transport settings. A missing file is empty settings.
func (k Keys) Transports() (RoomTransports, error) {
	var t RoomTransports
	raw, err := os.ReadFile(k.TransportsPath())
	if errors.Is(err, os.ErrNotExist) {
		return t, nil
	}
	if err != nil {
		return t, err
	}
	if err := json.Unmarshal(raw, &t); err != nil {
		return t, fmt.Errorf("%s: %w", k.TransportsPath(), err)
	}
	return t, nil
}

// SaveTransports writes them. 0600: it can name an identity file and a share.
func (k Keys) SaveTransports(t RoomTransports) error {
	if err := os.MkdirAll(k.Dir, 0o700); err != nil {
		return err
	}
	for i := range t.Paths {
		t.Paths[i].From = ""
	}
	for i := range t.Hub {
		t.Hub[i].From = ""
	}
	raw, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(k.TransportsPath(), raw, 0o600)
}

// merge lays the fields src sets over dst.
func merge(dst, src Path, from string) Path {
	set := false
	for _, f := range []struct {
		d *string
		s string
	}{
		{&dst.Addr, src.Addr}, {&dst.Service, src.Service}, {&dst.Share, src.Share}, {&dst.Identity, src.Identity},
	} {
		if strings.TrimSpace(f.s) != "" {
			*f.d, set = strings.TrimSpace(f.s), true
		}
	}
	if set || dst.From == "" {
		dst.From = from
	}
	return dst
}

// Candidates is the ladder a room climbs: every path it can dial, best first.
//
// Layered in rising precedence: the join in room.json, the hub's list, what was
// told to the room on its own machine. Ordered by the room's order, else the
// hub's, else DefaultOrder. A path that cannot dial is left out and named in
// `skipped` so the log can say why there is no fallback.
//
// A DIRECT ADDRESS IS NEVER LAID OVER ANOTHER ONE. Each distinct address is its
// own rung: a local one first, then the one the room joined with, then the hub's
// advertise. The advertise is where the hub thinks it can be reached, and a hub
// that moved network keeps saying the old one. Laying it over the join turned a
// room that reached its hub on 127.0.0.1 into one whose every data dial went to
// an address that timed out, while its control connection, dialled before the
// hub had spoken, stayed up and made the room look attached.
func Candidates(joined Saved, t RoomTransports) (rungs []Path, skipped []string) {
	by := map[string]Path{}
	var direct []Path
	add := func(p Path, from string) {
		k := strings.ToLower(strings.TrimSpace(p.Transport))
		if k == "openziti" {
			k = "ziti"
		}
		if !knownKind(k) {
			return
		}
		if k == "direct" {
			by[k] = Path{Transport: k}
			addr := strings.TrimSpace(p.Addr)
			if addr == "" {
				return
			}
			for _, d := range direct {
				if equalFold(d.Addr, addr) {
					return
				}
			}
			direct = append(direct, Path{Transport: k, Addr: addr, From: from})
			return
		}
		cur, ok := by[k]
		if !ok {
			cur = Path{Transport: k}
		}
		by[k] = merge(cur, p, from)
	}
	// A direct address typed on this machine is tried before the join's.
	for _, p := range t.Paths {
		if strings.EqualFold(strings.TrimSpace(p.Transport), "direct") {
			add(p, "local")
		}
	}
	if joined.Transport != "" {
		add(Path{Transport: joined.Transport, Addr: joined.Hub, Service: joined.Service,
			Share: joined.Share, Identity: joined.Identity}, "join")
	}
	var hubOrder []string
	for _, p := range t.Hub {
		add(p, "hub")
		hubOrder = append(hubOrder, p.Transport)
	}
	for _, p := range t.Paths {
		add(p, "local")
	}

	for _, k := range DefaultOrder {
		p, ok := by[k]
		if !ok {
			continue
		}
		switch k {
		case "direct":
			if len(direct) == 0 {
				skipped = append(skipped, "direct: no hub address")
				continue
			}
			rungs = append(rungs, direct...)
			continue
		case "ziti":
			if p.Service == "" {
				p.Service = DefaultZitiService
			}
			if p.Identity == "" {
				skipped = append(skipped, "ziti: no identity on this room. `atrium room transports ziti --identity <file>`")
				continue
			}
		case "zrok":
			if p.Share == "" {
				skipped = append(skipped, "zrok: no share token on this room. `atrium room transports zrok --token <share token>`")
				continue
			}
		}
		rungs = append(rungs, p)
	}
	order := t.Order
	if len(order) == 0 {
		order = hubOrder
	}
	return loopbackFirst(Arrange(rungs, order)), skipped
}

// loopbackFirst lifts a direct rung on this machine's own loopback above every
// other rung, whatever the order says. A room dialling 127.0.0.1 is on the hub's
// machine, and sending its traffic round an overlay costs the overlay's round trip
// (85ms against 1ms) on every board call and keystroke. The overlay stays below it
// as the fallback. Order among the rest is untouched.
func loopbackFirst(ps []Path) []Path {
	// A direct rung that already sits above every overlay (an address typed on this machine) keeps its place.
	first := -1
	for i, p := range ps {
		if p.Transport != "direct" {
			first = i
			break
		}
	}
	if first < 0 {
		return ps
	}
	out := append([]Path(nil), ps[:first]...)
	var rest []Path
	for _, p := range ps[first:] {
		if isLoopbackDirect(p) {
			out = append(out, p)
		} else {
			rest = append(rest, p)
		}
	}
	return append(out, rest...)
}

func isLoopbackDirect(p Path) bool {
	if p.Transport != "direct" {
		return false
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(p.Addr))
	if err != nil {
		host = strings.TrimSpace(p.Addr)
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
