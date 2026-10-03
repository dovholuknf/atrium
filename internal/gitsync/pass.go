package gitsync

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// A fetch passed through the hub to the room that has the work. See docs/rnd/hub-forge-design.md 3.3.
//
//	GET  /git/room/<room>/<repo>.git/info/refs?service=git-upload-pack
//	POST /git/room/<room>/<repo>.git/git-upload-pack
//
// goes to that room's link-only git route (`/v1/git/<name>.git/...`, room.go) over the data connections the room
// dialled, and the answer is streamed back.
//
// THE HUB STORES NOTHING. No directory is made, no object is written and no file is opened: the request body is
// read into memory (a want list, bounded), looked at, and sent on, and the answer is copied from the room's
// connection to the reader's. What may be fetched is what the ROOM serves (ServedHide), and a want outside that
// set is refused by the room's own git, so nothing here has an opinion about which refs are fine.
//
// WHAT THE HUB DOES ADD, because it is the one place that sees the reader:
//   - protocol v0: the Git-Protocol header is never sent on;
//   - a request carrying `shallow`, `deepen` or `filter` lines is refused BEFORE it is passed on;
//   - 503 `<room> is not connected` when the room is not attached, or does not answer;
//   - a rate per reader, and a cap on how many run at once for one room.

// PassPrefix is where the pass-through is served on the board.
const PassPrefix = "/git/room/"

// The limits of 3.3. A fetch is announced by one info/refs, and may then take several upload-pack rounds, so the
// rate counts the announcements (6 fetches a minute) and has its own, wider, count for rounds, so a client that
// skips the announcement is limited too. The running cap counts both kinds.
const (
	passFetchesPerMinute = 6
	passRoundsPerMinute  = 60
	passPerRoom          = 2
)

// THE DEADLINES, because a slot is held for as long as the request is open and the board's listeners set no
// read, write or idle timeout of their own (only ReadHeaderTimeout). A reader that stalls, a room that stalls and a
// body that stalls would each hold one of a room's two slots until the client closed, and a phone that drops off a
// share without a FIN would hold it until TCP gave up. Each deadline is for ONE read or ONE write, moved forward every
// time bytes move, so a large fetch that keeps making progress is never cut, however long it takes in all:
//
//	the request body     30 s with no byte arriving (a want list, at most 64 MiB)
//	the room's answer    2 min to its first byte (upload-pack may count objects first), then 60 s between reads
//	the reader's side    30 s for any one write or flush to be taken
const (
	passBodyIdle       = 30 * time.Second
	passRoomHeaderWait = 2 * time.Minute
	passRoomIdle       = 60 * time.Second
	passWriteIdle      = 30 * time.Second
)

// Reader is who is on the other end of a pass-through, as the listener knows it.
type Reader struct {
	// Reach names how the request arrived: `loopback`, `overlay` or `zrok-private`. It is the listener's mark.
	Reach string
	// Key tells readers apart for the rate. The listener sets it to the peer's address.
	Key string
}

type readerKey struct{}

// WithReader puts the reader on a context.
func WithReader(ctx context.Context, r Reader) context.Context {
	return context.WithValue(ctx, readerKey{}, r)
}

// ReaderFrom is the reader a listener put on the context.
func ReaderFrom(ctx context.Context) (Reader, bool) {
	r, ok := ctx.Value(readerKey{}).(Reader)
	return r, ok
}

// Pass is the pass-through handler. Get it from Hub.PassHandler.
type Pass struct {
	h *Hub
	// Now is the clock. Nil is time.Now.
	Now func() time.Time
	// FetchesPerMinute, RoundsPerMinute and PerRoom override the limits above when above zero. A test sets them.
	FetchesPerMinute, RoundsPerMinute, PerRoom int
	// BodyIdle, RoomHeaderWait, RoomIdle and WriteIdle override the deadlines above when above zero. A test sets them.
	BodyIdle, RoomHeaderWait, RoomIdle, WriteIdle time.Duration

	mu      sync.Mutex
	fetches map[string][]time.Time
	rounds  map[string][]time.Time
	running map[string]int
}

// PassHandler is the pass-through for this hub's rooms, made once. The listener must put a Reader on the context,
// and has already refused the reaches that may not fetch.
func (h *Hub) PassHandler() *Pass {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.pass == nil {
		h.pass = &Pass{h: h}
	}
	return h.pass
}

