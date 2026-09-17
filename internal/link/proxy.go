package link

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"path"
	"strings"
	"sync"
	"time"
)

// What the hub actually answers with.
//
// ONE RULE, AND IT IS WORTH STATING BEFORE THE CODE: if the hub has the file,
// the hub serves it. Everything else goes to the room.
//
// That rule is the whole feature. `index.html`, the CSS and the JS are files
// the hub has, so changing them and restarting the hub changes the board.
// `/v1/tasks`, the event stream and the terminal websocket are not files, so
// they go to the process that owns the database and the pseudo terminals,
// which has been up for a week and is not being restarted.
//
// The board's JavaScript is untouched and cannot tell. Every URL it asks for is
// relative, so it asks whoever served the page, which is the hub.
//
// Resolved against the directory rather than against a list of prefixes. A list
// needs editing every time somebody adds a stylesheet, and the failure when
// they forget is a 404 in production for a file sitting on disk.

// Proxy serves the board from a hub.
type Proxy struct {
	hub   *Hub
	board fs.FS
	// boardID is the hash of THIS hub's board tree. See `rewriteHealth` for why
	// it is here and not taken from the room.
	boardID string
	room    func() string
	proxy   *httputil.ReverseProxy

	// clients are per-room, for the aggregate fan-out and the event streams.
	// The scoped proxy path does not use them: it goes through `proxy` and its
	// single transport.
	mu      sync.Mutex
	clients map[string]*http.Client
	// own is this hub running agents on its own machine, when it can.
	own OwnRoom

	// feeds is the one upstream event stream per room, and the boards watching
	// them. See events.go.
	feeds *feeds
}

// NewProxy wires a hub, its board and a room chooser into one handler.
//
// `boardID` is the hash of the board being served. Empty turns the rewrite off,
// which is right for a test and wrong for a running hub.
func NewProxy(hub *Hub, board fs.FS, boardID string, room func() string) *Proxy {
	p := &Proxy{hub: hub, board: board, boardID: boardID, room: room}
	p.feeds = newFeeds(p)

	p.proxy = &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			// A HOST PER ROOM, AND THE ROOM'S NAME IS IN IT ON PURPOSE.
			//
			// Nothing resolves this name: which connection the request goes
			// down is decided by `DialContext` below, from the room on the
			// context. But `http.Transport` KEYS ITS CONNECTION POOL BY HOST,
			// and `DialContext` only runs when the pool has nothing to reuse.
			//
			// With one fixed host, a request for beta would happily reuse an
			// idle connection dialled to alpha and land in the wrong room,
			// which is a click in the aggregate view opening somebody else's
			// card. Putting the room in the host gives each one its own pool.
			r.Out.URL.Scheme = "http"
			name, _ := r.Out.Context().Value(roomKey{}).(string)
			r.Out.URL.Host = hostFor(name)
			// THE TAG COMES OFF BEFORE THE ROOM SEES IT. A room minted the
			// bare id and knows nothing about `room~id`, so sending the tagged
			// form would be a 404 on every card clicked from an aggregate list.
			r.Out.URL.Path = untag(r.Out.URL.Path)
			// The browser's Host is forwarded, so anything building a link
			// builds one pointing at the hub, which is the address a browser
			// can reach. Nothing in the room reads it today, which is what
			// makes this safe rather than what makes it unnecessary.
			r.Out.Host = r.In.Host
			r.SetXForwarded()
		},
		Transport: &http.Transport{
			DialContext: p.dial,
			// KEEP-ALIVE IS THE POINT. One pooled connection answers hundreds
			// of requests, so the room dials rarely.
			MaxIdleConns:        64,
			MaxIdleConnsPerHost: 64,
			// COMFORTABLY MORE THAN THE EVENT STREAM'S KEEPALIVE, which is 25
			// seconds. An idle timeout under that would cut the stream between
			// pings and the board would spend its life reconnecting.
			IdleConnTimeout: 90 * time.Second,
			// ZERO, meaning no limit, and this is not an oversight. The event
			// stream sends its first byte when something happens, which may be
			// an hour after the request. A response header timeout would kill
			// exactly the endpoint the board depends on most.
			ResponseHeaderTimeout: 0,
			ExpectContinueTimeout: time.Second,
			// The link is already encrypted by the transport. There is no
			// second TLS layer inside it to configure.
			ForceAttemptHTTP2: false,
		},
		// IMMEDIATE FLUSH, and this is the single line that decides whether a
		// proxied atrium feels like atrium.
		//
		// -1 means every write goes straight out. The event stream and the
		// terminal both send small messages that matter the instant they
		// happen, and any buffering turns a live board into one that updates in
		// clumps.
		FlushInterval:  -1,
		ModifyResponse: p.rewriteHealth,
		ErrorHandler:   p.oops,
	}
	return p
}

