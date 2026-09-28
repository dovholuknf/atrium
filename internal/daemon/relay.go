package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// A message to a card on another room. See docs/cross-room-say-design.md.
//
// ── by way of the sender's room, always ─────────────────
//
// Whichever door a cross-room say comes in by (this room's own `/v1/say`, the
// agent listener's `/tell`, or the hub forwarding its own `atrium_say`), it is
// this room that relays it. This room owns the sender's card, so this room is
// where the work ledger and "a worker reported to its launcher" are written.
// The hub carries the message and forgets it.
//
// ── and held here when nobody answers ───────────────────
//
// The hub holds nothing, so a message the hub or the target room cannot take
// right now is held on THIS room, in `relay_outbox`, and sent when they answer:
// on the reaper tick, right after it is held, and when the link reattaches.
// Only a failure KNOWN to come before delivery is held. One that may have
// delivered is unconfirmed, and a say is never sent twice.

// RelayKeep is how long an owed message is kept before it is given up on.
var RelayKeep = 24 * time.Hour

// relayWait bounds one relay from this side. The hub's own bound is shorter.
const relayWait = 45 * time.Second

// Relay is this room's way to the hub. The link implements it, adapted in
// internal/cli, because this package does not import internal/link.
type Relay interface {
	Say(ctx context.Context, s RelaySay) (RelayResult, error)
	Peers(ctx context.Context, all bool) ([]RemotePeer, string, error)
}

// RelaySay is one message for another room. From is the sender's handle here,
// with no room: the hub adds this room's name from its certificate.
type RelaySay struct {
	From, Room, To, Text, When string
}

// RelayResult is what the hub said. OK false is a refusal, with Code and
// Error. Unreachable and Unconfirmed say which kind of failure it was.
type RelayResult struct {
	OK          bool
	Code        int
	Error       string
	Unreachable bool
	Unconfirmed bool
	Delivered   string
	When        string
	Warning     string
	To          string
	Card        string
}

// RemotePeer is a session on another room, handle `name@room`.
type RemotePeer struct {
	Handle  string `json:"handle"`
	Alias   string `json:"alias,omitempty"`
	Card    string `json:"card"`
	Room    string `json:"room"`
	Title   string `json:"title,omitempty"`
	Status  string `json:"status"`
	Doing   string `json:"doing,omitempty"`
	Where   string `json:"where,omitempty"`
	Waiting int    `json:"waiting_seconds,omitempty"`
	Owned   bool   `json:"atrium_owns_terminal"`
}

// The three failures a Relay reports, which decide what is held.
var (
	// ErrRelayOld is a hub older than cross-room say. Holding for it is
	// pointless, since nothing would ever drain.
	ErrRelayOld = errors.New("the hub is older than cross-room say, so it cannot carry this. update the hub")
	// ErrRelayDown is the hub not answering, found out before anything was sent.
	ErrRelayDown = errors.New("the hub is not answering")
	// ErrRelayUnconfirmed is the request sent and no answer read.
	ErrRelayUnconfirmed = errors.New("the hub took the message and did not say what became of it")
)

// relayState is the relay and its drain.
type relayState struct {
	mu    sync.Mutex
	relay Relay
	// draining is one drain at a time, and again is a kick that arrived
	// during one, so it runs once more rather than being lost.
	draining sync.Mutex
	again    atomic.Bool
}

// SetRelay wires this room to its hub. Called once the link is built.
func (d *Daemon) SetRelay(r Relay) {
	d.relays.mu.Lock()
	d.relays.relay = r
	d.relays.mu.Unlock()
}

func (d *Daemon) relay() Relay {
	d.relays.mu.Lock()
	defer d.relays.mu.Unlock()
	return d.relays.relay
}

// RelayAttached is the link saying it is back. What is owed goes now.
func (d *Daemon) RelayAttached() { go d.drainRelays() }

// ── the doors ───────────────────────────────────────────────────────────────

