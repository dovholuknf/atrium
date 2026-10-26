package link

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/dovholuknf/atrium/internal/link/wire"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/base/remoteaccess"
)

// An ssh stream: bytes from a caller to a room's own sshd, through the hub. See docs/rnd/ssh-via-hub.md.
//
// ATRIUM CARRIES BYTES AND NOTHING ELSE. No key, password or agent socket passes through here, and the bytes are
// counted and never read. ssh runs end to end inside the stream and the login is sshd's.
//
//	caller --(A)--> HUB --(B)--> room --(C)--> sshd 127.0.0.1:port
//
// (A) is a loopback request on the hub's board (stream_door.go) or a `stream` connection a room dialled. (B) is one
// data connection borrowed from the target room's pool and spent on this stream, never returned. (C) is a plain TCP
// dial the room makes. After each hop's `101` the connection is raw bytes.

// streamKind is a room asking the hub to carry an ssh stream to another room.
const streamKind = wire.StreamKind

// StreamUpgrade is the Upgrade token of the hub-to-room request and of the caller's own request.
const StreamUpgrade = "atrium-stream"

// MaxStreams is how many ssh streams one target room may have open. A stream holds a data connection for its whole
// life and the pool keeps 64 idle at most.
const MaxStreams = 16

// streamLinger is how long a half closed stream may sit with its other direction open before both ends close, so a
// peer that never finishes its half cannot hold a data connection forever.
var streamLinger = 30 * time.Second

// spliceBuf is the whole of the memory a direction owns. Backpressure is the transport's, never a buffer of ours.
const spliceBuf = 32 << 10

// ErrStreamOld is a hub that predates streams.
var ErrStreamOld = errors.New("the hub does not carry ssh streams yet. update the hub")

// ErrStreamNotAttached is a room that is not attached, asked to open a stream.
var ErrStreamNotAttached = errors.New("this room is not attached to a hub right now")

// StreamError is a refusal in the operator's words, with the HTTP status it travels as.
type StreamError struct {
	Status int
	Msg    string
}

func (e *StreamError) Error() string { return e.Msg }

func streamErr(status int, format string, a ...any) *StreamError {
	return &StreamError{Status: status, Msg: fmt.Sprintf(format, a...)}
}

// ── splice ──────────────────────────────────────────────

// Why a stream ended, as a fact about who ended it.
const (
	endCaller  = "the caller"
	endTarget  = "the target"
	endLinger  = "the linger bound"
	endStopped = "remote access being turned off"
)

// spliceResult is what crossed. ToB is bytes written to b (a's data), FromB bytes read from b.
type spliceResult struct {
	ToB, FromB int64
	EndedBy    string
}

// splice joins a and b until both directions end. a is the caller's side and b the target's.
//
// HALF CLOSE FIRST. When a direction reads EOF the other side's write half is closed where the connection has one, so
// ssh's own shutdown passes through intact. The other direction then has `linger` to finish. Any error closes both at
// once. `stop` closes both at once too, which is how turning remote access off ends a stream.
func splice(a, b net.Conn, linger time.Duration, stop <-chan struct{}) spliceResult {
	type end struct {
		toB bool
		n   int64
		err error
	}
	done := make(chan end, 2)
	cp := func(dst, src net.Conn, toB bool) {
		buf := make([]byte, spliceBuf)
		n, err := io.CopyBuffer(dst, src, buf)
		done <- end{toB, n, err}
	}
	go cp(b, a, true)
	go cp(a, b, false)

	var res spliceResult
	closeBoth := func() { _ = a.Close(); _ = b.Close() }
	var timer <-chan time.Time
	first := true
	for got := 0; got < 2; {
		select {
		case e := <-done:
			got++
			if e.toB {
				res.ToB = e.n
			} else {
				res.FromB = e.n
			}
			if first {
				first = false
				if res.EndedBy == "" {
					if e.toB {
						res.EndedBy = endCaller
					} else {
						res.EndedBy = endTarget
					}
				}
			}
			if e.err != nil {
				closeBoth()
				continue
			}
			if got == 1 {
				if e.toB {
					closeWrite(b)
				} else {
					closeWrite(a)
				}
				t := time.NewTimer(linger)
				defer t.Stop()
				timer = t.C
			}
		case <-timer:
			timer = nil
			if res.EndedBy != endStopped {
				res.EndedBy += ", then " + endLinger
			}
			closeBoth()
		case <-stop:
			stop = nil
			res.EndedBy = endStopped
			closeBoth()
		}
	}
	closeBoth()
	return res
}

