// Package edge is the browser edge of every listener a browser can reach:
// stage 0 of docs/rnd/security-design.md.
//
// ── what it stops ───────────────────────────────────────
//
// A web page open in any browser on this machine can send requests to
// 127.0.0.1. Three things stop it doing harm, and every browser-facing listener
// takes all three:
//
//   - CROSS-ORIGIN WRITES. `http.CrossOriginProtection` refuses a non-safe
//     method whose `Sec-Fetch-Site` is not same-origin or none, falling back to
//     comparing `Origin` with `Host`. A request with neither header passes,
//     which is every hook, the CLI, the MCP server and curl.
//   - A REBOUND HOST. DNS rebinding points a name the page controls at an
//     address atrium answers, and then the page is "same origin" with atrium:
//     its Origin and its Host are both the attacker's name. So a listener
//     answers only the names it is known by. Loopback names always, `127.0.0.1`,
//     `localhost` and `[::1]` with any port, and on any other listener the names
//     it was given: the share's host, the address it is bound to, this machine's
//     own names, and $ATRIUM_HOSTS.
//   - A CROSS-ORIGIN WEBSOCKET. An upgrade is a GET, which the first check
//     passes, so an upgrade whose `Origin` names another host is refused here.
//
// ── what it does not wrap ───────────────────────────────
//
// A room's link data handler, which is what the hub sends a room. The hub
// forwards a board request with the browser's `Origin` and its own `Host`, so a
// room checking those would refuse every attach through the hub. The hub checks
// at its edge, and a room tells the two paths apart by the listener a request
// arrived on (ViaLink), never by a header.
package edge

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// EnvHosts is the variable naming extra hosts every listener answers, comma
// separated: a LAN name, a ziti intercept address, a reverse proxy's name.
const EnvHosts = "ATRIUM_HOSTS"

// Loopback wraps a handler served on a loopback listener: loopback names only.
func Loopback(h http.Handler) http.Handler { return Named(h) }

// Named wraps a handler that answers loopback names and `names`, plus
// $ATRIUM_HOSTS. A name may be a host, a host and port, or a URL.
//
// `*.example.com` answers every name under that domain, and not the domain
// itself. For a share frontend that hands out a random name per share, like
// `*.shares.zrok.io`. Still no rebinding: a page can only rebind a name whose
// DNS it controls, and nobody but the frontend's operator controls names under
// its domain.
func Named(h http.Handler, names ...string) http.Handler {
	allow := hostSet{exact: map[string]bool{}}
	for _, n := range append(names, EnvNames()...) {
		host := hostOf(n)
		if suffix, ok := strings.CutPrefix(host, "*."); ok {
			if strings.Contains(suffix, ".") {
				allow.under = append(allow.under, "."+suffix)
			}
			continue
		}
		if host != "" {
			allow.exact[host] = true
		}
	}
	return hostCheck(checks(h), allow)
}

// hostSet is the names a listener answers besides loopback: exact names, and
// the domains every name under which it answers, each with its leading dot.
type hostSet struct {
	exact map[string]bool
	under []string
}

func (s hostSet) has(host string) bool {
	if s.exact[host] {
		return true
	}
	for _, d := range s.under {
		if len(host) > len(d) && strings.HasSuffix(host, d) {
			return true
		}
	}
	return false
}

// Unnamed wraps a handler whose listener has no name atrium knows, which is a
// ziti service nobody listed in $ATRIUM_HOSTS: the name a browser uses is the
// service's intercept address, configured on the network and not here. It
// checks origins and not hosts, which leaves rebinding open there, so a caller
// must say so where somebody will read it.
func Unnamed(h http.Handler) http.Handler { return checks(h) }

// For wraps a handler by the address it listens on: loopback names for a
// loopback address, and for any other the address itself and, when it is every
// interface, each interface's address and this machine's names.
func For(addr string, h http.Handler) http.Handler {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	host = strings.Trim(host, "[]")
	if LoopbackHost(host) {
		return Loopback(h)
	}
	if ip := net.ParseIP(host); host != "" && (ip == nil || !ip.IsUnspecified()) {
		return Named(h, host)
	}
	return Named(h, machineNames()...)
}

// EnvNames is $ATRIUM_HOSTS, split.
func EnvNames() []string {
	var out []string
	for _, s := range strings.Split(os.Getenv(EnvHosts), ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// machineNames is what a browser on the network can call this machine: every
// interface address, and the host name with and without its domain.
func machineNames() []string {
	var out []string
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok {
				out = append(out, ipn.IP.String())
			}
		}
	}
	if h, err := os.Hostname(); err == nil && h != "" {
		out = append(out, h)
		if i := strings.IndexByte(h, '.'); i > 0 {
			out = append(out, h[:i])
		}
	}
	return out
}

// hostOf is the host part of a name, a host:port or a URL, lower case.
func hostOf(s string) string {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "://") {
		if u, err := url.Parse(s); err == nil {
			s = u.Host
		}
	}
	if h, _, err := net.SplitHostPort(s); err == nil {
		s = h
	}
	return strings.ToLower(strings.Trim(s, "[]"))
}

// LoopbackHost reports whether a Host header names this machine's loopback.
func LoopbackHost(hostport string) bool {
	host := hostOf(hostport)
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func refuse(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(`{"error":"` + msg + `"}` + "\n"))
}

func checks(h http.Handler) http.Handler {
	cop := http.NewCrossOriginProtection()
	return cop.Handler(upgradeCheck(h))
}

func hostCheck(h http.Handler, allow hostSet) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !LoopbackHost(r.Host) && !allow.has(hostOf(r.Host)) {
			refuse(w, "this listener does not answer to that name. $"+EnvHosts+" adds one")
			return
		}
		h.ServeHTTP(w, r)
	})
}

// upgradeCheck refuses a websocket upgrade whose Origin is another host. An
// upgrade with no Origin is not a browser, and passes.
func upgradeCheck(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			if o := r.Header.Get("Origin"); o != "" {
				u, err := url.Parse(o)
				if err != nil || !strings.EqualFold(u.Host, r.Host) {
					refuse(w, "a terminal is attached only from this board's own page")
					return
				}
			}
		}
		h.ServeHTTP(w, r)
	})
}

type viaLinkKey struct{}

// MarkLink wraps a room's link data handler, so a websocket accept can tell a
// request the hub forwarded, which the hub's edge checked, from one that came
// in on a listener of its own.
func MarkLink(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), viaLinkKey{}, true)))
	})
}

// ViaLink reports whether a request came through the hub's link.
func ViaLink(r *http.Request) bool {
	v, _ := r.Context().Value(viaLinkKey{}).(bool)
	return v
}