// sayIn is the body every door takes.
type sayIn struct {
	From string `json:"from"`
	To   string `json:"to"`
	Text string `json:"text"`
	When string `json:"when"`
}

// handleSay is `POST /v1/say`: a message to a card on this room or another,
// by address. See address.go for the grammar.
//
// A LOCAL TARGET TAKES EXACTLY TODAY'S PATH, the one `/v1/tasks/<id>/message`
// runs, so a bare name through here is typed and queued and recorded the same
// way. A sender with no name is the operator's channel, as it is there, and is
// refused for another room.
func (d *Daemon) handleSay(w http.ResponseWriter, r *http.Request) {
	var in sayIn
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err)
		return
	}
	name, room, err := SplitAddress(in.To)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err)
		return
	}
	if other := d.otherRoom(room); other != "" {
		code, body := d.sayAcross(r.Context(), strings.TrimSpace(in.From), name, other, in.Text, in.When)
		writeJSONCode(w, code, body)
		return
	}
	target := d.localTarget(name)
	if target == nil {
		list, _ := d.peers(d.st.Qualify(strings.TrimSpace(in.From)))
		writeJSONCode(w, http.StatusNotFound, map[string]any{"error": "no session called " + name, "peers": list})
		return
	}
	// THE SAME HANDLER, over the same body, so there is one way a local message
	// is delivered. Its answer gains who it went to.
	raw, _ := json.Marshal(map[string]string{"text": in.Text, "from": strings.TrimSpace(in.From), "when": in.When})
	inner, err := http.NewRequestWithContext(r.Context(), http.MethodPost, "/v1/tasks/"+target.ID+"/message",
		bytes.NewReader(raw))
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err)
		return
	}
	inner.SetPathValue("id", target.ID)
	rec := &answerRecorder{code: http.StatusOK}
	d.handleMessage(rec, inner)
	var body map[string]any
	if err := json.Unmarshal(rec.body.Bytes(), &body); err != nil || body == nil {
		body = map[string]any{"error": strings.TrimSpace(rec.body.String())}
	}
	if rec.code < 400 {
		body["to"], body["card"] = target.WireName, target.ID
	}
	writeJSONCode(w, rec.code, body)
}

// localTarget resolves a name on this room: handle, then alias, then card id.
func (d *Daemon) localTarget(name string) *store.Task {
	if t, err := d.st.GetByWireName(d.st.Qualify(name)); err == nil {
		return t
	}
	if t, err := d.st.GetByAlias(name); err == nil {
		return t
	}
	if t, err := d.st.Get(name); err == nil {
		return t
	}
	return nil
}