// closeWrite half closes c where it can, looking through the link's buffered wrapper.
func closeWrite(c net.Conn) {
	for {
		switch v := c.(type) {
		case interface{ CloseWrite() error }:
			_ = v.CloseWrite()
			return
		case *buffered:
			c = v.Conn
		default:
			return
		}
	}
}

// ── the hub's side ──────────────────────────────────────

// Stream is a stream the target room has said yes to: its connection is spent on this. Run it with the caller's
// connection and it audits itself and counts itself out of the room's cap.
type Stream struct {
	ID     string
	Room   string
	Caller string
	Claim  string
	Via    string

	h     *Hub
	a     *attached
	conn  net.Conn
	start time.Time
}

// OpenStream asks `room` for a stream to its sshd, as `caller` (`hub`, or a calling room's name from its certificate).
//
// THE ROOM'S ANSWER IS THE ONE THAT COUNTS. The checks here are quick refusals from what the room last said, and cost
// the room nothing. A hub with a stale or wrong view can only refuse early, never grant: the room checks its own file
// again at the moment of the request.
func (h *Hub) OpenStream(ctx context.Context, room, caller, claim string) (*Stream, error) {
	id := newStreamID()
	st, err := h.openStream(ctx, id, room, caller, claim)
	if err != nil {
		h.auditStream(room, "ssh-refused", fmt.Sprintf("%s %s -> %s: %s", id, caller, room, err.Error()))
	}
	return st, err
}

func (h *Hub) openStream(ctx context.Context, id, room, caller, claim string) (*Stream, error) {
	h.mu.Lock()
	a := h.rooms[keyOf(room)]
	h.mu.Unlock()
	if a == nil {
		return nil, streamErr(http.StatusServiceUnavailable, "%s", h.offlineSentence(room))
	}
	a.mu.Lock()
	unproven, acc, open := a.unproven, a.access, a.streams
	a.mu.Unlock()
	switch {
	case unproven != "":
		return nil, streamErr(http.StatusForbidden, "ssh streams need rooms that proved their names. %s", RejoinSentence(room))
	case acc == nil:
		return nil, streamErr(http.StatusServiceUnavailable, "room %s is older than remote access. update it", a.name)
	case !acc.On:
		return nil, streamErr(http.StatusForbidden,
			"%s has remote access off. turn it on at that machine: atrium room remote-access on", a.name)
	case !acc.Allows(caller):
		return nil, streamErr(http.StatusForbidden, "%s does not offer ssh to %s", a.name, caller)
	case open >= MaxStreams:
		return nil, streamErr(http.StatusTooManyRequests, "%s already has %d open ssh streams", a.name, MaxStreams)
	}

	conn, err := h.streamConn(ctx, a, id, caller)
	if err != nil {
		return nil, err
	}
	s := &Stream{ID: id, Room: a.name, Caller: caller, Claim: claim, h: h, a: a, conn: conn, start: time.Now()}
	if b, ok := conn.(*buffered); ok {
		s.Via = b.via
	}
	if s.Via == "" {
		s.Via = a.via
	}
	a.mu.Lock()
	a.streams++
	a.mu.Unlock()
	return s, nil
}