func (p *Proxy) dial(ctx context.Context, _, _ string) (net.Conn, error) {
	// THE ROOM COMES OFF THE REQUEST, not off this connection.
	//
	// `http.Transport` calls this with no request in hand, so the room has to
	// arrive through the context. `Rewrite` puts it there, because that is the
	// last place both the request and the outbound connection are in scope.
	name, _ := ctx.Value(roomKey{}).(string)
	if name == "" && p.room != nil {
		name = p.room()
	}
	if name == "" {
		name = p.hub.Only()
	}
	if name == "" {
		return nil, ErrNoRoom
	}
	return p.hub.Dial(ctx, name)
}

// roomKey carries the chosen room from the rewrite into the dial.
type roomKey struct{}

// hostFor is the synthetic host a room's requests are addressed to.
//
// It exists to key the connection pool, and nothing ever resolves it. Lowercase
// because a host is case-insensitive and `Alpha` and `alpha` are one room, and
// because two spellings would be two pools to the same place.
func hostFor(room string) string {
	if room == "" {
		return "room.atrium.internal"
	}
	return keyOf(room) + ".room.atrium.internal"
}

// roomFor answers which room a request is for, and whether it named one.
//
// A HEADER ON JSON AND A QUERY PARAMETER ON THE WEBSOCKET, because a websocket
// cannot set a header. Both are read here so nothing else has to know there are
// two spellings.
//
// An id of the form `room~card` names a room too, and that is what makes an
// aggregate board clickable: the board got the id from a merged list and hands
// it straight back in the next url without knowing what it means.
func (p *Proxy) roomFor(r *http.Request) (name string, named bool) {
	if v := strings.TrimSpace(r.Header.Get(RoomHeader)); v != "" {
		return v, true
	}
	if v := strings.TrimSpace(r.URL.Query().Get(RoomParam)); v != "" {
		return v, true
	}
	if room, _ := splitTag(cardIDIn(r.URL.Path)); room != "" {
		return room, true
	}
	// Exactly one room needs no choosing. The operator was explicit: do not
	// ask when there is nothing to ask about.
	if only := p.hub.Only(); only != "" {
		return only, false
	}
	return "", false
}

// cardIDIn pulls the id out of `/v1/tasks/<id>/...`, which is the only shape
// that carries one.
func cardIDIn(path string) string {
	const pre = "/v1/tasks/"
	if !strings.HasPrefix(path, pre) {
		return ""
	}
	rest := strings.TrimPrefix(path, pre)
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		rest = rest[:i]
	}
	return rest
}

// untag rewrites `/v1/tasks/room~id/...` back to `/v1/tasks/id/...` on the way
// to a room, so the room sees the id it minted and needs to know nothing.
func untag(path string) string {
	id := cardIDIn(path)
	if id == "" {
		return path
	}
	room, bare := splitTag(id)
	if room == "" {
		return path
	}
	return strings.Replace(path, "/v1/tasks/"+id, "/v1/tasks/"+bare, 1)
}