// sayAcross relays one message to `name` on `room`, and answers the sender.
func (d *Daemon) sayAcross(ctx context.Context, from, name, room, text, when string) (int, map[string]any) {
	text = strings.TrimSpace(text)
	to := name + "@" + room
	switch {
	case from == "":
		return http.StatusBadRequest, errBody("a message to another room needs the session sending it, or it " +
			"would be typed as the operator. say which session is sending")
	case text == "":
		return http.StatusBadRequest, errBody("there is nothing to say")
	case len(text) > maxPeerMessage:
		return http.StatusRequestEntityTooLarge, errBody(fmt.Sprintf(
			"that is %d characters, over the %d limit. write it to a file and say where it is",
			len(text), maxPeerMessage))
	}
	if _, err := parseWhen(when); err != nil {
		return http.StatusBadRequest, errBody(err.Error())
	}
	// The sender's own card, when it is one here. Its handle as this room
	// knows it is what the other room is told.
	sender, _ := d.st.GetByWireName(d.st.Qualify(from))
	wire := from
	if sender != nil {
		wire = sender.WireName
	}
	if !d.peerLimit.allow(wire) {
		return http.StatusTooManyRequests, errBody(fmt.Sprintf(
			"%s has sent %d messages in the last minute, which is the limit", wire, peerSendsPerMinute))
	}
	rl := d.relay()
	if rl == nil {
		return http.StatusServiceUnavailable, errBody("this atrium is not a room linked to a hub, so it cannot " +
			"reach room " + room)
	}

	cctx, cancel := context.WithTimeout(ctx, relayWait)
	defer cancel()
	res, err := rl.Say(cctx, RelaySay{From: wire, Room: room, To: name, Text: text, When: when})
	switch {
	case errors.Is(err, ErrRelayOld):
		return http.StatusBadGateway, errBody(err.Error())
	case errors.Is(err, ErrRelayDown), err == nil && res.Unreachable:
		why := res.Error
		if err != nil {
			why = err.Error()
		}
		if _, herr := d.holdRelay(sender, wire, name, room, "", text, when, store.RelaySourceSay); herr != nil {
			return http.StatusInternalServerError, errBody("could not reach " + room + " (" + why +
				") and could not hold the message either: " + herr.Error())
		}
		d.reportedAcross(sender, to, "")
		log.Printf("[atrium] %s's message to %s is held: %s", wire, to, why)
		return http.StatusOK, map[string]any{
			"delivered": "held", "to": to, "when": whenWord(when == WhenDone),
			"note": "the hub or room " + room + " is not answering (" + why + "). held on this room and sent " +
				"when it answers, for up to 24 hours. nothing is queued on the hub.",
		}
	case errors.Is(err, ErrRelayUnconfirmed), err == nil && res.Unconfirmed:
		why := res.Error
		if err != nil {
			why = err.Error()
		}
		log.Printf("[atrium] %s's message to %s is unconfirmed: %s", wire, to, why)
		return http.StatusOK, map[string]any{
			"delivered": "unconfirmed", "to": to,
			"note": "it may or may not have reached " + to + " (" + why + "). it is not held, so it will not " +
				"arrive twice. ask whether it arrived before sending it again.",
		}
	case err != nil:
		return http.StatusBadGateway, errBody(err.Error())
	case !res.OK:
		code := res.Code
		if code < 400 {
			code = http.StatusBadGateway
		}
		return code, errBody(res.Error)
	}
	if res.To != "" {
		to = res.To
	}
	d.reportedAcross(sender, to, res.Card)
	log.Printf("[atrium] %s told %s something across rooms (%d chars, %s)", wire, to, len(text), res.Delivered)
	out := map[string]any{"delivered": res.Delivered, "to": to, "card": res.Card, "when": res.When}
	switch {
	case res.Warning != "":
		out["warning"] = res.Warning
	case res.Delivered == "queued" && res.When == WhenDone:
		out["note"] = turnQueuedNote
	case res.Delivered == "queued":
		out["note"] = queuedNote
	case res.Delivered == "terminal":
		out["note"] = typedNote
	}
	return http.StatusOK, out
}

// reportedAcross marks a worker reported when what it said went to its
// launcher on another room. The address it was resolved to and the card are
// both compared, since either may be what the lineage recorded.
func (d *Daemon) reportedAcross(sender *store.Task, to, card string) {
	if sender == nil || !sender.Launched() {
		return
	}
	name, room, ok := d.remoteLauncher(sender)
	if !ok {
		return
	}
	hit := sameAddress(to, name+"@"+room)
	if !hit && card != "" {
		// `room~id` splits as the id and its room.
		if cid, croom, err := SplitAddress(card); err == nil {
			_, lid, _ := d.remoteLauncherCard(sender)
			hit = lid != "" && lid == cid && strings.EqualFold(croom, room)
		}
	}
	if !hit {
		return
	}
	if err := d.st.MarkReported(sender.ID); err != nil {
		log.Printf("[atrium] could not record that %s reported: %v", sender.DisplayTitle(), err)
	}
}

