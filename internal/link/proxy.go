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
	// stock is every room this hub knows about, attached or not.
	stock Inventory

	// feeds is the one upstream event stream per room, and the boards watching
	// them. See events.go.
	feeds *feeds

	// control is the hub-side control MCP server, mounted at /_hub/mcp. Nil
	// until SetControl wires it, and a hub without one answers that path 404.
	// See control_mcp.go.
	control http.Handler
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
		ModifyResponse: p.rewrite,
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

// taggedKey carries the room when it came from a `room~id` IN THE PATH, which
// is the only case where the answer has to be tagged on the way back out. A
// board that named its room in a header asked that room directly and wants the
// room's own ids. See `retagCard`.
type taggedKey struct{}

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
	//
	// UNLESS THE HUB REMEMBERS ANOTHER ONE'S WORK. One room attached is not the
	// same as one room existing: a laptop that is shut still has cards on it,
	// the hub knows what they were, and scoping straight to the single live
	// room would drop them from the board entirely. That is the case decision
	// 16 is about, and it is the common one, because rooms are machines and
	// machines get shut.
	if only := p.hub.Only(); only != "" && !p.rememberingOthers(only) {
		return only, false
	}
	return "", false
}

// rememberingOthers reports whether the hub is holding cards for a room other
// than this one.
func (p *Proxy) rememberingOthers(besides string) bool {
	stock := p.inventory()
	if stock == nil {
		return false
	}
	names, err := stock.Holding()
	if err != nil {
		return false
	}
	for _, n := range names {
		if !equalFold(n, besides) {
			return true
		}
	}
	return false
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
	// THE CONTROL MCP SERVER, ahead of the rest of the hub API because it sets
	// its own content type and streams: serveHubAPI marks everything JSON, which
	// is wrong for this. Loopback only, and refused otherwise. See serveControl.
	if strings.HasPrefix(r.URL.Path, "/_hub/mcp") {
		p.serveControl(w, r)
		return
	}
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
	// A ROOM ON ITS WAY OUT STARTS NOTHING NEW.
	//
	// That is the whole difference between marking a room for deletion and
	// deleting one. Everything already running carries on and is worked out
	// normally, the room stays on every list, and nothing is destroyed: the one
	// thing that changes is that no more work lands there.
	//
	// Checked here rather than on the room, because it is the HUB's decision.
	// The room has no idea it has been marked, and telling it would be a second
	// copy of a fact somebody can change while that room is offline.
	if p.startsNothing(w, r) {
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
		// Paged, sorted and cut at the hub, which a row in the merge table
		// cannot express. See `history`.
		if r.URL.Path == "/v1/history" && r.Method == http.MethodGet && len(p.hub.Rooms()) > 0 {
			p.history(w, r)
			return
		}
		if p.aggregate(w, r, r.URL.Path) {
			return
		}
		// A READ THE BOARD CANNOT DRAW WITHOUT goes to the first room rather
		// than being refused.
		//
		// These are machine-shaped and merging them would be a lie: four
		// machines have four editors and four sets of terminal themes. But the
		// board asks for them to DRAW ITSELF, not to tell you about a machine,
		// and refusing left it collecting 409s and rendering panes empty.
		//
		// One room's answer, which is a choice the operator can override by
		// scoping to the room they mean. Writing one is still refused, because
		// that is the question `needsARoom` exists to ask.
		if r.Method == http.MethodGet && borrowed[r.URL.Path] {
			if first := p.firstRoom(); first != "" {
				room = first
			}
		}
		// A write, or a read nothing knows how to merge. Asked rather than
		// guessed at.
		if room == "" {
			if rooms := p.hub.Rooms(); len(rooms) > 1 {
				needsARoom(w, rooms)
				return
			}
		}
	}
	// Carried on the context, which is the only thing that reaches the dial.
	ctx := context.WithValue(r.Context(), roomKey{}, room)
	// And separately, whether the room was named BY A TAG IN THE PATH, which
	// decides whether the answer needs one putting back. See `retagCard`.
	if tagged, _ := splitTag(cardIDIn(r.URL.Path)); tagged != "" {
		ctx = context.WithValue(ctx, taggedKey{}, tagged)
	}
	r = r.WithContext(ctx)
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
// rewrite is everything the hub changes on the way back out.
func (p *Proxy) rewrite(res *http.Response) error {
	if err := p.rewriteHealth(res); err != nil {
		return err
	}
	return p.retagCard(res)
}

// retagCard puts the room back on a single card's id.
//
// THE OTHER HALF OF `untag`, AND WITHOUT IT THE TAG SURVIVES EXACTLY ONE HOP.
//
// A merged list hands the board `athens~01a0`. The board clicks it and asks for
// `/v1/tasks/athens~01a0`, which the hub strips to `/v1/tasks/01a0` because the
// room minted that id and knows nothing about rooms. The room then answers with
// its own payload, in which `id` is `01a0`.
//
// So the board, which had a tagged id, now holds a bare one, and every url it
// builds from THAT is unaddressable: the terminal websocket went to
// `/v1/tasks/01a0/attach`, which names no room, and with two rooms attached the
// hub can only refuse it. The symptom is a terminal that never attaches and a
// board quietly collecting 409s.
//
// Only in aggregate mode, and only for a request that arrived carrying a tag.
// A scoped board asked a room directly and wants the room's own answer.
func (p *Proxy) retagCard(res *http.Response) error {
	if res.Request == nil || res.StatusCode != http.StatusOK {
		return nil
	}
	// FROM THE CONTEXT, NOT THE PATH. `res.Request` is the OUTBOUND request,
	// and `Rewrite` already took the tag off it, so reading the path here finds
	// the bare id every time and this quietly does nothing.
	room, _ := res.Request.Context().Value(taggedKey{}).(string)
	if room == "" {
		return nil
	}
	if !strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
		// A download, a terminal, an icon. Nothing with an id in it, and
		// reading it into memory to find out would be the one place this
		// design promises not to.
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	res.Body.Close()
	if err != nil {
		return err
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		// Not an object we understand. Passed through rather than failed.
		res.Body = io.NopCloser(bytes.NewReader(raw))
		return nil
	}
	// The same fields the list and the event stream tag, for the same reason:
	// all three have to describe the same card.
	obj["room"] = room
	for _, field := range []string{"id", "task_id"} {
		if id, ok := obj[field].(string); ok && id != "" {
			obj[field] = tagFor(room, id)
		}
	}
	out, err := json.Marshal(obj)
	if err != nil {
		res.Body = io.NopCloser(bytes.NewReader(raw))
		return nil
	}
	res.Body = io.NopCloser(bytes.NewReader(out))
	res.ContentLength = int64(len(out))
	res.Header.Set("Content-Length", fmt.Sprint(len(out)))
	return nil
}

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
		// A REQUEST FOR A ROOM THE HUB KNOWS ABOUT SAYS SO BY NAME.
		//
		// This is what every operation on a card from an offline room lands on,
		// and "no room is attached" is wrong there in a way that matters: there
		// are rooms attached, just not that one. The board shows this sentence,
		// so it has to be about the thing that was clicked.
		//
		// NO QUEUEING BEHIND IT. Not "we will apply this when the machine
		// returns": a queue of intentions against a machine nobody has heard
		// from is a second source of truth, and reconciling it is the part that
		// goes wrong. An offline room is a room you cannot act on.
		// OFF THE CONTEXT, NOT OFF THE PATH. By the time this runs the request
		// is the OUTBOUND one: its `room~id` has already been rewritten to the
		// room's own id, so reading the path here finds no room at all. The
		// rewrite puts the name in the context precisely because it is the last
		// place both halves are in scope.
		room, _ := r.Context().Value(roomKey{}).(string)
		if room == "" {
			room, _ = p.roomFor(r)
		}
		if room != "" && !p.hub.Has(room) {
			msg = "the room " + room + " is not answering, so nothing on it can be " +
				"opened or changed. what is shown of it is what it last said. it will " +
				"work again when that machine is back."
		}
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

// ── the inventory ───────────────────────────────────────

// Known is one room as the rooms tab sees it: what the hub has written down
// about it, and whether it is answering right now.
//
// THE TWO HALVES COME FROM DIFFERENT PLACES AND MUST NOT BE CONFUSED. The
// record is durable and is the hub's own truth. `Attached` is a socket in this
// process. A room can be one without the other in both directions: added this
// morning and never dialled in, or dialled in and holding cards while somebody
// marks it for deletion.
type Known struct {
	Name string `json:"name"`
	// SelfName is what the machine calls itself. Observed, shown beside the
	// name, never instead of it.
	SelfName string `json:"self_name,omitempty"`
	// Transport earns a badge and never a column. See docs/hub-room-requirements.
	Transport string `json:"transport"`
	State     string `json:"state"`
	// Attached is the live half, from this hub's own connection list rather
	// than from anything written down.
	Attached bool `json:"attached"`
	// Since is when the current connection was made, and is nothing when there
	// is no current connection.
	Since *time.Time `json:"since,omitempty"`
	// FirstSeen being absent is what "never connected" means, and it is the one
	// state that draws nowhere but this tab.
	FirstSeen *time.Time `json:"first_seen,omitempty"`
	LastSeen  *time.Time `json:"last_seen,omitempty"`
	// ClearedAt is when this room last said, while connected, that it holds
	// nothing. It is what a removal rests on, and the board draws it as the
	// difference between a room waiting to be tidied and one that is ready.
	ClearedAt *time.Time `json:"cleared_at,omitempty"`
	Host      string     `json:"host,omitempty"`
	Version   string     `json:"version,omitempty"`
	// Cards is how many this room was last holding, from the cache. Only worth
	// showing when the room is not attached, and worth saying is a memory.
	Cards int `json:"cards"`
	// Waiting is a join string minted and not yet used.
	Waiting bool `json:"waiting"`
}

// Inventory is a hub that knows which rooms exist, not only which are here.
//
// AN INTERFACE FOR THE SAME REASON `OwnRoom` IS ONE. Knowing which rooms exist
// means a database, and this package holds none and must not learn to. Whoever
// builds the hub implements it, and a hub without one answers the inventory
// with what is attached, which is what a hub with no store can honestly say.
// ── WHAT THE BOARD MAY DO TO A ROOM, AND WHAT IT MAY NOT ─
//
// It may read the list, and it may mark a room for deletion or take that mark
// back off. Marking destroys nothing and is one click to undo, which is the
// whole reason decision 9 made it the reversible step.
//
// IT MAY NOT MINT A JOIN STRING, and that is not an oversight. Atrium has no
// login: it is loopback, and reaching it from elsewhere is an overlay's job.
// A board that could mint one would mean anybody who can open that page can
// enrol a machine that runs agents, and a board served over an overlay is
// exactly the case that matters. So adding a room, replacing its join string
// and forgetting it are things somebody does at a terminal ON the hub, where
// being there is the credential.
//
// That line is where it already was: the join dialog has always said what to
// run rather than minting anything.
type Inventory interface {
	Known() ([]Known, error)
	// MarkRoom puts a room on its way out, or takes the mark back off.
	MarkRoom(name string, marked bool) error
	// Remembered is what a room last said it was holding.
	//
	// ONLY EVER CALLED FOR A ROOM THAT IS NOT ANSWERING. A connected room is
	// asked, every time, and its answer is the answer. This is what the board
	// draws instead of nothing for a machine somebody shut.
	Remembered(name string) ([]CardState, error)
	// Holding names every room that has connected before and has cards
	// remembered for it, whether or not it is here now.
	//
	// ASKED ON EVERY LIST THE BOARD DRAWS, so it has to be one cheap question.
	// It decides whether there is anything to merge at all, which is why it
	// cannot be worked out by reading every room and counting its cards.
	Holding() ([]string, error)
}

// SetInventory wires the durable room list up. Optional.
func (p *Proxy) SetInventory(s Inventory) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stock = s
}

