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
//   - A REBOUND HOST. DNS rebinding points a name the page controls at
//     127.0.0.1, and then the page is "same origin" with atrium. A loopback
//     listener answers only a loopback `Host`: `127.0.0.1`, `localhost`,
//     `[::1]`, any port.
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
	"strings"
)

// Loopback wraps a handler served on a loopback listener: the Host check, the
// cross-origin write check and the websocket Origin check.
func Loopback(h http.Handler) http.Handler { return hostCheck(Shared(h)) }

// Shared wraps a handler served on a listener that is not loopback, an
// overlay share, where the share names the host and there is no Host list to
// check against.
func Shared(h http.Handler) http.Handler {
	cop := http.NewCrossOriginProtection()
	return cop.Handler(upgradeCheck(h))
}

// For wraps a handler by the address it listens on: Loopback when the address
// is a loopback one, Shared otherwise. A board bound wide on purpose is reached
// by whatever name somebody typed, and refusing those would break it.
func For(addr string, h http.Handler) http.Handler {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if ip := net.ParseIP(strings.Trim(host, "[]")); (ip != nil && ip.IsLoopback()) || strings.EqualFold(host, "localhost") {
		return Loopback(h)
	}
	return Shared(h)
}

// LoopbackHost reports whether a Host header names this machine's loopback.
func LoopbackHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
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

func hostCheck(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !LoopbackHost(r.Host) {
			refuse(w, "this listener answers only 127.0.0.1, localhost or [::1]")
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