func dur(v, def time.Duration) time.Duration {
	if v > 0 {
		return v
	}
	return def
}

// idleReader is a body whose read deadline is moved forward before every read, so it is bounded by a stall and not
// by its length.
type idleReader struct {
	rc   *http.ResponseController
	body io.ReadCloser
	idle time.Duration
}

func (i *idleReader) Read(b []byte) (int, error) {
	// An error is a writer that has no deadlines (a test's recorder), and then there is nothing to move.
	_ = i.rc.SetReadDeadline(time.Now().Add(i.idle))
	return i.body.Read(b)
}

func (i *idleReader) Close() error { return i.body.Close() }

func (p *Pass) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

// splitPassPath reads `/git/room/<room>/<repo>.git/<tail>`, with the repository as `<owner>/<repo>` or
// `<host>/<owner>/<repo>` and answering the canonical name.
func splitPassPath(path string) (room, name, tail string, ok bool) {
	rest, found := strings.CutPrefix(path, PassPrefix)
	if !found {
		return "", "", "", false
	}
	room, rest, found = strings.Cut(rest, "/")
	if !found || room == "" {
		return "", "", "", false
	}
	i := strings.Index(rest, ".git/")
	if i < 0 {
		return "", "", "", false
	}
	raw, tail := rest[:i], rest[i+len(".git/"):]
	ref, err := ParseName(raw)
	if err != nil {
		return "", "", "", false
	}
	return room, ref.Name(), tail, true
}

