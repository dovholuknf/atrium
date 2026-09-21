package link

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// The event stream, fanned in once and dealt back out per client.
//
// ── why the hub holds the stream and not the browser ─────
//
// A browser could open one EventSource per room and merge them itself. It
// cannot, for two reasons that are not going away. An EventSource sets no
// headers, so it could not name its room, and the number of streams would be
// the number of rooms times the number of open tabs, which is how a laptop with
// four rooms and six tabs ends up holding twenty-four long-lived connections
// across four machines.
//
// So the hub holds exactly ONE upstream stream per room, for as long as anybody
// is watching, and deals events out to whoever is connected. Twenty-four
// browser streams become four.
//
// ── the three addresses ──────────────────────────────────
//
//	/v1/events/hub          every room, tagged, one merged stream
//	/v1/events/room/<name>  one room, untouched, exactly what that room sent
//	/v1/events              whichever of the two the request is asking for
//
// PATHS, NOT A HEADER, and that is the reason this shape was chosen over the
// one it replaces. `EventSource` cannot set `X-Atrium-Room`, so a stream can
// only say which room it wants in its URL.
//
// ── what the stream carries, and what it does not ────────
//
// Deltas. Never state. The board fetches `/v1/tasks` when it opens a stream and
// again whenever the stream reconnects, and the stream only ever says what
// changed since. That is why a dropped hub costs one re-fetch per tab and
// nothing else, and it is the reason the board has always looked self-healing.
//
// Nothing is replayed and nothing is buffered for a client that is not there.
// A missed event is not a problem because the re-fetch on reconnect is the
// recovery, and it is a better recovery than a replay buffer because it also
// repairs anything missed while the hub itself was down.

// Event is one thing a room said.
type Event struct {
	Room string
	Kind string
	Data []byte
}

// taggedFields is which payload fields carry a card id, per event kind.
//
// A TABLE, MATCHING `merged` IN fanout.go, and for the same reason: the two
// have to agree. A list that hands the board `sg4~01a0` and a stream that then
// says `01a0` changed describes a card the board has never heard of, and the
// row goes stale until the next reload.
var taggedFields = map[string][]string{
	"task":         {"id"},
	"task-removed": {"id"},
	// A permission is its own row AND points at a card, and the second
	// spelling `task` is the cancel message's field. Both are ids.
	"permission": {"id", "task_id", "task"},
	"activity":   {"task_id"},
}

// feeds is one upstream stream per room, and the clients watching them.
type feeds struct {
	p *Proxy

	mu    sync.Mutex
	subs  map[*sub]struct{}
	pumps map[string]context.CancelFunc
	stop  context.CancelFunc

	// wake shortcuts the reconciler's tick when a client arrives, so a board
	// scoped to a room nobody was watching does not wait out a second of
	// silence before its stream starts.
	wake chan struct{}
}

// sub is one connected board.
type sub struct {
	// room is which room this client wants, empty meaning all of them.
	room string
	ch   chan Event
	// closed guards against a double close when a slow client is dropped at
	// the same moment it disconnects.
	once sync.Once
}

func (s *sub) shut() { s.once.Do(func() { close(s.ch) }) }

func newFeeds(p *Proxy) *feeds {
	return &feeds{
		p: p, subs: map[*sub]struct{}{}, pumps: map[string]context.CancelFunc{},
		wake: make(chan struct{}, 1),
	}
}

// add registers a client and makes sure the upstream streams are running.
func (f *feeds) add(room string) *sub {
	// Deep enough for a burst of activity events, which arrive one per tool
	// call across every session on a machine.
	s := &sub{room: room, ch: make(chan Event, 128)}
	f.mu.Lock()
	f.subs[s] = struct{}{}
	// NOBODY WATCHING MEANS NOTHING STREAMING. The upstreams exist to serve
	// connected boards, and holding four cross-machine streams open for an
	// empty browser is a cost with no reader.
	//
	// STARTED UNDER THE LOCK, so a client that connects and leaves immediately
	// cannot have its `drop` run before `stop` was recorded and leave the
	// reconciler running with nobody to run for.
	var started context.Context
	if len(f.subs) == 1 {
		ctx, cancel := context.WithCancel(context.Background())
		f.stop, started = cancel, ctx
	}
	f.mu.Unlock()
	if started != nil {
		go f.reconcile(started)
	} else {
		select {
		case f.wake <- struct{}{}:
		default:
		}
	}
	return s
}

// drop unregisters a client, and stops the upstreams when it was the last.
func (f *feeds) drop(s *sub) {
	f.mu.Lock()
	delete(f.subs, s)
	last := len(f.subs) == 0
	stop := f.stop
	f.mu.Unlock()
	s.shut()
	if last && stop != nil {
		stop()
	}
}