// streamConn borrows a data connection, asks for the stream on it and returns it after the room's 101.
func (h *Hub) streamConn(ctx context.Context, a *attached, id, caller string) (net.Conn, error) {
	var lastErr error
	// A pooled connection may have died while it sat there. One fresh one is worth trying, no more.
	for try := 0; try < 2; try++ {
		c, err := h.Dial(ctx, a.name)
		if err != nil {
			if errors.Is(err, ErrNoRoom) {
				return nil, streamErr(http.StatusServiceUnavailable, "%s", h.offlineSentence(a.name))
			}
			return nil, streamErr(http.StatusGatewayTimeout, "%s did not hand over a connection: %v", a.name, err)
		}
		// Bounded while the room answers. Cleared again before the splice, so a deadline set while the connection was
		// pooled cannot cut an ssh session short.
		_ = c.SetDeadline(time.Now().Add(handshakeWait + 10*time.Second))
		req := fmt.Sprintf("GET /_link/ssh HTTP/1.1\r\nHost: atrium-link\r\nConnection: Upgrade\r\nUpgrade: %s\r\n"+
			"X-Atrium-Caller: %s\r\nX-Atrium-Stream: %s\r\n\r\n", StreamUpgrade, caller, id)
		if _, err := io.WriteString(c, req); err != nil {
			c.Close()
			lastErr = err
			continue
		}
		br := bufio.NewReader(c)
		resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodGet})
		if err != nil {
			c.Close()
			lastErr = err
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				continue
			}
			break
		}
		if resp.StatusCode != http.StatusSwitchingProtocols {
			var body struct {
				Error string `json:"error"`
			}
			_ = json.NewDecoder(io.LimitReader(resp.Body, 4<<10)).Decode(&body)
			resp.Body.Close()
			c.Close()
			if body.Error == "" {
				body.Error = fmt.Sprintf("%s answered %s", a.name, resp.Status)
			}
			status := resp.StatusCode
			if status < 400 {
				status = http.StatusBadGateway
			}
			return nil, &StreamError{Status: status, Msg: body.Error}
		}
		_ = c.SetDeadline(time.Time{})
		out := &buffered{Conn: c, br: br}
		if b, ok := c.(*buffered); ok {
			out.via = b.via
		}
		return out, nil
	}
	return nil, streamErr(http.StatusBadGateway, "%s did not answer the stream request: %v", a.name, lastErr)
}

// Run splices the caller's connection to the room's and returns when the stream is over. It audits the open and the
// close, and counts itself out of the room's cap. `stop` ends it early.
func (s *Stream) Run(client net.Conn, stop <-chan struct{}) {
	defer func() {
		s.a.mu.Lock()
		s.a.streams--
		s.a.mu.Unlock()
	}()
	detail := fmt.Sprintf("%s %s -> %s", s.ID, s.Caller, s.Room)
	if s.Via != "" {
		detail += " over " + s.Via
	}
	if s.Claim != "" {
		detail += " claims " + s.Claim
	}
	s.h.auditStream(s.Room, "ssh-open", detail)
	res := splice(client, s.conn, streamLinger, stop)
	s.h.auditStream(s.Room, "ssh-close", fmt.Sprintf("%s %s, %s to %s, %s from %s, ended by %s",
		s.ID, time.Since(s.start).Round(time.Second), byteCount(res.ToB), s.Room, byteCount(res.FromB), s.Room, res.EndedBy))
}

// Abort gives up a stream that was opened and will not run, so its connection and its slot are released.
func (s *Stream) Abort() {
	_ = s.conn.Close()
	s.a.mu.Lock()
	s.a.streams--
	s.a.mu.Unlock()
}

func (h *Hub) auditStream(room, kind, detail string) {
	log.Printf("[hub] %s %s: %s", kind, room, detail)
	if h.OnStream != nil {
		h.OnStream(room, kind, detail)
	}
}

func (h *Hub) offlineSentence(room string) string {
	if h.LastSeen != nil {
		if at, ok := h.LastSeen(room); ok && !at.IsZero() {
			return fmt.Sprintf("room %s is offline (last seen %s)", room, at.Local().Format("2006-01-02 15:04"))
		}
	}
	return fmt.Sprintf("room %s is offline", room)
}

