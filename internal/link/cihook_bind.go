package link

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/dovholuknf/atrium/internal/edge"
)

// The ziti-hook bind, and the hub's loopback door for the same handler. See docs/fabric/ci-hook-design.md, 1.

const (
	ciBindFirst = 30 * time.Second
	ciBindCap   = 10 * time.Minute
	// ciBindStable is how long a listener has to serve before a later failure is news again.
	ciBindStable = time.Minute
	// CIListenerKind is the row in the hub's listener states.
	CIListenerKind = "ci-hook"
)

// CIBinder binds the ziti-hook service and keeps trying. A controller that is down at start leaves the hub up without
// the hook, and a missed webhook is not recoverable by the sender, so unlike the board it never gives up.
type CIBinder struct {
	Listen  func() (net.Listener, error)
	Handler http.Handler
	// State records the listener row: up, or down with why.
	State func(up bool, err error)
	Audit func(kind, detail string)
	// Wait sleeps for d and says whether to go on. Nil is a real timer, and a test injects one that does not wait.
	Wait  func(ctx context.Context, d time.Duration) bool
	First time.Duration
	Cap   time.Duration
}

func (b *CIBinder) audit(kind, detail string) {
	if b.Audit != nil {
		b.Audit(kind, detail)
	}
}

func (b *CIBinder) state(up bool, err error) {
	if b.State != nil {
		b.State(up, err)
	}
}

func waitTimer(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// Run binds, serves, and goes back to binding when the listener dies, until the context ends. The first failure and
// each recovery are one audit line.
func (b *CIBinder) Run(ctx context.Context) {
	first, limit, wait := b.First, b.Cap, b.Wait
	if first <= 0 {
		first = ciBindFirst
	}
	if limit <= 0 {
		limit = ciBindCap
	}
	if wait == nil {
		wait = waitTimer
	}
	delay := first
	downSaid, upSaid := false, false
	for ctx.Err() == nil {
		ln, err := b.Listen()
		if err == nil {
			b.state(true, nil)
			if !upSaid {
				upSaid = true
				b.audit("ci-hook-up", "the ci hook is listening")
			}
			delay = first
			began := time.Now()
			err = b.serve(ctx, ln)
			if ctx.Err() != nil {
				return
			}
			if time.Since(began) >= ciBindStable {
				downSaid = false
			}
		}
		b.state(false, err)
		if !downSaid {
			downSaid, upSaid = true, false
			b.audit("ci-hook-down", err.Error())
		}
		if !wait(ctx, delay) {
			return
		}
		if delay *= 2; delay > limit {
			delay = limit
		}
	}
}

func (b *CIBinder) serve(ctx context.Context, ln net.Listener) error {
	srv := &http.Server{
		Handler:           b.Handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		select {
		case <-ctx.Done():
			_ = srv.Close()
		case <-stopped:
		}
	}()
	err := srv.Serve(ln)
	_ = ln.Close()
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		err = errors.New("the listener closed")
	}
	return err
}

// ListenService binds another service with the same identity, and so the same ziti context as the link and the board.
func (z *Ziti) ListenService(name string) (net.Listener, error) {
	ctx, err := z.context()
	if err != nil {
		return nil, err
	}
	ln, err := ctx.Listen(name)
	if err != nil {
		return nil, errors.New("could not bind the ziti service " + name + ". does this identity have a bind policy for it: " +
			err.Error())
	}
	return ln, nil
}

var ciNoticeSeq atomic.Uint64

// ciNotify puts one post in front of the operator as a growler on the board, the channel a new push device and a change
// request already use. It hangs on the first room by name because a growler needs one. A hub with no room attached, or
// none wired for growlers, says nothing here and the audit line is still written.
func (p *Proxy) ciNotify(title, body string) {
	g := p.growler()
	if g == nil {
		return
	}
	rs, ok := g.st.(growlRaiser)
	if !ok {
		return
	}
	rooms := p.hub.Rooms()
	if len(rooms) == 0 {
		return
	}
	names := make([]string, 0, len(rooms))
	for _, r := range rooms {
		names = append(names, r.Name)
	}
	sort.Strings(names)
	id := fmt.Sprintf("ci|%d|%d", time.Now().UnixNano(), ciNoticeSeq.Add(1))
	raised, err := rs.Raise(names[0], GrowlRow{ID: id, Reason: ReasonCI, Title: title, Body: body})
	if err != nil {
		log.Printf("[hub] growlers could not raise a ci hook notice: %v", err)
		return
	}
	if raised {
		g.publish(nil)
	}
}