// sameAddress compares two `name@room` addresses, the room without case.
func sameAddress(a, b string) bool {
	an, ar, err1 := SplitAddress(a)
	bn, br, err2 := SplitAddress(b)
	return err1 == nil && err2 == nil && an == bn && strings.EqualFold(ar, br)
}

// ── a launcher on another room ────────────────────────────────────────────────

// remoteLauncher is the launcher's name and room when it is on another room.
// A card whose launcher is here has none: launcherOf answers for it.
func (d *Daemon) remoteLauncher(worker *store.Task) (name, room string, ok bool) {
	if worker == nil || !worker.Launched() {
		return "", "", false
	}
	n, r, err := SplitAddress(worker.SpawnedBy)
	if err != nil {
		return "", "", false
	}
	other := d.otherRoom(r)
	if other == "" {
		return "", "", false
	}
	return n, other, true
}

// remoteLauncherCard is the launcher's bare card id on its room, when the
// lineage recorded it as `room~id`. See LaunchRequest.SpawnedByID.
func (d *Daemon) remoteLauncherCard(worker *store.Task) (room, id string, ok bool) {
	tag := strings.TrimSpace(worker.SpawnedByID)
	i := strings.Index(tag, "~")
	if i <= 0 || i == len(tag)-1 {
		return "", "", false
	}
	return tag[:i], tag[i+1:], true
}

// launcherRelay is the outbox row for a notice to a remote launcher, or nil.
func (d *Daemon) launcherRelay(worker *store.Task, text string) *store.RelaySpec {
	name, room, ok := d.remoteLauncher(worker)
	if !ok {
		return nil
	}
	card := ""
	if r, id, ok := d.remoteLauncherCard(worker); ok && strings.EqualFold(r, room) {
		card = id
	}
	return &store.RelaySpec{
		FromTask: worker.ID, FromWire: worker.WireName, ToRoom: room, ToName: name, ToCard: card,
		Text: truncatePeer(text),
	}
}

// ── the outbox ──────────────────────────────────────────────────────────────

// holdRelay keeps one message for later and starts a drain.
func (d *Daemon) holdRelay(sender *store.Task, wire, name, room, card, text, when, source string) (*store.RelayRow, error) {
	row := store.RelayRow{FromWire: wire, ToRoom: room, ToName: name, ToCard: card, Text: text,
		When: when, Source: source}
	if sender != nil {
		row.FromTask = sender.ID
	}
	held, err := d.st.HoldRelay(row)
	if err != nil {
		return nil, err
	}
	go d.drainRelays()
	return held, nil
}

// drainRelays sends what is owed. One at a time, and a kick that arrives
// during one runs it again rather than being lost.
func (d *Daemon) drainRelays() {
	if !d.relays.draining.TryLock() {
		d.relays.again.Store(true)
		return
	}
	defer d.relays.draining.Unlock()
	for {
		d.relays.again.Store(false)
		d.drainOnce()
		if !d.relays.again.Load() {
			return
		}
	}
}