// serveStream is the `stream` kind: a room asking for a stream to another room. The caller is named by its
// certificate and never by its hello.
func (h *Hub) serveStream(name string, conn net.Conn, br *bufio.Reader) {
	if h.Authenticated != nil && identify(conn) == "" {
		_ = writeJSON(conn, welcome{OK: false, Error: "ssh streams need rooms that proved their names. " + RejoinSentence(name)})
		return
	}
	if !h.Has(name) {
		_ = writeJSON(conn, welcome{OK: false, Error: "attach to this hub before asking it to carry a stream"})
		return
	}
	if err := writeJSON(conn, welcome{OK: true}); err != nil {
		return
	}
	var req struct {
		Room  string `json:"room"`
		Claim string `json:"claim"`
	}
	_ = conn.SetDeadline(time.Now().Add(handshakeWait))
	if err := readJSON(br, &req); err != nil || strings.TrimSpace(req.Room) == "" {
		_ = writeJSON(conn, streamAnswer{Error: "name the room to reach"})
		return
	}
	_ = conn.SetDeadline(time.Time{})
	st, err := h.OpenStream(context.Background(), req.Room, name, CleanClaim(req.Claim))
	if err != nil {
		_ = writeJSON(conn, streamAnswer{Error: err.Error()})
		return
	}
	if err := writeJSON(conn, streamAnswer{OK: true}); err != nil {
		st.Abort()
		return
	}
	st.Run(&buffered{Conn: conn, br: br}, nil)
}

// streamAnswer is the hub's one line to a calling room, after which the connection is raw bytes.
type streamAnswer struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// CleanClaim bounds what a caller says about itself: one printable line. It is recorded as a claim and never trusted.
func CleanClaim(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	if len(s) > 100 {
		s = s[:100]
	}
	return strings.TrimSpace(s)
}

func newStreamID() string { return "st-" + newSession()[:12] }

func byteCount(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// remoteHost is the address the room dials for its own sshd.
func remoteHost(r RemoteAccess) string {
	if r.Addr == "" {
		return remoteaccess.DefaultAddr
	}
	return r.Addr
}

// ── the room's side ─────────────────────────────────────

// openStream is one stream the room is carrying.
type openStream struct {
	id, caller string
	stop       chan struct{}
	once       sync.Once
}

func (o *openStream) end() { o.once.Do(func() { close(o.stop) }) }

// linkHandler is what the room serves on the link, and ONLY on the link: the board's handler, with `/_link/ssh` in
// front of it. The room's own loopback board never sees it. Everything else under `/_link/` is a 404, so a path a
// browser could ask for through the hub is not a route on the room.
func (r *Room) linkHandler() http.Handler {
	next := r.Handler
	if next == nil {
		next = http.DefaultServeMux
	}
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.URL.Path == "/_link/ssh":
			r.serveSSH(w, req)
		case req.URL.Path == "/v1/link/transports":
			r.transportsHandler(w, req)
		case req.URL.Path == "/_link" || strings.HasPrefix(req.URL.Path, "/_link/"):
			http.NotFound(w, req)
		default:
			next.ServeHTTP(w, req)
		}
	})
}

