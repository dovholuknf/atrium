package link

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// The cache: a room telling its hub what it is holding, so a hub whose room is
// offline can show what was there rather than nothing.
//
// ── pushed, not polled ──────────────────────────────────
//
// The room is the only thing that knows something changed. A timer was the
// first instinct and it is the wrong trigger: most of the time nothing is
// happening, so a fixed tick means a change waits for no reason, and the moment
// things get busy it is the wrong number anyway.
//
// So this watches the room's OWN event stream, through the same handler it
// serves to the hub, and sends when something happens. A ceiling of a second or
// two stops a busy room thrashing the hub's database, and an announcement
// identical to the last one is dropped rather than sent, which is what keeps a
// stream of activity events from producing a stream of writes. Activity is the
// noisiest thing on that stream and is never cached, so most of what wakes this
// up has nothing to say.
//
// ── its own connection, not the control one ─────────────
//
// A card list is not small and the control connection is framed at 64KB a line
// for good reasons. So an announcement is a connection the room dials, like
// the upgrade fetch is: one hello that says what it is, then a JSON body, then
// an acknowledgement. The hub never asks for it, which is the same direction
// everything else in this design runs.
//
// ── and the hub takes it whole ──────────────────────────
//
// What arrives IS the state. Anything the hub was holding that is not in it is
// discarded, because it is no longer there. See `Announce` in internal/hubstore
// for why that is the right rule and what is written down to make it
// answerable afterwards.

// announceKind is the connection kind. Its own, so the hub can tell it from a
// data connection before reading a byte of the body.
const announceKind = "announce"

// announceMax bounds an announcement. Large enough for a room holding a few
// thousand cards, bounded because it arrives over a socket.
const announceMax = 8 << 20

// announceEvery is the ceiling on how often one room announces. Quiet periods
// are unaffected: the first change after silence goes immediately.
const announceEvery = 2 * time.Second

// CardState is one cached card on the wire.
//
// The payload is opaque here on purpose. This package has no business knowing
// what a card is made of, and a field list would be a second copy of the room's
// schema that drifts from it.
type CardState struct {
	ID string `json:"id"`
	// Status is lifted out because the board groups on it. Everything else
	// stays inside the payload.
	Status  string          `json:"status"`
	Payload json.RawMessage `json:"payload"`
}

// announcement is the body of one of those connections.
type announcement struct {
	Room  string      `json:"room"`
	Cards []CardState `json:"cards"`
}

// ── the room's side ─────────────────────────────────────

// announcer watches one room and tells the hub when it changes.
type announcer struct {
	r       *Room
	session string

	mu   sync.Mutex
	last string // hash of what was sent, so nothing identical is sent twice
	// owed is an announcement that was meant to land and did not. Cleared by
	// the next one that does.
	owed bool
}

// announce runs for the life of one attachment.
func (r *Room) announce(ctx context.Context, session string) {
	a := &announcer{r: r, session: session}

	// THE FIRST ONE IS UNCONDITIONAL, and it is the important one. A room that
	// has just attached is a room the hub may have been holding a stale picture
	// of for hours, and decision 13 is that coming back replaces that picture
	// whole.
	a.send(ctx)

	// A NUDGE CHANNEL RATHER THAN A SEND PER EVENT. One card moving produces
	// several events, and a working agent produces them constantly.
	nudge := make(chan struct{}, 1)
	go a.watch(ctx, nudge)

	for {
		// A FAILED ANNOUNCEMENT IS TRIED AGAIN WITHOUT WAITING FOR SOMETHING
		// ELSE TO HAPPEN.
		//
		// The one that matters is the first, which is what replaces a picture
		// the hub may have been holding for hours. A room that announces on
		// attaching, fails, and then sits quietly because nobody is working on
		// it would leave the hub showing yesterday's cards for as long as that
		// room stayed quiet, which is exactly the room whose board somebody is
		// looking at because nothing is happening on it.
		//
		// The retry is the same send: the state is read again, so what
		// eventually lands is what is there then, not what was there when the
		// first attempt failed.
		var retry <-chan time.Time
		if a.pending() {
			retry = time.After(announceEvery)
		}
		select {
		case <-ctx.Done():
			return
		case <-retry:
			a.send(ctx)
		case <-nudge:
			a.send(ctx)
			// The ceiling. Anything that happens during it collapses into the
			// one nudge the channel can hold, so a busy room announces on a
			// rhythm and a quiet one announces the moment it changes.
			select {
			case <-ctx.Done():
				return
			case <-time.After(announceEvery):
			}
		}
	}
}

// pending reports whether the last attempt did not land.
func (a *announcer) pending() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.owed
}

