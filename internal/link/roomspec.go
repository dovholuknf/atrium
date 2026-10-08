package link

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

// A room's spec and lock, over the room link. See internal/hubstore/roomspec.go for what is stored and why.
//
//	roomspec kind, on the room's mutual TLS link, one HTTP request on the connection:
//	  GET  /_room/spec    the room's own room.yaml, as text, with its sha256 in X-Atrium-Spec-Sha256
//	  POST /_room/lock    the room's room.lock, a JSON object, replacing the last
//	GET /_hub/rooms/<name>/spec   the board's read of one room's spec, JSON
//	GET /_hub/rooms/<name>/lock   the board's read of one room's latest lock, JSON
//
// ── identified by the certificate and nothing else ──────
//
// The room dialled and the hub took its name out of the certificate, as it does for every kind. The path has no room
// in it, a room's headers and query are never read for one, and the hub sets RoomSpecRoomHeader itself over whatever
// arrived. A room can therefore only ever read and write its own spec and lock. The board's read is the only way to
// name a room, and it is read only: the spec is set by the operator with `atrium rooms spec set`, on the hub.
//
// ── bounded ─────────────────────────────────────────────
//
// A connection carries one request. The lock is limited to hubstore.MaxRoomLock, and the answer to a spec is at most
// hubstore.MaxRoomSpec.

const (
	roomSpecKind = "roomspec"
	// RoomSpecPrefix is the path the room asks, and RoomSpecRoomHeader carries the room the hello named.
	RoomSpecPrefix     = "/_room/"
	RoomSpecRoomHeader = "X-Atrium-Spec-Room"
	// RoomSpecHashHeader carries the sha256 of the YAML on a spec answer.
	RoomSpecHashHeader = "X-Atrium-Spec-Sha256"
)

// SetRoomSpecs wires the store the room link serves a room's spec and lock from. Without it the room link answers 404
// and /_hub/rooms/<name>/spec 404.
func (p *Proxy) SetRoomSpecs(st *hubstore.Store) {
	p.mu.Lock()
	p.specs = st
	p.mu.Unlock()
	if p.hub != nil {
		p.hub.RoomSpec = http.HandlerFunc(p.serveRoomSpec)
	}
}

func (p *Proxy) roomSpecs() *hubstore.Store {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.specs
}

// serveRoomSpec is the room's side of the link, as the room the hub named.
func (p *Proxy) serveRoomSpec(w http.ResponseWriter, r *http.Request) {
	st := p.roomSpecs()
	room := strings.TrimSpace(r.Header.Get(RoomSpecRoomHeader))
	if st == nil || room == "" {
		http.NotFound(w, r)
		return
	}
	switch {
	case r.URL.Path == RoomSpecPrefix+"spec" && r.Method == http.MethodGet:
		sp, err := st.RoomSpecOf(room)
		if err != nil {
			specFail(w, err)
			return
		}
		w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
		w.Header().Set(RoomSpecHashHeader, sp.SHA256)
		_, _ = io.WriteString(w, sp.YAML)
	case r.URL.Path == RoomSpecPrefix+"lock" && r.Method == http.MethodPost:
		raw, err := io.ReadAll(io.LimitReader(r.Body, hubstore.MaxRoomLock+1))
		if err != nil {
			crFail(w, http.StatusBadRequest, "could not read that lock")
			return
		}
		if _, err := st.PutRoomLock(room, raw); err != nil {
			specFail(w, err)
			return
		}
		crJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		http.NotFound(w, r)
	}
}

// specFail answers a store error: what is missing is 404, what the caller got wrong is 400, and anything else 500.
func specFail(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	switch {
	case errors.Is(err, hubstore.ErrNoRoomSpec), errors.Is(err, hubstore.ErrNoRoomLock),
		errors.Is(err, hubstore.ErrNoSuchRoom):
		crFail(w, http.StatusNotFound, err.Error())
	case hubstore.IsRefusal(err):
		crFail(w, http.StatusBadRequest, err.Error())
	default:
		crFail(w, http.StatusInternalServerError, err.Error())
	}
}