// drainOnce is one pass over the outbox, oldest first.
func (d *Daemon) drainOnce() {
	rows, err := d.st.OwedRelays(200)
	if err != nil {
		log.Printf("[atrium] could not read what is owed to other rooms: %v", err)
		return
	}
	rl := d.relay()
	for _, r := range rows {
		if time.Since(r.CreatedAt) > RelayKeep {
			d.giveUpRelay(r, fmt.Sprintf("it was held for %s and %s never answered", RelayKeep, r.ToRoom))
			continue
		}
		if rl == nil {
			continue
		}
		to := r.ToName
		if r.ToCard != "" {
			to = r.ToCard
		}
		ctx, cancel := context.WithTimeout(context.Background(), relayWait)
		res, err := rl.Say(ctx, RelaySay{From: r.FromWire, Room: r.ToRoom, To: to, Text: r.Text, When: r.When})
		cancel()
		switch {
		case err == nil && res.OK:
			if err := d.st.RelaySent(r.ID); err != nil {
				log.Printf("[atrium] sent a held message to %s@%s but could not clear it: %v", r.ToName, r.ToRoom, err)
			}
			log.Printf("[atrium] sent %s's held message to %s@%s (%s)", r.FromWire, r.ToName, r.ToRoom, res.Delivered)
		case errors.Is(err, ErrRelayDown), errors.Is(err, ErrRelayOld):
			// THE HUB, NOT THIS ROW. Every other row would fail the same way, so
			// the pass stops here and the next kick tries again.
			_ = d.st.RelayFailed(r.ID, err.Error())
			return
		case err == nil && res.Unreachable:
			_ = d.st.RelayFailed(r.ID, res.Error)
		case errors.Is(err, ErrRelayUnconfirmed), err == nil && res.Unconfirmed:
			why := res.Error
			if err != nil {
				why = err.Error()
			}
			if r.Source == store.RelaySourceNotice {
				// A launcher told twice beats a launcher never told.
				_ = d.st.RelayFailed(r.ID, why)
				continue
			}
			d.giveUpRelay(r, "it may or may not have arrived ("+why+"), so it is not sent again")
		case err != nil:
			_ = d.st.RelayFailed(r.ID, err.Error())
		default:
			d.giveUpRelay(r, res.Error)
		}
	}
}

// giveUpRelay drops a row and says so on the sender's card and in the log.
func (d *Daemon) giveUpRelay(r store.RelayRow, why string) {
	if err := d.st.RelaySent(r.ID); err != nil {
		log.Printf("[atrium] could not drop a held message to %s@%s: %v", r.ToName, r.ToRoom, err)
		return
	}
	log.Printf("[atrium] gave up on %s's message to %s@%s: %s", r.FromWire, r.ToName, r.ToRoom, why)
	if r.FromTask == "" {
		return
	}
	if err := d.st.AppendEvent(r.FromTask, store.EventNotified, map[string]any{
		"kind": "relay-dropped", "to": r.ToName + "@" + r.ToRoom, "why": why, "text": r.Text,
	}); err != nil {
		log.Printf("[atrium] could not record the dropped message on %s: %v", r.FromTask, err)
		return
	}
	d.publishTask(r.FromTask)
}

// ── peers on other rooms ────────────────────────────────────────────────────

// handleRoomPeers is `GET /v1/peers/rooms`: the sessions on every other room,
// asked of the hub.
func (d *Daemon) handleRoomPeers(w http.ResponseWriter, r *http.Request) {
	rl := d.relay()
	if rl == nil {
		writeJSONErr(w, http.StatusServiceUnavailable, errString("this atrium is not a room linked to a hub, "+
			"so it has no other rooms to list"))
		return
	}
	all := r.URL.Query().Get("all") != "" && r.URL.Query().Get("all") != "0"
	ctx, cancel := context.WithTimeout(r.Context(), relayWait)
	defer cancel()
	list, note, err := rl.Peers(ctx, all)
	if err != nil {
		writeJSONErr(w, http.StatusBadGateway, err)
		return
	}
	if list == nil {
		list = []RemotePeer{}
	}
	body := map[string]any{"peers": list}
	if note != "" {
		body["note"] = note
	}
	writeJSONCode(w, http.StatusOK, body)
}

// ── small helpers ───────────────────────────────────────────────────────────

func errBody(msg string) map[string]any { return map[string]any{"error": msg} }

func writeJSONCode(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

// answerRecorder holds one handler's answer so a door can add to it.
type answerRecorder struct {
	header http.Header
	code   int
	body   bytes.Buffer
}

func (a *answerRecorder) Header() http.Header {
	if a.header == nil {
		a.header = http.Header{}
	}
	return a.header
}

func (a *answerRecorder) WriteHeader(code int) { a.code = code }

func (a *answerRecorder) Write(p []byte) (int, error) { return a.body.Write(p) }