func (r *Room) streamRefusal(w http.ResponseWriter, id, caller string, status int, msg string) {
	if r.RemoteDir != "" {
		remoteaccess.AppendLog(r.RemoteDir, remoteaccess.LogLine{Event: "refused", ID: id, Caller: caller, Error: msg,
			End: time.Now()})
	}
	log.Printf("[link] ssh stream %s from %q refused: %s", id, caller, msg)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// readAccess is the switch as the room acts on it. A room with no key directory is off.
func readAccess(dir string) RemoteAccess {
	if dir == "" {
		return RemoteAccess{}
	}
	st := remoteaccess.Effective(dir)
	return RemoteAccess{On: st.On, From: st.From, Addr: st.Addr, Port: st.Port, Forced: st.Forced}
}

// serveSSH is (B) and (C): the hub asking this room for its own sshd.
func (r *Room) serveSSH(w http.ResponseWriter, req *http.Request) {
	r.serving.Add(1)
	defer r.serving.Done()
	id := req.Header.Get("X-Atrium-Stream")
	caller := req.Header.Get("X-Atrium-Caller")
	if req.Method != http.MethodGet || !strings.EqualFold(req.Header.Get("Upgrade"), StreamUpgrade) ||
		!headerHas(req.Header, "Connection", "upgrade") || id == "" || caller == "" {
		r.streamRefusal(w, id, caller, http.StatusBadRequest, "that is not a stream request")
		return
	}
	st := readAccess(r.RemoteDir)
	if !st.On {
		r.streamRefusal(w, id, caller, http.StatusForbidden,
			r.Name+" has remote access off. turn it on at that machine: atrium room remote-access on")
		return
	}
	// THE ROOM'S OWN FILE, AGAIN, whatever the hub believes. See OpenStream.
	if !st.Allows(caller) {
		r.streamRefusal(w, id, caller, http.StatusForbidden, r.Name+" does not offer ssh to "+caller)
		return
	}
	addr := net.JoinHostPort(remoteHost(st), fmt.Sprint(st.Port))
	d := net.Dialer{Timeout: 10 * time.Second}
	sshd, err := d.DialContext(req.Context(), "tcp", addr)
	if err != nil {
		r.streamRefusal(w, id, caller, http.StatusBadGateway,
			fmt.Sprintf("%s could not reach its sshd on %s: %v", r.Name, addr, err))
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		sshd.Close()
		r.streamRefusal(w, id, caller, http.StatusInternalServerError, "this connection cannot be taken over")
		return
	}
	c, rw, err := hj.Hijack()
	if err != nil {
		sshd.Close()
		return
	}
	_ = c.SetDeadline(time.Time{})
	if _, err := io.WriteString(c, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: "+StreamUpgrade+"\r\n\r\n"); err != nil {
		c.Close()
		sshd.Close()
		return
	}
	// Bytes the server already read are the first of ssh's, and go onward first.
	hub := &buffered{Conn: c, br: rw.Reader}

	o := &openStream{id: id, caller: caller, stop: make(chan struct{})}
	r.addStream(o)
	start := time.Now()
	via := r.viaNow()
	r.logStream(remoteaccess.LogLine{Event: "open", ID: id, Caller: caller, Via: via, Start: start})
	log.Printf("[link] ssh stream %s from %q to %s opened", id, caller, addr)

	res := splice(hub, sshd, streamLinger, o.stop)

	r.dropStream(o)
	ended := res.EndedBy
	switch {
	case strings.HasPrefix(ended, endCaller):
		ended = strings.Replace(ended, endCaller, "the hub side", 1)
	case strings.HasPrefix(ended, endTarget):
		ended = strings.Replace(ended, endTarget, "sshd", 1)
	}
	r.logStream(remoteaccess.LogLine{Event: "close", ID: id, Caller: caller, Via: via, Start: start, End: time.Now(),
		BytesIn: res.ToB, BytesOut: res.FromB, EndedBy: ended})
	log.Printf("[link] ssh stream %s from %q closed after %s, %s to sshd, %s from it, ended by %s", id, caller,
		time.Since(start).Round(time.Second), byteCount(res.ToB), byteCount(res.FromB), ended)
}

func (r *Room) logStream(l remoteaccess.LogLine) {
	if r.RemoteDir != "" {
		remoteaccess.AppendLog(r.RemoteDir, l)
	}
}

func headerHas(h http.Header, name, token string) bool {
	for _, v := range h.Values(name) {
		for _, t := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(t), token) {
				return true
			}
		}
	}
	return false
}

func (r *Room) addStream(o *openStream) {
	r.smu.Lock()
	if r.streams == nil {
		r.streams = map[string]*openStream{}
	}
	r.streams[o.id] = o
	r.smu.Unlock()
	r.pokeAccess()
}

func (r *Room) dropStream(o *openStream) {
	r.smu.Lock()
	delete(r.streams, o.id)
	r.smu.Unlock()
	r.pokeAccess()
}

