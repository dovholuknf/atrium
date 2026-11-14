package conn

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"strconv"
	"strings"
	"time"
)

// A port preview: the hub forwarding a browser to a loopback port on this room, so a card's docs site or mock board
// opens through the hub. See the hub's half in internal/link/preview.go.
//
//	browser --> HUB /<room>/preview/<port>/<rest> --> room /_link/preview/<port>/<rest> --> 127.0.0.1:<port>/<rest>
//
// THE ROOM DIALS 127.0.0.1 AND NOTHING ELSE. The path carries a port, never a host, and the dial ignores whatever host
// the request names. PreviewPort decides which ports, so atrium's own listeners, which trust loopback, are not among
// them. Every forwarded request carries X-Forwarded-For, so a loopback gate behind the port sees a proxy.

// PreviewPrefix is the room's route for a port preview, on the link only.
const PreviewPrefix = "/_link/preview/"

// previewTransport dials 127.0.0.1 whatever the request names.
var previewTransport = &http.Transport{
	DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
		_, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		d := net.Dialer{Timeout: 10 * time.Second}
		return d.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", port))
	},
	MaxIdleConnsPerHost: 8,
	IdleConnTimeout:     90 * time.Second,
}

// PreviewPath splits `<port>/<rest>` into the port and the path to ask for, which always starts with a slash.
func PreviewPath(s string) (int, string, bool) {
	p, rest, _ := strings.Cut(s, "/")
	if p == "" || len(p) > 5 || strings.Trim(p, "0123456789") != "" {
		return 0, "", false
	}
	n, err := strconv.Atoi(p)
	if err != nil || n < 1 || n > 65535 {
		return 0, "", false
	}
	return n, "/" + rest, true
}

func previewRefuse(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// servePreview is the hub asking this room for one request to a loopback port.
func (r *Room) servePreview(w http.ResponseWriter, req *http.Request) {
	port, rest, ok := PreviewPath(strings.TrimPrefix(req.URL.Path, PreviewPrefix))
	if !ok {
		previewRefuse(w, http.StatusBadRequest, "a preview names a port, 1 to 65535, and nothing else")
		return
	}
	if r.PreviewPort == nil {
		previewRefuse(w, http.StatusForbidden, r.Name+" does not offer port previews")
		return
	}
	if err := r.PreviewPort(port); err != nil {
		previewRefuse(w, http.StatusForbidden, r.Name+" does not preview that port: "+err.Error())
		return
	}
	target := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme = "http"
			pr.Out.URL.Host = target
			pr.Out.URL.Path = rest
			pr.Out.URL.RawPath = ""
			pr.Out.Host = target
			pr.Out.Header["X-Forwarded-For"] = pr.In.Header["X-Forwarded-For"]
			pr.SetXForwarded()
			if h := pr.In.Header.Get("X-Forwarded-Host"); h != "" {
				pr.Out.Header.Set("X-Forwarded-Host", h)
			}
			if pr.Out.Header.Get("X-Forwarded-For") == "" {
				pr.Out.Header.Set("X-Forwarded-For", "atrium-hub")
			}
		},
		Transport:     previewTransport,
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			log.Printf("[link] preview of %s: %v", target, err)
			previewRefuse(w, http.StatusBadGateway, fmt.Sprintf("nothing answered on %s on %s: %v", target, r.Name, err))
		},
	}
	rp.ServeHTTP(w, req)
}
