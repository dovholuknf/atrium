package gitsync

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"regexp"
	"strings"
)

// HubRemotePrefix is where a room's stable hub forwarder is mounted on its agent listener. A clone's `hub` remote
// is `http://127.0.0.1:<agent port>` + HubRemotePrefix + `<host>/<owner>/<repo>.git`, the same path the hub serves
// its store at, so the room rewrites nothing.
const HubRemotePrefix = StorePrefix

// HubForwarder is the room's stable forwarder to the hub's store, docs/rnd/hub-forge-design.md 5.1. Unlike Forwarder
// it lives as long as the room, because a clone's remote needs a URL that does not change.
//
//   - It takes the card from the card's own token and refuses a request without one, with its own sentence, before
//     anything is sent to the hub. The hub refuses too.
//   - It sends the card to the hub in X-Atrium-Card and X-Atrium-Card-Chain. A copy of either (or of the token
//     header) that the client sent is DELETED first and the forwarder's own is Set, never Added: the hub reads the
//     card from these and a client must not choose it.
//   - It does not touch git's bytes, in either direction. The hub's pre-receive reads the ref commands from the
//     body, and the hub's refusals (`remote: atrium: ...`, `ERR atrium: ...`) reach git as they are.
//   - With the room's push setting at `none` it refuses receive-pack, GET and POST.
type HubForwarder struct {
	// Auth says which card a token is. Nil refuses everything.
	Auth CardAuth
	// Push is the room's git.push setting, `hub` or `none`. Nil reads as `none`.
	Push func() string
	// Transport reaches the hub over the link's git kind. An error says why it cannot.
	Transport func() (http.RoundTripper, error)
	// Chain is the card then its `moved_to` predecessors, nearest first, at most MaxChain. Nil is the card alone,
	// which is all there is until atrium move lands.
	Chain func(card string) []string
}

// ServeHTTP implements http.Handler.
func (h *HubForwarder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	esc := strings.ToLower(r.URL.EscapedPath())
	if strings.Contains(esc, "%2e") || strings.Contains(esc, "%5c") || strings.Contains(esc, "%2f") {
		http.NotFound(w, r)
		return
	}
	_, tail, ok := splitStorePath(r.URL.Path)
	if !ok {
		http.Error(w, "atrium: that is not a hub repository path. the form is /git/hub/<host>/<owner>/<repo>.git, with the host spelled the way the hub does (github, not github.com)", http.StatusNotFound)
		return
	}
	// ONLY THE FOUR REQUESTS GIT MAKES. Anything else is not forwarded.
	var service string
	switch {
	case tail == "info/refs" && r.Method == http.MethodGet:
		service = r.URL.Query().Get("service")
		if service != "git-upload-pack" && service != "git-receive-pack" {
			http.Error(w, "atrium: this serves fetch and push and nothing else", http.StatusForbidden)
			return
		}
	case tail == "git-upload-pack" && r.Method == http.MethodPost &&
		r.Header.Get("Content-Type") == "application/x-git-upload-pack-request":
		service = "git-upload-pack"
	case tail == "git-receive-pack" && r.Method == http.MethodPost &&
		r.Header.Get("Content-Type") == "application/x-git-receive-pack-request":
		service = "git-receive-pack"
	default:
		http.Error(w, "atrium: this serves fetch and push and nothing else", http.StatusForbidden)
		return
	}

	// WHO IS ASKING, before anything else. A request with no token goes nowhere.
	var card string
	if h.Auth != nil {
		card, _ = h.Auth.Check(strings.TrimSpace(r.Header.Get(HeaderCardToken)))
	}
	if card == "" {
		h.refuse(w, tail, service, "this room's hub remote answers a card with its atrium token, and this request has none that is good. run git from a card the room launched, or have the operator push")
		return
	}
	if service == "git-receive-pack" && (h.Push == nil || h.Push() != "hub") {
		h.refuse(w, tail, service, "this room does not let cards push to the hub (git.push is none)")
		return
	}
	if h.Transport == nil {
		h.refuse(w, tail, service, "this room has no hub to push to")
		return
	}
	rt, err := h.Transport()
	if err != nil {
		h.refuse(w, tail, service, "the hub is not reachable from this room right now: "+err.Error())
		return
	}
	chain := []string{card}
	if h.Chain != nil {
		if c := h.Chain(card); len(c) > 0 && c[0] == card && len(c) <= MaxChain {
			chain = c
		}
	}

	limit := int64(maxRequest)
	if service == "git-receive-pack" {
		limit = maxPush + maxPushCommands
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme = "http"
			pr.Out.URL.Host = "hub"
			pr.Out.Host = "hub"
			// DELETE, THEN SET. A client's own card headers never get through, and the token is not the hub's.
			pr.Out.Header.Del(HeaderCard)
			pr.Out.Header.Del(HeaderChain)
			pr.Out.Header.Del(HeaderCardToken)
			pr.Out.Header.Set(HeaderCard, card)
			pr.Out.Header.Set(HeaderChain, strings.Join(chain, ","))
		},
		Transport: rt,
		// Streamed, not buffered: a pack is not held whole.
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				h.refuse(w, tail, service, "that is bigger than a room forwards to the hub")
				return
			}
			h.refuse(w, tail, service, "the hub did not answer: "+oneLine(err.Error()))
		},
	}
	proxy.ServeHTTP(w, r)
}