// watch turns the room's own event stream into nudges.
//
// IN PROCESS, through the handler this room already serves. Not a loopback
// request: the room's own board address is a flag somebody can set to `-`, and
// a feature that stops working because of that would be a bad afternoon for
// whoever finds it.
func (a *announcer) watch(ctx context.Context, nudge chan<- struct{}) {
	for ctx.Err() == nil {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/v1/events", nil)
		if err != nil {
			return
		}
		req.Header.Set("Accept", "text/event-stream")
		a.r.Handler.ServeHTTP(&nudgeWriter{ctx: ctx, nudge: nudge}, req)
		if ctx.Err() != nil {
			return
		}
		// The stream ended, which on a running room means the daemon closed it.
		// Waiting before asking again, so a handler that refuses immediately
		// cannot become a spin.
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

// nudgeWriter is an http.ResponseWriter that turns written bytes into a nudge.
//
// It never parses the stream. WHAT changed does not matter here: the answer is
// always to ask the room for everything, because taking the state whole is the
// rule and a diff would be a second way to be wrong.
type nudgeWriter struct {
	ctx    context.Context
	nudge  chan<- struct{}
	header http.Header
}

func (n *nudgeWriter) Header() http.Header {
	if n.header == nil {
		n.header = http.Header{}
	}
	return n.header
}

func (n *nudgeWriter) WriteHeader(int) {}

func (n *nudgeWriter) Write(p []byte) (int, error) {
	if err := n.ctx.Err(); err != nil {
		// What stops the handler. An event stream runs until its writer says
		// no, and this is the only thing that will ever say it.
		return 0, err
	}
	select {
	case n.nudge <- struct{}{}:
	default:
		// One already waiting. Dropping this is the coalescing.
	}
	return len(p), nil
}

// Flush is here because the event stream calls it. Without it the handler
// writes into a buffer nobody drains and the first nudge never arrives.
func (n *nudgeWriter) Flush() {}

// send asks the room what it holds and tells the hub, unless nothing changed.
func (a *announcer) send(ctx context.Context) {
	cards, sum, err := a.read(ctx)
	if err != nil {
		// OWED, so this is tried again rather than waiting for something else
		// to happen. A room that cannot read its own state has a problem worth
		// solving, and in the meantime the hub keeps what it already had.
		a.mu.Lock()
		a.owed = true
		a.mu.Unlock()
		log.Printf("[link] could not read this room's own state: %v", err)
		return
	}
	a.mu.Lock()
	same := sum == a.last && !a.owed
	a.mu.Unlock()
	if same {
		// THE REASON A NOISY STREAM IS NOT A NOISY DATABASE. Activity, output
		// and telemetry all publish events and none of them are in what is
		// cached, so most of what wakes this up produces an identical answer.
		return
	}

	if err := a.post(ctx, cards); err != nil {
		// Best effort in the sense that it never fails a room: the agents here
		// carry on and the room's own board is untouched. It is not best effort
		// in the sense of being forgotten, because the hub is now holding
		// something older than this room, and nothing else is going to correct
		// that for a room nobody is working on.
		a.mu.Lock()
		a.owed = true
		a.mu.Unlock()
		log.Printf("[link] could not tell the hub what is here: %v", err)
		return
	}
	a.mu.Lock()
	a.last, a.owed = sum, false
	a.mu.Unlock()
}

// read asks this room's own handler what it is holding.
//
// ── A FAILURE HERE IS NOT AN EMPTY ROOM ─────────────────
//
// This is the most dangerous line in the file and it is one `if`. The hub takes
// an announcement whole: everything it is holding that is not in the message is
// discarded. So a room whose own store cannot be read, and which answers with an
// error object rather than a card list, would announce nothing and the hub would
// faithfully record that the room has nothing on it.
//
// A room in trouble must not be able to tell the hub its work is gone. Both
// checks below exist for that one sentence: the status has to say the answer is
// an answer, and the `cards` key has to be PRESENT, because a room holding
// nothing sends an empty list and a room that failed sends no list at all.
// Telling those apart is the whole difference between "the board is clear" and
// "the board is unreadable".
func (a *announcer) read(ctx context.Context) ([]CardState, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/v1/state", nil)
	if err != nil {
		return nil, "", err
	}
	rec := &collector{status: http.StatusOK}
	a.r.Handler.ServeHTTP(rec, req)
	if rec.status < 200 || rec.status > 299 {
		return nil, "", fmt.Errorf("this room answered %d when asked what it holds: %s",
			rec.status, firstLine(rec.body))
	}

	var body struct {
		// A POINTER, so "no cards field" and "an empty cards field" are
		// different answers. They mean opposite things here.
		Cards *[]json.RawMessage `json:"cards"`
	}
	if err := json.Unmarshal(rec.body, &body); err != nil {
		return nil, "", err
	}
	if body.Cards == nil {
		return nil, "", errors.New(
			"this room answered without a card list at all, which is not the same as " +
				"answering that it has no cards. nothing will be announced from it")
	}
	cards := *body.Cards
	out := make([]CardState, 0, len(cards))
	for _, raw := range cards {
		// Only the two fields anything outside the payload needs. The rest
		// stays exactly as the room wrote it.
		var head struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		}
		if err := json.Unmarshal(raw, &head); err != nil || head.ID == "" {
			continue
		}
		out = append(out, CardState{ID: head.ID, Status: head.Status, Payload: raw})
	}
	sum := sha256.Sum256(rec.body)
	return out, hex.EncodeToString(sum[:]), nil
}

// post dials the hub and hands over one announcement.
func (a *announcer) post(ctx context.Context, cards []CardState) error {
	dialCtx, cancel := context.WithTimeout(ctx, a.r.T.DialWait)
	conn, err := a.r.Dial.Dial(dialCtx)
	cancel()
	if err != nil {
		return err
	}
	defer conn.Close()

	br := bufio.NewReader(conn)
	if _, err := sayHello(conn, br, hello{
		Kind: announceKind, Room: a.r.Name, Session: a.session,
	}); err != nil {
		return err
	}
	// A deadline for this exchange only. Unlike a data connection, nothing here
	// is meant to stay open.
	if err := conn.SetDeadline(time.Now().Add(handshakeWait)); err != nil {
		return err
	}
	if err := json.NewEncoder(conn).Encode(announcement{Room: a.r.Name, Cards: cards}); err != nil {
		return err
	}
	// THE SECOND FRAME, which is the hub saying it wrote this down. Waited for
	// rather than fired and forgotten, because a room that carries on as though
	// an announcement landed would then skip the next identical one and leave
	// the hub holding something older than it thinks.
	var ack welcome
	if err := readJSON(br, &ack); err != nil {
		return err
	}
	if !ack.OK {
		return errAnnounce(ack.Error)
	}
	return nil
}

type errAnnounce string

func (e errAnnounce) Error() string { return "the hub refused the announcement: " + string(e) }

// firstLine is enough of a failed answer to recognise it in a log, and not so
// much that a room's whole error page ends up in one.
func firstLine(b []byte) string {
	s := strings.TrimSpace(string(b))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
}

// collector is the smallest ResponseWriter that can hold one JSON answer.
//
// Rather than `httptest.NewRecorder`, which is a testing package and carries a
// great deal this does not need. Bounded, because it is reading an answer whose
// size depends on how much work is on this machine.
type collector struct {
	header http.Header
	body   []byte
	// status defaults to 200 the way an http.ResponseWriter does: a handler
	// that writes a body without calling WriteHeader has answered OK.
	status int
}

func (c *collector) Header() http.Header {
	if c.header == nil {
		c.header = http.Header{}
	}
	return c.header
}

func (c *collector) WriteHeader(code int) { c.status = code }

func (c *collector) Write(p []byte) (int, error) {
	if len(c.body)+len(p) > announceMax {
		return 0, io.ErrShortWrite
	}
	c.body = append(c.body, p...)
	return len(p), nil
}

// ── the hub's side ──────────────────────────────────────

// serveAnnouncement reads one and hands it to whoever keeps the cache.
func (h *Hub) serveAnnouncement(name string, conn net.Conn, br *bufio.Reader) {
	if h.Cached == nil {
		_ = writeJSON(conn, welcome{OK: false,
			Error: "this hub keeps no cache, so there is nothing to tell it"})
		return
	}
	// THE WELCOME COMES FIRST, BEFORE THE BODY, and getting that order wrong is
	// a deadlock rather than an error: the room's handshake waits for a welcome
	// and the hub waits for a body, and both sit there until a deadline fires.
	// Two frames on purpose, and this is the first of them.
	if err := writeJSON(conn, welcome{OK: true}); err != nil {
		return
	}
	if err := conn.SetDeadline(time.Now().Add(handshakeWait)); err != nil {
		return
	}
	var body announcement
	// NOT `readJSON`, because that is bounded at one frame and this is not one.
	// Bounded all the same, at a size a room holding thousands of cards fits
	// inside and a socket cannot exceed.
	if err := json.NewDecoder(io.LimitReader(br, announceMax)).Decode(&body); err != nil {
		log.Printf("[hub] could not read %q's announcement: %v", name, err)
		_ = writeJSON(conn, welcome{OK: false, Error: "that announcement could not be read"})
		return
	}
	// THE NAME COMES FROM THE CONNECTION, not from the body. The body says one
	// too and it is ignored: a room that authenticated as sparta cannot
	// announce athens' cards by filling in a field.
	if err := h.Cached(name, body.Cards); err != nil {
		log.Printf("[hub] could not write down what %q said: %v", name, err)
		_ = writeJSON(conn, welcome{OK: false, Error: err.Error()})
		return
	}
	_ = writeJSON(conn, welcome{OK: true})
}