func (p *Proxy) inventory() Inventory {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stock
}

// startsNothing refuses a request that would put new work on a room that is
// marked for deletion.
//
// A SHORT LIST OF PATHS, NOT EVERY WRITE. Marking is not read-only mode: a card
// already on that machine can still be renamed, tagged, answered, shelved and
// finished, because the whole point is that work in flight is worked out
// normally. What stops is arriving.
//
// Missing one of these is not a hole anybody can walk through, it is a card
// started on a machine somebody is decommissioning, which they will see on the
// board and can shelve. So this is a list that can grow without being a
// liability, which is why it is a list rather than a rule about methods.
func (p *Proxy) startsNothing(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	switch r.URL.Path {
	case "/v1/launch", "/v1/tasks", "/v1/intake", "/v1/dispatch":
	default:
		return false
	}
	stock := p.inventory()
	if stock == nil {
		return false
	}
	// WHICHEVER ROOM THIS WOULD LAND ON, however it was chosen. Not only a room
	// named explicitly: a hub with one room attached routes there without
	// anybody saying so, and that is exactly the case where somebody is about
	// to start work on the machine they are decommissioning.
	room, _ := p.roomFor(r)
	if room == "" {
		return false
	}
	known, err := stock.Known()
	if err != nil {
		return false
	}
	for _, k := range known {
		if !equalFold(k.Name, room) || k.State != "marked-for-deletion" {
			continue
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		fmt.Fprintf(w, `{"error":%q}`, "the room "+k.Name+" is marked for deletion, so it "+
			"starts no new work. what is already running on it carries on as normal. "+
			"take the mark off if you want to use it again")
		return true
	}
	return false
}

// serveInventory answers the rooms tab.
//
// UNSORTED HERE. Live before offline before never connected is the board's
// grouping, and it is drawn there rather than baked in, because the same list
// is also the answer to "what exists" and that question has no groups in it.
func (p *Proxy) serveInventory(w http.ResponseWriter, _ *http.Request) {
	stock := p.inventory()
	if stock == nil {
		// A hub with nothing written down says so rather than pretending its
		// connection list is an inventory. The board draws the difference.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"durable": false, "rooms": p.hub.Rooms(),
		})
		return
	}
	rooms, err := stock.Known()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error":%q}`, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"durable": true, "rooms": rooms})
}

