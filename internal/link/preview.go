package link

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/dovholuknf/atrium/internal/link/conn"
)

// A port preview through the hub: `/<room>/preview/<port>/<rest>` is `127.0.0.1:<port>/<rest>` on that room, over the
// room's link. See internal/link/conn/preview.go for the room's half, which decides the port.
//
// THE BOARD'S AUTH, because it is served by the same handler behind the same edge. Nothing new listens.
//
// A PREVIEW IS SANDBOXED. It is served from the board's origin, so its script could otherwise drive the board as the
// person looking at it, and on the hub's machine that is the operator. Every answer carries a CSP sandbox without
// allow-same-origin, so the page runs in an opaque origin: it cannot read the board, and the edge refuses its writes.
// Cookies and Clear-Site-Data from a preview are dropped for the same reason.
//
// PREFIX AWARE BY REWRITING: a Location, and root-relative href, src, action and url() in HTML and CSS, gain the prefix.
// Script is not rewritten. X-Forwarded-Prefix tells a site that wants to build its own links.

// previewSandbox is the CSP every preview answer carries.
const previewSandbox = "sandbox allow-scripts allow-forms allow-popups allow-popups-to-escape-sandbox allow-modals " +
	"allow-downloads"

// previewRewriteMax is the largest HTML or CSS body rewritten. A larger one passes as it is.
const previewRewriteMax = 16 << 20

var (
	previewAttr = regexp.MustCompile(`(?i)\s(?:href|src|action|poster|formaction)\s*=\s*["']?/`)
	previewURL  = regexp.MustCompile(`(?i)url\(\s*["']?/`)
)

// previewOf reads `/<room>/preview/<port...>`: the room, and what follows `preview`, which is "" or starts with a
// slash.
func previewOf(p string) (room, after string, ok bool) {
	if !strings.HasPrefix(p, "/") {
		return "", "", false
	}
	room, tail, found := strings.Cut(p[1:], "/")
	if !found || room == "" || strings.HasPrefix(room, "_") {
		return "", "", false
	}
	if tail != "preview" && !strings.HasPrefix(tail, "preview/") {
		return "", "", false
	}
	return room, strings.TrimPrefix(tail, "preview"), true
}

// servePreview answers a port preview, and says whether the request was one.
func (p *Proxy) servePreview(w http.ResponseWriter, r *http.Request) bool {
	room, after, ok := previewOf(r.URL.Path)
	if !ok || p.hub == nil {
		return false
	}
	if !p.hub.Has(room) {
		writeErrBody(w, http.StatusServiceUnavailable, "room "+room+" is not attached to this hub")
		return true
	}
	portText, _, slash := strings.Cut(strings.TrimPrefix(after, "/"), "/")
	port, rest, valid := conn.PreviewPath(strings.TrimPrefix(after, "/"))
	if !valid {
		writeErrBody(w, http.StatusBadRequest, "a preview is /"+room+"/preview/<port>/, a port from 1 to 65535 on that "+
			"room's 127.0.0.1")
		return true
	}
	prefix := "/" + room + "/preview/" + portText
	if !slash {
		u := *r.URL
		u.Path = prefix + "/"
		http.Redirect(w, r, u.RequestURI(), http.StatusMovedPermanently)
		return true
	}
	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme = "http"
			pr.Out.URL.Host = hostFor(room)
			pr.Out.URL.Path = conn.PreviewPrefix + strconv.Itoa(port) + rest
			pr.Out.URL.RawPath = ""
			pr.Out.Host = r.Host
			pr.Out.Header.Del("Cookie")
			pr.Out.Header.Del("Authorization")
			pr.Out.Header.Del("Accept-Encoding")
			pr.SetXForwarded()
			pr.Out.Header.Set("X-Forwarded-Prefix", prefix)
		},
		Transport:      p.proxy.Transport,
		FlushInterval:  -1,
		ModifyResponse: func(res *http.Response) error { return previewAnswer(res, prefix, port) },
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			if errors.Is(err, ErrNoRoom) {
				writeErrBody(w, http.StatusServiceUnavailable, "room "+room+" is not attached to this hub")
				return
			}
			log.Printf("[hub] preview %s on %s: %v", prefix, room, err)
			writeErrBody(w, http.StatusBadGateway, "room "+room+" did not answer the preview: "+err.Error())
		},
	}
	rp.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), roomKey{}, room)))
	return true
}

// previewAnswer puts the prefix into what the preview said, and the sandbox around it.
func previewAnswer(res *http.Response, prefix string, port int) error {
	h := res.Header
	h.Del("Set-Cookie")
	h.Del("Clear-Site-Data")
	h.Add("Content-Security-Policy", previewSandbox)
	if loc := h.Get("Location"); loc != "" {
		h.Set("Location", previewLocation(loc, prefix, port))
	}
	if res.StatusCode == http.StatusSwitchingProtocols || h.Get("Content-Encoding") != "" {
		return nil
	}
	ct := strings.ToLower(h.Get("Content-Type"))
	html := strings.HasPrefix(ct, "text/html")
	if !html && !strings.HasPrefix(ct, "text/css") {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, previewRewriteMax+1))
	if err != nil {
		return err
	}
	if len(body) > previewRewriteMax {
		res.Body = struct {
			io.Reader
			io.Closer
		}{io.MultiReader(bytes.NewReader(body), res.Body), res.Body}
		return nil
	}
	_ = res.Body.Close()
	if html {
		body = previewRoot(body, previewAttr, prefix)
	}
	body = previewRoot(body, previewURL, prefix)
	res.Body = io.NopCloser(bytes.NewReader(body))
	res.ContentLength = int64(len(body))
	h.Set("Content-Length", strconv.Itoa(len(body)))
	return nil
}

// previewRoot puts the prefix in front of each root-relative path re finds, ending at its slash. A protocol-relative
// `//host` and a path already under the prefix are left alone.
func previewRoot(b []byte, re *regexp.Regexp, prefix string) []byte {
	under := []byte(prefix[1:])
	var out []byte
	last := 0
	for _, m := range re.FindAllIndex(b, -1) {
		slash := m[1] - 1
		next := b[m[1]:]
		if len(next) > 0 && next[0] == '/' {
			continue
		}
		if bytes.HasPrefix(next, under) && (len(next) == len(under) || strings.IndexByte(`/"' )>?#`, next[len(under)]) >= 0) {
			continue
		}
		out = append(out, b[last:slash]...)
		out = append(out, prefix...)
		last = slash
	}
	if out == nil {
		return b
	}
	return append(out, b[last:]...)
}

// previewLocation is a redirect the preview gave, moved under the prefix. Another site's address is left alone.
func previewLocation(loc, prefix string, port int) string {
	u, err := url.Parse(loc)
	if err != nil {
		return loc
	}
	if u.Scheme == "" && u.Host == "" {
		if !strings.HasPrefix(loc, "/") || strings.HasPrefix(loc, prefix+"/") || loc == prefix {
			return loc
		}
		return prefix + loc
	}
	host, p, err := net.SplitHostPort(u.Host)
	if err != nil || p != strconv.Itoa(port) {
		return loc
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return loc
	}
	return prefix + "/" + strings.TrimPrefix(u.RequestURI(), "/")
}