// reconcile keeps one pump running per attached room.
//
// A TICKER RATHER THAN A CALLBACK FROM THE HUB. Rooms attach and detach on
// their own schedule and the hub has no listener list, so this asks. A second
// of lag before a newly attached room's events appear is invisible next to the
// board's own re-fetch.
func (f *feeds) reconcile(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	defer f.closeAll()
	last := ""
	for {
		rooms := f.p.hub.Rooms()
		names := make([]string, 0, len(rooms))
		for _, r := range rooms {
			names = append(names, r.Name)
		}
		sort.Strings(names)

		// ONLY THE ROOMS SOMEBODY IS WATCHING. A board scoped to one room
		// wants one stream, and pumping the other three so their events can be
		// filtered out on arrival is the cost this whole file exists to avoid.
		want := map[string]bool{}
		for _, name := range names {
			if f.wanted(name) {
				want[name] = true
			}
		}

		f.mu.Lock()
		for name := range f.pumps {
			if !want[name] {
				f.pumps[name]()
				delete(f.pumps, name)
			}
		}
		for name := range want {
			if _, ok := f.pumps[name]; ok {
				continue
			}
			pctx, cancel := context.WithCancel(ctx)
			f.pumps[name] = cancel
			go f.pump(pctx, name)
		}
		f.mu.Unlock()

		// MEMBERSHIP IS ITSELF AN EVENT. The board draws a room counter and it
		// has to change when a room comes or goes, without a poll.
		if now := strings.Join(names, ","); now != last {
			last = now
			payload, _ := json.Marshal(map[string]any{
				"rooms": names, "only": f.p.hub.Only(),
			})
			f.emit(Event{Kind: "rooms", Data: payload})
		}

		select {
		case <-ctx.Done():
			return
		case <-f.wake:
		case <-t.C:
		}
	}
}

// wanted reports whether anybody is listening for a room. One subscriber
// asking for everything wants all of them.
func (f *feeds) wanted(room string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for s := range f.subs {
		if s.room == "" || s.room == room {
			return true
		}
	}
	return false
}

// count is how many upstream streams are running. For tests, which is the only
// place the number is checkable rather than merely observable.
func (f *feeds) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.pumps)
}

func (f *feeds) closeAll() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for name, cancel := range f.pumps {
		cancel()
		delete(f.pumps, name)
	}
}