// OpenStreams is how many streams this room is carrying.
func (r *Room) OpenStreams() int {
	r.smu.Lock()
	defer r.smu.Unlock()
	return len(r.streams)
}

// endStreams ends every stream this room carries and waits for each handler to finish, its close line written. A
// stream's handler logs after the stream is over, so a caller that removes RemoteDir must wait here first.
func (r *Room) endStreams() {
	r.smu.Lock()
	var all []*openStream
	for _, o := range r.streams {
		all = append(all, o)
	}
	r.smu.Unlock()
	for _, o := range all {
		o.end()
	}
	r.serving.Wait()
}

func (r *Room) pokeAccess() {
	if r.accessPoke == nil {
		return
	}
	select {
	case r.accessPoke <- struct{}{}:
	default:
	}
}

// viaNow is the transport the room is attached over, for the record.
func (r *Room) viaNow() string {
	if cl, ok := r.Dial.(climber); ok {
		return cl.Transport()
	}
	return ""
}

// enforceAccess ends every open stream the switch no longer allows.
func (r *Room) enforceAccess() {
	st := readAccess(r.RemoteDir)
	r.smu.Lock()
	var drop []*openStream
	for _, o := range r.streams {
		if !st.Allows(o.caller) {
			drop = append(drop, o)
		}
	}
	r.smu.Unlock()
	for _, o := range drop {
		o.end()
	}
}

// watchAccess tells the hub what the switch says, at attach and on every change, and enforces it. Checked every
// Timings.Access, and at once when a stream opens or closes.
func (r *Room) watchAccess(ctx context.Context, send func(note) error) {
	var told []byte
	say := func() bool {
		a := readAccess(r.RemoteDir)
		a.Open = r.OpenStreams()
		raw, _ := json.Marshal(a)
		if string(raw) == string(told) {
			return true
		}
		told = raw
		return send(note{Access: &a}) == nil
	}
	if !say() {
		return
	}
	t := time.NewTicker(r.T.Access)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-r.accessPoke:
		}
		r.enforceAccess()
		if !say() {
			return
		}
	}
}

// OpenStream asks the hub for a stream to `target`'s sshd, from this room. The returned connection carries ssh's bytes
// and nothing else. An error is the sentence a person should read.
func (r *Room) OpenStream(ctx context.Context, target, claim string) (net.Conn, error) {
	r.mu.Lock()
	up, session := r.up, r.session
	r.mu.Unlock()
	if !up {
		return nil, ErrStreamNotAttached
	}
	dialCtx, cancel := context.WithTimeout(ctx, r.T.DialWait)
	conn, err := r.Dial.Dial(dialCtx)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("the hub is not answering: %v", err)
	}
	br := bufio.NewReader(conn)
	w, err := sayHello(conn, br, hello{Kind: streamKind, Room: r.Name, Session: session})
	if err != nil {
		conn.Close()
		// An older hub names the kinds it knows, and stream is not one of them.
		if !w.OK && strings.Contains(w.Error, "a connection is control") && !strings.Contains(w.Error, streamKind) {
			return nil, ErrStreamOld
		}
		if !w.OK && w.Error != "" {
			return nil, errors.New(w.Error)
		}
		return nil, fmt.Errorf("the hub is not answering: %v", err)
	}
	_ = conn.SetDeadline(time.Now().Add(handshakeWait + 15*time.Second))
	if err := writeJSON(conn, map[string]string{"room": target, "claim": CleanClaim(claim)}); err != nil {
		conn.Close()
		return nil, err
	}
	var ans streamAnswer
	if err := readJSON(br, &ans); err != nil {
		conn.Close()
		return nil, fmt.Errorf("the hub did not answer the stream request: %v", err)
	}
	if !ans.OK {
		conn.Close()
		return nil, errors.New(ans.Error)
	}
	_ = conn.SetDeadline(time.Time{})
	return &buffered{Conn: conn, br: br}, nil
}