// SetCIHook turns the hook on for this hub. A hub that never calls it answers /_hub/ci-hook 404.
func (p *Proxy) SetCIHook(h *CIHook) {
	if h.Tell == nil {
		h.Tell = p.ciNotify
	}
	p.mu.Lock()
	p.ci = h
	p.mu.Unlock()
}

func (p *Proxy) ciHook() *CIHook {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ci
}

// serveCIHook answers POST /_hub/ci-hook and GET or PUT /_hub/ci-hook/routes. Loopback only: it is not a second public
// door, so a share or an overlay request is a 403.
func (p *Proxy) serveCIHook(w http.ResponseWriter, r *http.Request, sub string) {
	h := p.ciHook()
	if h == nil {
		http.NotFound(w, r)
		return
	}
	if !edge.LocalOperator(r) {
		ciReply(w, http.StatusForbidden, map[string]string{
			"error": "the ci hook is reached from the machine the hub runs on, or over its ziti service" +
				edge.ProxyNote(r)})
		return
	}
	switch {
	case sub == "ci-hook" && r.Method == http.MethodPost:
		h.Receive(w, r)
	case sub == "ci-hook/routes" && r.Method == http.MethodGet:
		raw, err := h.Store.CIHookRoutes()
		if err != nil {
			ciReply(w, http.StatusInternalServerError, map[string]string{"error": "could not read the routes"})
			return
		}
		routes, _ := ParseCIRoutes(raw)
		if routes == nil {
			routes = []CIRoute{}
		}
		repos, _ := h.Store.CIHookRepos()
		if repos == nil {
			repos = []string{}
		}
		ciReply(w, http.StatusOK, map[string]any{"routes": routes, "repos": repos})
	case sub == "ci-hook/routes" && r.Method == http.MethodPut:
		h.putRoutes(w, r)
	default:
		ciReply(w, http.StatusMethodNotAllowed, map[string]string{"error": "that has to be a POST to /_hub/ci-hook, " +
			"or a GET or PUT to /_hub/ci-hook/routes"})
	}
}

// putRoutes takes a list of rules, or {"routes": [...], "repos": [...]}. One bad rule refuses the whole list.
func (h *CIHook) putRoutes(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 256<<10))
	if err != nil {
		ciReply(w, http.StatusBadRequest, map[string]string{"error": "the body could not be read"})
		return
	}
	body := strings.TrimSpace(string(raw))
	var repos []string
	var setRepos bool
	if strings.HasPrefix(body, "{") {
		var in struct {
			Routes json.RawMessage `json:"routes"`
			Repos  *[]string       `json:"repos"`
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			ciReply(w, http.StatusBadRequest, map[string]string{"error": "that is not JSON"})
			return
		}
		body = strings.TrimSpace(string(in.Routes))
		if in.Repos != nil {
			repos, setRepos = *in.Repos, true
			for _, rp := range repos {
				if !ciRepoRe.MatchString(strings.TrimSpace(rp)) {
					ciReply(w, http.StatusBadRequest, map[string]string{"error": "repos are owner/name"})
					return
				}
			}
		}
	}
	routes, err := ParseCIRoutes(body)
	if err != nil {
		ciReply(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	norm := ""
	if len(routes) > 0 {
		b, _ := json.Marshal(routes)
		norm = string(b)
	}
	if err := h.Store.SetCIHookRoutes(norm); err != nil {
		ciReply(w, http.StatusInternalServerError, map[string]string{"error": "could not store the routes"})
		return
	}
	if setRepos {
		if err := h.Store.SetCIHookRepos(repos); err != nil {
			ciReply(w, http.StatusInternalServerError, map[string]string{"error": "could not store the repos"})
			return
		}
	}
	h.audit("ci-hook-routes", "the ci hook routes were set")
	ciReply(w, http.StatusOK, map[string]any{"ok": true, "rules": len(routes)})
}