// ServeHTTP is the rule.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// The hub's own, under a reserved prefix so it can never collide with a
	// board route the room grows later.
	if strings.HasPrefix(r.URL.Path, "/_hub/") {
		p.serveHubAPI(w, r)
		return
	}
	// STOPPING A ROOM IS NOT SOMETHING THE HUB MAY DO, and refusing it here is
	// closing a hole rather than withholding a feature.
	//
	// `POST /v1/shutdown` is guarded by a loopback check on `r.RemoteAddr`
	// (internal/daemon/shutdown.go). Through a link, the room sees a connection
	// that terminates inside its own process, so that check would pass for
	// anybody who could reach the hub. The rule the daemon already states for
	// shares applies here word for word: once something else can reach it,
	// loopback stops meaning "this machine".
	if r.URL.Path == "/v1/shutdown" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"error":"a hub cannot stop a room. `+
			`run atrium stop on the machine the room is on."}`)
		return
	}
	// THE EVENT STREAM IS NEVER PROXIED. It is fanned in once per room and
	// dealt back out, so a browser holds one stream rather than one per room
	// per tab, and so a stream can say which room it wants in its URL, which
	// is the only place an EventSource can say anything.
	if p.eventsFor(w, r) {
		return
	}
	if name, ok := p.asset(r.URL.Path); ok {
		p.serveAsset(w, r, name)
		return
	}

	room, named := p.roomFor(r)
	if room == "" {
		// NO ROOM NAMED AND MORE THAN ONE ATTACHED: the aggregate view.
		if r.URL.Path == "/v1/health" {
			p.health(w, r)
			return
		}
		if p.aggregate(w, r, r.URL.Path) {
			return
		}
		// A write, or a read nothing knows how to merge. Asked rather than
		// guessed at.
		if rooms := p.hub.Rooms(); len(rooms) > 1 {
			needsARoom(w, rooms)
			return
		}
	}
	// Carried on the context, which is the only thing that reaches the dial.
	r = r.WithContext(context.WithValue(r.Context(), roomKey{}, room))
	_ = named
	p.proxy.ServeHTTP(w, r)
}

// asset decides whether the hub has this file.
func (p *Proxy) asset(urlPath string) (string, bool) {
	if p.board == nil {
		return "", false
	}
	clean := path.Clean("/" + strings.TrimPrefix(urlPath, "/"))
	if clean == "/" {
		clean = "/index.html"
	}
	name := strings.TrimPrefix(clean, "/")
	if name == "" {
		return "", false
	}
	// `..` cannot survive path.Clean on an absolute path, so what is left is a
	// name under the board directory or nothing.
	f, err := p.board.Open(name)
	if err != nil {
		return "", false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		return "", false
	}
	return name, true
}

func (p *Proxy) serveAsset(w http.ResponseWriter, r *http.Request, name string) {
	f, err := p.board.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// The same two rules the daemon's own file server uses, for the same
	// reasons. See `webHandler` in internal/api/web.go: a vendored library
	// never changes, and a cached copy of everything else after a restart looks
	// exactly like a fix that did not work.
	if strings.HasPrefix(r.URL.Path, "/vendor/") {
		w.Header().Set("Cache-Control", "public, max-age=86400")
	} else {
		w.Header().Set("Cache-Control", "no-store, must-revalidate")
	}
	if rs, ok := f.(io.ReadSeeker); ok {
		http.ServeContent(w, r, name, st.ModTime(), rs)
		return
	}
	// An fs.FS is not obliged to give a seeker. Range requests are lost, which
	// costs nothing for a stylesheet.
	http.ServeContent(w, r, name, st.ModTime(), bytes.NewReader(readAll(f)))
}

func readAll(f fs.File) []byte {
	raw, err := io.ReadAll(f)
	if err != nil {
		return nil
	}
	return raw
}