func (p *Pass) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	esc := strings.ToLower(r.URL.EscapedPath())
	if strings.Contains(esc, "%2e") || strings.Contains(esc, "%5c") || strings.Contains(esc, "%2f") || strings.Contains(esc, "%00") {
		http.NotFound(w, r)
		return
	}
	reader, ok := ReaderFrom(r.Context())
	if !ok {
		http.Error(w, "the hub cannot tell who is asking", http.StatusForbidden)
		return
	}
	room, name, tail, ok := splitPassPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	switch tail {
	case "info/refs":
		if r.Method != http.MethodGet || r.URL.Query().Get("service") != "git-upload-pack" {
			http.Error(w, "this passes fetches and nothing else", http.StatusForbidden)
			return
		}
	case "git-upload-pack":
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-git-upload-pack-request" {
			http.Error(w, "this passes fetches and nothing else", http.StatusForbidden)
			return
		}
	case "git-receive-pack":
		http.Error(w, "this passes fetches and nothing else. push to the hub's own store", http.StatusForbidden)
		return
	default:
		http.NotFound(w, r)
		return
	}

	info, attached := p.room(room)
	if !attached {
		http.Error(w, room+" is not connected", http.StatusServiceUnavailable)
		return
	}
	if !info.Git {
		http.Error(w, info.Name+" does not serve git (its build predates it)", http.StatusServiceUnavailable)
		return
	}
	room = info.Name

	// The deadlines are cleared on the way out: the connection may carry the reader's next request, and a deadline
	// that had passed would break it.
	rc := http.NewResponseController(w)
	writeIdle := dur(p.WriteIdle, passWriteIdle)
	_ = rc.SetWriteDeadline(time.Now().Add(writeIdle))
	defer func() {
		_ = rc.SetReadDeadline(time.Time{})
		_ = rc.SetWriteDeadline(time.Time{})
	}()

	release, why := p.admit(room, reader.Key, tail == "info/refs")
	if release == nil {
		w.Header().Set("Retry-After", "10")
		http.Error(w, why, http.StatusTooManyRequests)
		return
	}
	defer release()

	// The request the room sees: the same service, protocol v0 (no Git-Protocol header, none is copied), and
	// no credential, cookie or card header of the reader's.
	target := "http://" + room + "/v1/git/" + name + ".git/" + tail
	if tail == "info/refs" {
		target += "?service=git-upload-pack"
	}
	var body io.Reader
	if tail == "git-upload-pack" {
		r.Body = &idleReader{rc: rc, body: r.Body, idle: dur(p.BodyIdle, passBodyIdle)}
		raw, err := readBody(r, maxRequest)
		if err != nil {
			if err == errTooBig {
				http.Error(w, "that request is too large", http.StatusRequestEntityTooLarge)
			} else {
				http.Error(w, "that is not a git fetch request", http.StatusBadRequest)
			}
			return
		}
		why, err := fetchRefusal(raw)
		if err != nil {
			http.Error(w, "that is not a git fetch request", http.StatusBadRequest)
			return
		}
		if why != "" {
			w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
			w.Header().Set("Cache-Control", "no-cache")
			_, _ = w.Write(pktLine("ERR atrium: " + why + "\n"))
			return
		}
		body = bytes.NewReader(raw)
		_ = rc.SetReadDeadline(time.Time{})
	} else {
		p.h.audit(room, "git-passed", name+" via "+reader.Reach)
	}
	// THE ROOM'S SIDE has a watchdog: it cancels the request to the room when the room has said nothing for too long,
	// and is moved forward whenever bytes arrive.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	var stalled atomic.Bool
	roomIdle := dur(p.RoomIdle, passRoomIdle)
	watchdog := time.AfterFunc(dur(p.RoomHeaderWait, passRoomHeaderWait), func() { stalled.Store(true); cancel() })
	defer watchdog.Stop()

	out, err := http.NewRequestWithContext(ctx, r.Method, target, body)
	if err != nil {
		http.Error(w, "that is not a request the hub can pass on", http.StatusBadRequest)
		return
	}
	if body != nil {
		out.Header.Set("Content-Type", "application/x-git-upload-pack-request")
	}
	out.Header.Set("Accept", r.Header.Get("Accept"))

	resp, err := p.h.Rooms.Transport(room).RoundTrip(out)
	if err != nil {
		_ = rc.SetWriteDeadline(time.Now().Add(writeIdle))
		if stalled.Load() {
			http.Error(w, room+" did not answer in time", http.StatusGatewayTimeout)
			return
		}
		http.Error(w, room+" is not connected", http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()
	for _, k := range []string{"Content-Type", "Cache-Control", "Pragma", "Expires"} {
		if v := resp.Header.Get(k); v != "" {
			w.Header().Set(k, v)
		}
	}
	_ = rc.SetWriteDeadline(time.Now().Add(writeIdle))
	w.WriteHeader(resp.StatusCode)
	buf := make([]byte, 32<<10)
	for {
		watchdog.Reset(roomIdle)
		n, err := resp.Body.Read(buf)
		if n > 0 {
			// A write that is not taken in time ends the request: a reader that stopped reading holds nothing.
			_ = rc.SetWriteDeadline(time.Now().Add(writeIdle))
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if ferr := rc.Flush(); ferr != nil && ferr != http.ErrNotSupported {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// room finds an attached room by name, without regard to case.
func (p *Pass) room(name string) (RoomInfo, bool) {
	if p.h.Rooms == nil {
		return RoomInfo{}, false
	}
	for _, a := range p.h.Rooms.Attached() {
		if strings.EqualFold(a.Name, name) {
			return a, true
		}
	}
	return RoomInfo{}, false
}

// admit takes a slot for one request to a room, or says why not. The release gives the slot back. A refused
// request holds nothing and counts for nothing, so a reader that is told to wait is not pushed further back by it.
func (p *Pass) admit(room, reader string, announce bool) (release func(), why string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	if p.fetches == nil {
		p.fetches, p.rounds, p.running = map[string][]time.Time{}, map[string][]time.Time{}, map[string]int{}
	}
	key := strings.ToLower(room)
	pick := func(v, def int) int {
		if v > 0 {
			return v
		}
		return def
	}
	if per := pick(p.PerRoom, passPerRoom); p.running[key] >= per {
		return nil, fmt.Sprintf("%d fetches from %s are running already. wait for one to finish", per, room)
	}
	count, limit, word := p.rounds, pick(p.RoundsPerMinute, passRoundsPerMinute), "requests"
	if announce {
		count, limit, word = p.fetches, pick(p.FetchesPerMinute, passFetchesPerMinute), "fetches"
	}
	recent := count[reader][:0]
	for _, t := range count[reader] {
		if now.Sub(t) < time.Minute {
			recent = append(recent, t)
		}
	}
	if len(recent) >= limit {
		count[reader] = recent
		return nil, "that is " + strconv.Itoa(limit) + " " + word + " a minute from one reader. wait a little"
	}
	count[reader] = append(recent, now)
	p.running[key]++
	var once sync.Once
	return func() {
		once.Do(func() {
			p.mu.Lock()
			p.running[key]--
			p.mu.Unlock()
		})
	}, ""
}
