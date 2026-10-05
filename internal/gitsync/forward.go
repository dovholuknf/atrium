package gitsync

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	"time"
)

// Forwarder is a loopback HTTP listener that lives for the length of ONE git command.
//
// Git speaks HTTP to a URL, and neither side of the link has a URL for the other. So the
// side that wants the objects opens this on 127.0.0.1, points `git fetch` at it, and the
// reverse proxy carries each request over whatever Transport it was given: a link
// connection, in practice.
//
// The token in the path is a 128-bit guess for another process on this machine, and the
// listener is closed the moment the command exits, so the window is a few seconds. A
// request without the token is a 404 and never reaches the link.
type Forwarder struct {
	// URL is what to hand git: `http://127.0.0.1:PORT/TOKEN`. Append `/<name>.git`.
	URL string

	ln    net.Listener
	srv   *http.Server
	token string
}

// NewForwarder opens the listener. `prefix` is put in front of the path on the far side
// (`/v1/git` for a room, empty for a hub), and `host` is the Host the far side sees.
func NewForwarder(rt http.RoundTripper, host, prefix string) (*Forwarder, error) {
	return newForwarder(rt, host, prefix, nil)
}

// newForwarder is NewForwarder with a check on each request's path after the token. Nil allows every path.
func newForwarder(rt http.RoundTripper, host, prefix string, allow func(r *http.Request, path string) bool) (
	*Forwarder, error) {
	var tok [16]byte
	if _, err := rand.Read(tok[:]); err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	f := &Forwarder{ln: ln, token: hex.EncodeToString(tok[:])}
	f.URL = "http://" + ln.Addr().String() + "/" + f.token

	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme = "http"
			pr.Out.URL.Host = host
			pr.Out.URL.Path = prefix + strings.TrimPrefix(pr.In.URL.Path, "/"+f.token)
			pr.Out.URL.RawPath = ""
			pr.Out.Host = host
		},
		Transport:     rt,
		FlushInterval: -1,
	}
	f.srv = &http.Server{
		ReadHeaderTimeout: 10 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			want := "/" + f.token + "/"
			if len(r.URL.Path) < len(want) ||
				subtle.ConstantTimeCompare([]byte(r.URL.Path[:len(want)]), []byte(want)) != 1 {
				http.NotFound(w, r)
				return
			}
			if allow != nil && !allow(r, r.URL.Path[len(want)-1:]) {
				http.Error(w, "this serves a fetch of one repository and nothing else", http.StatusForbidden)
				return
			}
			proxy.ServeHTTP(w, r)
		}),
	}
	go func() { _ = f.srv.Serve(ln) }()
	return f, nil
}

// Close stops listening and drops what is in flight. Called when the git command exits.
func (f *Forwarder) Close() { _ = f.srv.Close() }

// HubLoopback is a read-only Forwarder to one repository of the hub's store, for a room's own `git fetch` of what the
// hub holds: a pull request's head under refs/atrium/pr/<N>, or the repository to clone. It serves the fetch of that
// one repository and nothing else, so no push and no other repository goes through it, and needs no card: the store
// serves a fetch to any room on the link. url is what to hand git, and stop closes it when the command exits.
func HubLoopback(rt http.RoundTripper, name string) (url string, stop func(), err error) {
	if _, err := ParseName(name); err != nil {
		return "", nil, err
	}
	base := StorePrefix + name + ".git/"
	f, err := newForwarder(rt, "hub", "", func(r *http.Request, path string) bool {
		switch {
		case path == base+"info/refs":
			return r.Method == http.MethodGet && r.URL.Query().Get("service") == "git-upload-pack"
		case path == base+"git-upload-pack":
			return r.Method == http.MethodPost
		}
		return false
	})
	if err != nil {
		return "", nil, err
	}
	return f.URL + StorePrefix + name + ".git", f.Close, nil
}