// refuse answers in a way git prints, so the sentence reaches the person: an ERR pkt where git is reading an
// advertisement or a result, as the hub's own refusals are made. The status is 200 on purpose, because git shows
// a body only for a 200 and a bare `error: 403` for anything else.
func (h *HubForwarder) refuse(w http.ResponseWriter, tail, service, why string) {
	w.Header().Set("Cache-Control", "no-cache")
	if tail == "info/refs" {
		w.Header().Set("Content-Type", "application/x-"+service+"-advertisement")
		_, _ = w.Write(refusedAdvert(service, why))
		return
	}
	w.Header().Set("Content-Type", "application/x-"+service+"-result")
	_, _ = w.Write(pktLine("ERR atrium: " + oneLine(why) + "\n"))
}

// The `hub` remote of a clone in the scm folder. See docs/rnd/hub-forge-design.md section 5.2.
//
// One file for the hub remote: the forwarder above, StableHubURL for the URL a clone's remote has, and
// ensureHubRemote, the one place `hub` (or `atrium-hub`) is added to a clone. Both the scm clone (SCM.hub) and
// the git-sync of a room's clones (ensureRemotes) call it.

// Remote names. atrium's own goes in as `hub`, and as `atrium-hub` where `hub` is taken.
const (
	HubRemote       = "hub"
	HubRemoteAtrium = "atrium-hub"
)

// StableHubURL is the hub remote of a repository on a room whose agent listener is at `agentAddr`
// (host:port): `http://<agent>/git/hub/<host>/<owner>/<repo>.git`.
//
// An agent listener on all interfaces (`0.0.0.0:7782`, `:7782` or `[::]:7782`) is reached at 127.0.0.1,
// because http://0.0.0.0 is not an address a client can rely on.
func StableHubURL(agentAddr string, r Ref) string {
	if h, p, err := net.SplitHostPort(agentAddr); err == nil && (h == "" || h == "0.0.0.0" || h == "::") {
		agentAddr = net.JoinHostPort("127.0.0.1", p)
	}
	return "http://" + agentAddr + "/git/hub/" + r.Name() + ".git"
}

// hub adds atrium's remote to a clone and records the outcome on the result. A failure here does
// not fail the clone, which is made and usable: the note says what was not done.
func (c *SCM) hub(ctx context.Context, ref Ref, dir string, res *SCMResult) {
	if c.HubURL == nil {
		res.Note = "the hub remote was not added: this room has no hub forwarder yet"
		return
	}
	url, err := c.HubURL(ref)
	if err != nil {
		res.Note = "the hub remote was not added: " + err.Error()
		return
	}
	name, note, err := c.addHubRemote(ctx, dir, url)
	if err != nil {
		res.Note = "the hub remote was not added: " + err.Error()
		return
	}
	res.Hub, res.Note = name, note
}

// addHubRemote is ensureHubRemote for the scm clone.
func (c *SCM) addHubRemote(ctx context.Context, dir, url string) (name, note string, err error) {
	return ensureHubRemote(ctx, c.runner(), dir, url)
}

// ownForwarderURL is a remote url that is an atrium forwarder: this room's from an earlier start, or on another
// agent port.
var ownForwarderURL = regexp.MustCompile(`^http://127\.0\.0\.1:[0-9]+/git/hub/`)

const movedNote = "this clone's own `hub` remote points somewhere else and was left alone, so atrium's is `atrium-hub`"

// ensureHubRemote adds `hub` pointing at url. A `hub` that already points at url is left as it is, and one that is
// atrium's own from another agent port is moved to url. A `hub` pointing anywhere else is left ALONE and reported,
// and atrium's remote goes in as `atrium-hub`.
func ensureHubRemote(ctx context.Context, g *Runner, dir, url string) (name, note string, err error) {
	for _, n := range []string{HubRemote, HubRemoteAtrium} {
		have, gerr := g.Git(ctx, dir, "config", "--local", "--get", "remote."+n+".url")
		have = strings.TrimSpace(have)
		switch {
		case gerr != nil || have == "":
			if _, err := g.Git(ctx, dir, "remote", "add", n, url); err != nil {
				return "", "", fmt.Errorf("%s", firstLine(err))
			}
		case have == url:
		case ownForwarderURL.MatchString(have):
			if _, err := g.Git(ctx, dir, "remote", "set-url", n, url); err != nil {
				return "", "", fmt.Errorf("%s", firstLine(err))
			}
		default:
			continue
		}
		if n == HubRemoteAtrium {
			note = movedNote
		}
		return n, note, nil
	}
	return "", "", fmt.Errorf("both `hub` and `atrium-hub` exist and point elsewhere, so both were left alone")
}