// pump holds one upstream stream to one room, forever, reconnecting.
func (f *feeds) pump(ctx context.Context, room string) {
	wait := 250 * time.Millisecond
	for ctx.Err() == nil {
		began := time.Now()
		f.read(ctx, room)
		if ctx.Err() != nil {
			return
		}
		// A LINK THAT STAYED UP IS NOT A FAILING LINK. Without this, one
		// outage early in a long session makes every later reconnect cost the
		// full backoff for the rest of the day.
		if time.Since(began) > 30*time.Second {
			wait = 250 * time.Millisecond
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if wait *= 2; wait > 10*time.Second {
			wait = 10 * time.Second
		}
	}
}

// read is one connection's worth of stream, returning when it breaks.
func (f *feeds) read(ctx context.Context, room string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://"+hostFor(room)+"/v1/events", nil)
	if err != nil {
		return
	}
	req.Header.Set("Accept", "text/event-stream")
	res, err := f.p.roomClient(room).Do(req)
	if err != nil {
		return
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return
	}
	// Bounded per line, so a room cannot make the hub allocate without limit.
	// An event payload is a card or a permission, never megabytes.
	br := bufio.NewReaderSize(res.Body, 64<<10)
	kind, data := "message", []byte(nil)
	for {
		line, err := readLine(br, 1<<20)
		if err != nil {
			return
		}
		switch {
		case len(line) == 0:
			// A blank line ends an event. A comment-only keepalive produces
			// none, which is what makes `: ping` free.
			if data != nil {
				f.emit(Event{Room: room, Kind: kind, Data: data})
				// AND, FOR A FEW KINDS, AN AUDIT LINE. A room winding down, a
				// permission raised or answered, a session starting, finishing or
				// exiting: these are operational, so the hub records them beside
				// its own events. The rest of the relay is per-card history with
				// its own home, and the audit log is not a second copy of it. See
				// audit.go.
				f.p.auditFromRelay(room, kind, data)
			}
			kind, data = "message", nil
		case line[0] == ':':
			// A keepalive. Read and discarded, because its only job was to
			// prove the connection is alive, and it just did.
		case bytes.HasPrefix(line, []byte("event:")):
			kind = string(bytes.TrimSpace(line[len("event:"):]))
		case bytes.HasPrefix(line, []byte("data:")):
			// One optional leading space after the colon belongs to the
			// framing, not to the payload. Everything after it does.
			chunk := line[len("data:"):]
			if len(chunk) > 0 && chunk[0] == ' ' {
				chunk = chunk[1:]
			}
			if data == nil {
				data = append([]byte{}, chunk...)
			} else {
				data = append(append(data, '\n'), chunk...)
			}
		}
	}
}

// readLine reads one line and refuses an endless one.
//
// `bufio.Reader.ReadString` will grow until the process dies, and the thing on
// the other end is another machine. A room that never sends a newline must cost
// this hub one dropped stream, not its memory.
func readLine(br *bufio.Reader, max int) ([]byte, error) {
	var out []byte
	for {
		chunk, more, err := br.ReadLine()
		if err != nil {
			return nil, err
		}
		out = append(out, chunk...)
		if len(out) > max {
			return nil, fmt.Errorf("a room sent a line over %d bytes", max)
		}
		if !more {
			return out, nil
		}
	}
}

// emit hands an event to everybody who asked for that room.
func (f *feeds) emit(e Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for s := range f.subs {
		if s.room != "" && s.room != e.Room {
			continue
		}
		// A ROOM'S `going-down` IS THAT ROOM'S NEWS, NOT THE BOARD'S. One room
		// shutting down says nothing about the hub, but the merged board's
		// handler reads any `going-down` as the hub restarting and paints a
		// banner into every terminal that never clears, because nothing
		// reconnects to clear it. The merged view already learns the room left
		// from the `rooms` event, which marks its cards offline, so drop a
		// room-originated `going-down` on the merged fan-out and keep it only
		// for a board scoped to that exact room.
		if e.Kind == "going-down" && e.Room != "" && s.room == "" {
			continue
		}
		select {
		case s.ch <- e:
		default:
			// A CLIENT TOO SLOW TO KEEP UP IS DROPPED, NOT STARVED, which is
			// what the daemon's own bus does and for the same reason. Closing
			// the stream makes the browser reconnect, and a reconnect
			// re-fetches state, so a dropped client comes back correct. Skipping
			// the event instead would leave it subscribed and permanently wrong.
			delete(f.subs, s)
			s.shut()
		}
	}
}

// ── what a client sees ───────────────────────────────────

// serveEvents writes one stream to one board.
//
// `room` empty means every room. `tag` asks for the merged view's identities,
// and it is a request rather than a decision: the answer is checked again for
// every event, because a room attaching changes it.
func (p *Proxy) serveEvents(w http.ResponseWriter, r *http.Request, room string, tag bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Nothing in the chain between here and the browser may buffer this, and
	// the one that would is an nginx somebody put in front of the hub.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	s := p.feeds.add(room)
	defer p.feeds.drop(s)

	// The same 25 seconds the daemon uses, and it has to stay under the
	// transport's 90 second idle timeout or a quiet board would be cut off
	// between events.
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case e, ok := <-s.ch:
			if !ok {
				return
			}
			data := e.Data
			// DECIDED PER EVENT, NOT PER CONNECTION, because membership
			// changes under an open stream. A board that connected when one
			// room was attached would otherwise keep receiving bare ids after
			// a second room joined, while `/v1/tasks` had started answering
			// tagged ones, and every card it heard about would be a card it
			// had never listed.
			if tag && p.hub.Only() == "" {
				data = tagEvent(e)
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Kind, data); err != nil {
				return
			}
			flusher.Flush()
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// tagEvent puts the room on an event and rewrites the ids inside it.
//
// The mirror of what `aggregate` does to a list row, and it has to stay the
// mirror. See `taggedFields`.
func tagEvent(e Event) []byte {
	if e.Room == "" || len(e.Data) == 0 {
		return e.Data
	}
	var obj map[string]any
	if err := json.Unmarshal(e.Data, &obj); err != nil {
		// Not an object. Passed through unchanged rather than dropped: an
		// event the hub does not understand is still an event the board might.
		return e.Data
	}
	obj["room"] = e.Room
	for _, field := range taggedFields[e.Kind] {
		if id, ok := obj[field].(string); ok && id != "" {
			obj[field] = tagFor(e.Room, id)
		}
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return e.Data
	}
	return out
}

// eventsFor routes the three stream addresses. Reports false when the path is
// not a stream at all.
func (p *Proxy) eventsFor(w http.ResponseWriter, r *http.Request) bool {
	switch {
	case r.URL.Path == "/v1/events/hub":
		// TAGGED EXACTLY WHEN THE LISTS ARE TAGGED, which is the one rule that
		// keeps the stream and `/v1/tasks` describing the same cards. One room
		// attached means the lists come through the pipe untagged, so the
		// stream must be untagged too. Asked again for every event, since
		// which it is can change while the stream is open.
		p.serveEvents(w, r, "", true)
		return true
	case strings.HasPrefix(r.URL.Path, "/v1/events/room/"):
		room := strings.TrimPrefix(r.URL.Path, "/v1/events/room/")
		if room == "" || strings.Contains(room, "/") {
			http.NotFound(w, r)
			return true
		}
		if !p.hub.Has(room) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, `{"error":"no room named %q is attached"}`, room)
			return true
		}
		// UNTAGGED, because a board scoped to one room asked that room's
		// questions and got that room's ids back.
		p.serveEvents(w, r, room, false)
		return true
	case r.URL.Path == "/v1/events":
		// The address a board that has never heard of rooms asks for. Answered
		// as whichever of the two above it meant.
		if room, _ := p.roomFor(r); room != "" {
			p.serveEvents(w, r, room, false)
			return true
		}
		p.serveEvents(w, r, "", true)
		return true
	}
	return false
}
