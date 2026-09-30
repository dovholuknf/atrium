package link

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

// The git kind: a room fetching a repository from its hub. See docs/rnd/git-sync-design.md.
//
// ── dialled by the room, like everything else ───────────
//
// The room dials, says hello with kind `git` and the control connection's session, and
// after the welcome the connection is plain HTTP/1.1 with the hub answering. A room's
// forwarder (internal/gitsync) dials one of these per TCP connection git opens to it, so
// no link connection outlives the git command that wanted it.
//
// ── and nothing is guessed ──────────────────────────────
//
// A hub says it serves git in its welcome and a room says it can take a sync in its hello.
// A room never dials this kind at a hub that did not say so, and a hub never asks a room
// that did not. Nothing is probed by being refused, and nothing is stored to remember it.

// gitKind is the connection kind. A hub that predates it refuses with the sentence in
// hearHello, and a room never gets that far because that hub did not say Git.
const gitKind = "git"

// ErrNoGit is a room that has no way to fetch from this hub right now.
var ErrNoGit = errors.New("this hub predates git sync (it did not say it serves git), or this room is not attached to it")

// ErrHubNoGit is ErrNoGit under the name the room side reads it by.
var ErrHubNoGit = ErrNoGit

// gitIdle bounds how long a git connection may sit between requests, and gitHeaderWait how
// long the hub waits for the first bytes of one. Neither may be unbounded: a link
// connection held open forever is a slot held forever.
const (
	gitIdle       = 30 * time.Second
	gitHeaderWait = 30 * time.Second
)

// ── the room's side ─────────────────────────────────────

// DialGit opens one `git` connection to the hub. What comes back is an ordinary HTTP/1.1
// socket, so it is the shape http.Transport's DialContext wants. The caller owns it.
//
// It answers ErrNoGit, without dialling, when the hub did not say Git in its welcome.
func (r *Room) DialGit(ctx context.Context) (net.Conn, error) {
	r.mu.Lock()
	up, session, ok := r.up, r.session, r.hubGit
	r.mu.Unlock()
	if !up || !ok {
		return nil, ErrNoGit
	}
	dialCtx, cancel := context.WithTimeout(ctx, r.T.DialWait)
	conn, err := r.Dial.Dial(dialCtx)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("the hub is not answering: %w", err)
	}
	br := bufio.NewReader(conn)
	w, err := sayHello(conn, br, hello{Kind: gitKind, Room: r.Name, Session: session})
	if err != nil {
		conn.Close()
		if !w.OK && w.Error != "" {
			return nil, fmt.Errorf("the hub refused a git connection: %s", w.Error)
		}
		return nil, fmt.Errorf("the hub is not answering: %w", err)
	}
	return &buffered{Conn: conn, br: br}, nil
}

// GitTransport is an http.RoundTripper whose every connection is a DialGit. For the
// forwarder a room's git command talks to.
func (r *Room) GitTransport() http.RoundTripper {
	return &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return r.DialGit(ctx)
		},
		MaxIdleConns:          4,
		IdleConnTimeout:       gitIdle / 2,
		ResponseHeaderTimeout: 0,
		ForceAttemptHTTP2:     false,
	}
}

// HubServesGit says whether the hub this room is attached to said it serves git.
func (r *Room) HubServesGit() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.up && r.hubGit
}

// ── the hub's side ──────────────────────────────────────

// serveGit answers HTTP on one connection a room dialled, with the hub's Git handler.
func (h *Hub) serveGit(name string, conn net.Conn, br *bufio.Reader) {
	if h.Git == nil {
		_ = writeJSON(conn, welcome{OK: false, Error: "this hub serves no repositories"})
		return
	}
	if !h.Has(name) {
		_ = writeJSON(conn, welcome{OK: false, Error: "attach to this hub before asking it for anything"})
		return
	}
	if err := writeJSON(conn, welcome{OK: true}); err != nil {
		return
	}
	one := newOneConn(&buffered{Conn: conn, br: br})
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h.Git.ServeHTTP(w, r)
		}),
		// BOTH BOUNDED, so a git client that opened a connection and went quiet does not
		// hold a link connection for ever.
		ReadHeaderTimeout: gitHeaderWait,
		IdleTimeout:       gitIdle,
		ConnState: func(_ net.Conn, s http.ConnState) {
			if s == http.StateClosed || s == http.StateHijacked {
				one.finish()
			}
		},
		ErrorLog: log.New(discard{}, "", 0),
	}
	_ = srv.Serve(one)
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

// oneConn is a listener that hands out one connection and then waits to be told it is
// done, so http.Server.Serve returns when that connection closes.
type oneConn struct {
	c    net.Conn
	once sync.Once
	done chan struct{}
	gave bool
	mu   sync.Mutex
}

func newOneConn(c net.Conn) *oneConn { return &oneConn{c: c, done: make(chan struct{})} }

func (o *oneConn) Accept() (net.Conn, error) {
	o.mu.Lock()
	first := !o.gave
	o.gave = true
	o.mu.Unlock()
	if first {
		return o.c, nil
	}
	<-o.done
	return nil, net.ErrClosed
}

func (o *oneConn) finish()        { o.once.Do(func() { close(o.done) }) }
func (o *oneConn) Close() error   { o.finish(); return nil }
func (o *oneConn) Addr() net.Addr { return hubAddr("atrium-link-git") }

// Transport is an http.RoundTripper to an attached room's handler, over the data pool the
// room dialled. For the hub's own git commands, which fetch from a room.
func (h *Hub) Transport(room string) http.RoundTripper {
	return &http.Transport{
		DialContext:       h.dialer(room),
		MaxIdleConns:      4,
		IdleConnTimeout:   gitIdle,
		ForceAttemptHTTP2: false,
	}
}

// RoomSaysGit reports whether an attached room said, in its hello, that it takes git
// syncs. A room that did not is never asked.
func (h *Hub) RoomSaysGit(room string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	a := h.rooms[keyOf(room)]
	return a != nil && a.git
}