// rewriteHealth replaces the room's board hash with the hub's.
//
// THE MOST IMPORTANT TWENTY LINES IN THIS FILE, because without them the board
// reload-loops and it looks like the whole idea does not work.
//
// `/v1/health` carries a `build` field: a hash of the board tree. The page
// remembers what it saw first and calls `location.reload()` when the answer
// changes, which is how a popped-out terminal left open for a day notices that
// atrium has been rebuilt under it.
//
// Split in two, the room answers `/v1/health` and hashes ITS OWN copy of the
// board, which is not the copy the browser is running. Every poll would then
// report a different build from the one the page loaded, every tab would
// reload, and every reload would do it again.
//
// So the hub answers for the board, because the hub IS the board. Everything
// else in the health payload is the room's and passes through untouched.
func (p *Proxy) rewriteHealth(res *http.Response) error {
	if p.boardID == "" || res.Request == nil || res.Request.URL.Path != "/v1/health" {
		return nil
	}
	if res.StatusCode != http.StatusOK {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	res.Body.Close()
	if err != nil {
		return err
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		// Not JSON we understand. Passed through rather than failed: health is
		// how the board decides atrium is up, and breaking it here would be
		// worse than a build id that is briefly wrong.
		res.Body = io.NopCloser(bytes.NewReader(raw))
		return nil
	}
	// Kept under its own name, so the room's build is still visible to anybody
	// debugging a version skew between the two halves.
	if was, ok := body["build"]; ok {
		body["room_build"] = was
	}
	body["build"] = p.boardID
	out, err := json.Marshal(body)
	if err != nil {
		res.Body = io.NopCloser(bytes.NewReader(raw))
		return nil
	}
	res.Body = io.NopCloser(bytes.NewReader(out))
	res.ContentLength = int64(len(out))
	res.Header.Set("Content-Length", fmt.Sprint(len(out)))
	return nil
}

// oops is what a request gets when the room cannot answer.
//
// IT SAYS WHICH HALF IS DOWN. "Bad gateway" on a split system sends somebody to
// read the wrong logs. A hub with no room is a normal state that happens every
// time a room restarts, and the board should say so in words.
func (p *Proxy) oops(w http.ResponseWriter, r *http.Request, err error) {
	code := http.StatusBadGateway
	msg := "the room did not answer: " + err.Error()
	if errors.Is(err, ErrNoRoom) {
		code = http.StatusServiceUnavailable
		msg = "no room is attached to this hub. the hub serves the board and holds nothing, " +
			"so until a room connects there is nothing to show. run `atrium2 join` on the " +
			"machine your agents are on."
	}
	// A dropped terminal or event stream is noise at this level: the client
	// reconnects by design, and logging every one buries the failures worth
	// reading.
	if !isStream(r) {
		log.Printf("[hub] %s %s: %v", r.Method, r.URL.Path, err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	fmt.Fprintf(w, `{"error":%q}`, msg)
}

func isStream(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/event-stream") ||
		strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

// OwnRoom is a hub that can also run agents on its own machine.
//
// AN INTERFACE RATHER THAN THE THING ITSELF, because starting a room means a
// database, a supervisor and pseudo terminals, and this package holds none of
// those and should not learn about them to own a switch. Whoever builds the hub
// implements it. A hub that cannot do it leaves this nil and the board asks
// nothing about it.
type OwnRoom interface {
	// On reports whether it is running right now.
	On() bool
	// Set starts or stops it, and remembers the answer for next time.
	Set(on bool) error
	// Name is what the room is called when it is on.
	Name() string
}

// SetOwnRoom wires that switch up. Optional: without it the board hides the
// control rather than offering one that cannot work.
func (p *Proxy) SetOwnRoom(o OwnRoom) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.own = o
}

func (p *Proxy) ownRoom() OwnRoom {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.own
}

// serveHubAPI answers the few things only the hub knows.
func (p *Proxy) serveHubAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch strings.TrimPrefix(r.URL.Path, "/_hub/") {
	case "rooms":
		_ = json.NewEncoder(w).Encode(map[string]any{"rooms": p.hub.Rooms()})
	case "room":
		p.serveOwnRoom(w, r)
	case "health":
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "rooms": len(p.hub.Rooms()),
			"only": p.hub.Only(), "build": p.boardID,
		})
	default:
		http.NotFound(w, r)
	}
}

// serveOwnRoom reads and sets whether this hub also runs agents.
//
// OFF IS THE DEFAULT AND TURNING IT ON IS A DECISION, which is why it is a
// switch a human throws rather than something that happens when a hub notices
// it could. A hub holding a database is a hub whose restart is no longer free,
// and that freedom is the whole reason the two halves are separate.
func (p *Proxy) serveOwnRoom(w http.ResponseWriter, r *http.Request) {
	own := p.ownRoom()
	if own == nil {
		// Not "off". Not available at all, which the board draws as nothing
		// rather than as a switch that would do nothing.
		_ = json.NewEncoder(w).Encode(map[string]any{"available": false})
		return
	}
	if r.Method == http.MethodPost {
		var body struct {
			On bool `json:"on"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `{"error":%q}`, "could not read that: "+err.Error())
			return
		}
		if err := own.Set(body.On); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, `{"error":%q}`, err.Error())
			return
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"available": true, "on": own.On(), "name": own.Name(),
	})
}