// changeInventory marks a room for deletion, or takes the mark back off.
//
// THE REFUSAL IS THE IMPLEMENTATION'S, and this only carries it back. Its whole
// job is to turn an error into a sentence the board can show.
func (p *Proxy) changeInventory(w http.ResponseWriter, r *http.Request) {
	stock := p.inventory()
	if stock == nil {
		w.WriteHeader(http.StatusNotImplemented)
		fmt.Fprintf(w, `{"error":%q}`, "this hub keeps no record of its rooms, "+
			"so there is nothing to mark")
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		fmt.Fprintf(w, `{"error":%q}`, "that has to be a POST")
		return
	}
	var body struct {
		Name   string `json:"name"`
		Marked bool   `json:"marked"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error":%q}`, "could not read that: "+err.Error())
		return
	}
	if err := stock.MarkRoom(body.Name, body.Marked); err != nil {
		// A ROOM THAT IS NOT THERE IS AN ANSWER, not a failure, and it is the
		// most likely thing to go wrong here. 409 rather than 500, so the board
		// shows the sentence instead of an apology.
		w.WriteHeader(http.StatusConflict)
		fmt.Fprintf(w, `{"error":%q}`, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

// SetControl mounts the hub-side control MCP server at /_hub/mcp.
//
// `boardAddr` is this hub's own board listen address, e.g. `:7778`. The control
// tools reach the hub's API over loopback derived from it, so they inherit the
// proxy's scoping and offline-room behaviour rather than reimplementing it.
// Optional: a hub that never calls this answers /_hub/mcp with 404.
func (p *Proxy) SetControl(boardAddr string) {
	p.control = newControlHandler(loopbackBase(boardAddr), p.hub)
}

// serveControl answers the hub-side control MCP server.
//
// LOOPBACK ONLY, for the same reason `/v1/shutdown` is refused through a hub.
// These tools restart daemons and drive other sessions, and the room is not the
// place to guard them: over a link the room sees a connection that terminates in
// its own process, so a loopback check there would pass for anybody who could
// reach the hub. The gate belongs here, on the browser-facing listener, and it
// is the CALLER's own address that has to be loopback. The overlay is not an
// auth layer: once something else can reach the hub, loopback on the far side
// stops meaning "this machine".
func (p *Proxy) serveControl(w http.ResponseWriter, r *http.Request) {
	if p.control == nil {
		http.NotFound(w, r)
		return
	}
	if !loopbackRemote(r.RemoteAddr) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"error":"the control server is reachable only from the machine the `+
			`hub runs on. it is not exposed over an overlay."}`)
		return
	}
	p.control.ServeHTTP(w, r)
}

// serveHubAPI answers the few things only the hub knows.
func (p *Proxy) serveHubAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch strings.TrimPrefix(r.URL.Path, "/_hub/") {
	case "rooms":
		// ATTACHED ONLY, and every other pane on the board depends on that.
		// The room picker, the grouping, the counter and the question about
		// where a write lands all mean "which rooms can answer right now", and
		// a list that included a laptop somebody shut last week would put it in
		// the picker, in the groups, and in the count of things wanting
		// attention. The inventory is a different question and has its own
		// endpoint.
		_ = json.NewEncoder(w).Encode(map[string]any{"rooms": p.hub.Rooms()})
	case "inventory":
		p.serveInventory(w, r)
	case "inventory/mark":
		p.changeInventory(w, r)
	case "health":
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "rooms": len(p.hub.Rooms()),
			"only": p.hub.Only(), "build": p.boardID,
		})
	default:
		http.NotFound(w, r)
	}
}