// serveRoomSpecBoard is GET /_hub/rooms/<name>/spec and /lock, read only.
func (p *Proxy) serveRoomSpecBoard(w http.ResponseWriter, r *http.Request, sub string) {
	st := p.roomSpecs()
	parts := strings.Split(strings.TrimPrefix(sub, "rooms/"), "/")
	if st == nil || len(parts) != 2 || (parts[1] != "spec" && parts[1] != "lock") {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		crFail(w, http.StatusMethodNotAllowed, "a room's spec and lock are read here with a GET")
		return
	}
	if parts[1] == "spec" {
		sp, err := st.RoomSpecOf(parts[0])
		if err != nil {
			specFail(w, err)
			return
		}
		crJSON(w, http.StatusOK, sp)
		return
	}
	lk, err := st.RoomLockOf(parts[0])
	if err != nil {
		specFail(w, err)
		return
	}
	crJSON(w, http.StatusOK, lk)
}

// ── the hub's side of the kind ──────────────────────────

// serveRoomSpec answers HTTP on one connection a room dialled, for the room the certificate named.
func (h *Hub) serveRoomSpec(name string, conn net.Conn, br *bufio.Reader) {
	if h.RoomSpec == nil {
		_ = writeJSON(conn, welcome{OK: false, Error: "this hub keeps no room specs. update the hub"})
		return
	}
	if err := writeJSON(conn, welcome{OK: true}); err != nil {
		return
	}
	one := newOneConn(&buffered{Conn: conn, br: br})
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// SET, NOT ADDED TO: whatever the room sent in this header is gone.
			r.Header.Set(RoomSpecRoomHeader, name)
			h.RoomSpec.ServeHTTP(w, r)
		}),
		ReadHeaderTimeout: gitHeaderWait,
		ReadTimeout:       gitHeaderWait,
		IdleTimeout:       time.Second,
		ConnState: func(_ net.Conn, s http.ConnState) {
			if s == http.StateClosed || s == http.StateHijacked {
				one.finish()
			}
		},
		ErrorLog: log.New(discard{}, "", 0),
	}
	_ = srv.Serve(one)
}

// ── the room's side ─────────────────────────────────────

// ErrNoRoomSpec is what a room is told when the hub holds no spec for it.
var ErrNoRoomSpec = errors.New("the hub has no spec for this room")

// roomSpecDo sends one request to the hub over a fresh roomspec connection and returns the status and body.
func roomSpecDo(ctx context.Context, d Dialer, room, method, path string, body []byte, limit int64) (
	*http.Response, []byte, error) {

	dctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	conn, err := d.Dial(dctx)
	if err != nil {
		return nil, nil, fmt.Errorf("the hub is not answering: %w", err)
	}
	defer conn.Close()
	br := bufio.NewReader(conn)
	if _, err := sayHello(conn, br, hello{Kind: roomSpecKind, Room: room}); err != nil {
		return nil, nil, err
	}
	rt := &http.Transport{
		DialContext:       func(context.Context, string, string) (net.Conn, error) { return &buffered{Conn: conn, br: br}, nil },
		DisableKeepAlives: true,
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://hub"+path, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := rt.RoundTrip(req)
	if err != nil {
		return nil, nil, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, limit))
	return res, b, err
}

// FetchRoomSpec asks the hub for this room's own spec over the room link. d is the room's dialer and room its name.
// It answers the YAML bytes and their sha256, or ErrNoRoomSpec when the hub has none.
func FetchRoomSpec(ctx context.Context, d Dialer, room string) (yaml []byte, sha256 string, err error) {
	res, b, err := roomSpecDo(ctx, d, room, http.MethodGet, RoomSpecPrefix+"spec", nil, hubstore.MaxRoomSpec+1)
	if err != nil {
		return nil, "", err
	}
	switch res.StatusCode {
	case http.StatusOK:
		return b, res.Header.Get(RoomSpecHashHeader), nil
	case http.StatusNotFound:
		return nil, "", ErrNoRoomSpec
	}
	return nil, "", fmt.Errorf("the hub answered %d: %s", res.StatusCode, specSaid(b))
}

// PostRoomLock sends this room's lock, a JSON object holding version 1, to the hub over the room link, where it
// replaces the last. d is the room's dialer and room its name. The hub never changes the spec because of it.
func PostRoomLock(ctx context.Context, d Dialer, room string, lock []byte) error {
	res, b, err := roomSpecDo(ctx, d, room, http.MethodPost, RoomSpecPrefix+"lock", lock, 1<<16)
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("the hub answered %d: %s", res.StatusCode, specSaid(b))
	}
	return nil
}

func specSaid(b []byte) string {
	var m struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(b, &m) == nil && m.Error != "" {
		return m.Error
	}
	return strings.TrimSpace(string(b))
}
