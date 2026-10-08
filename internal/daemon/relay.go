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

// A message to a card on another room. See docs/fabric/cross-room-say-design.md.
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
	// Peers lists the sessions on other rooms. With everywhere it asks for the
	// cards tagged atrium:everywhere and nothing else.
	Peers(ctx context.Context, all, everywhere bool) ([]RemotePeer, string, error)
	// Find looks a bare name up among the cards tagged atrium:everywhere on other
	// rooms. A read: the result's To and Card name the one match, or it is a
	// refusal. See docs/rnd/everywhere-card-design.md.
	Find(ctx context.Context, name string) (RelayResult, error)
	// Card reads `to` on `room`, and Exit asks it to leave. The result's Task
	// is the card, and To and Card name it across.
	Card(ctx context.Context, room, to string, events bool) (RelayResult, error)
	// Exit carries who is asking (from, a handle on THIS room) and whether they
	// force it, because the room that owns the card makes the call. See
	// api.guardExit.
	Exit(ctx context.Context, room, to, from string, force bool) (RelayResult, error)
	// Launch starts a session on another room. See relay_launch.go.
	Launch(ctx context.Context, l RelayLaunch) (RelayResult, error)
}

// RelaySay is one message for another room. From is the sender's handle here,
// with no room: the hub adds this room's name from its certificate.
type RelaySay struct {
	From, Room, To, Text, When string
	// Kind is `fyi` or empty, the say's kind, so a launcher's fyi on another room
	// makes no report owed there either. See fyi.go. An older hub drops it.
	Kind string
	// Wake resumes a parked target so the message can be delivered. A held row carries no wake,
	// so a say that asks for one is refused, not held, when the hub cannot be reached.
	Wake bool
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
	Task        *RemoteTask
	// What a launch started: where to watch it, where its briefing was written on the
	// target's disk, and the model and effort it ran with.
	Watch, Brief, Model, Effort string
}

// RemoteTask is one card on another room, as atrium_task reports it.
type RemoteTask struct {
	Card    string        `json:"card"`
	Handle  string        `json:"handle,omitempty"`
	Title   string        `json:"title,omitempty"`
	Status  string        `json:"status"`
	Doing   string        `json:"doing,omitempty"`
	Where   string        `json:"where,omitempty"`
	Why     string        `json:"why,omitempty"`
	Idle    int           `json:"idle_seconds,omitempty"`
	Waiting int           `json:"waiting_seconds,omitempty"`
	Owned   bool          `json:"atrium_owns_terminal"`
	Events  []RemoteEvent `json:"events,omitempty"`
}

// RemoteEvent is one event on a RemoteTask.
type RemoteEvent struct {
	At   string `json:"at"`
	Kind string `json:"kind"`
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
	// Everywhere is set by a hub that knows cards on every room, on the rows that
	// are there because of it. An older hub never sets it.
	Everywhere bool `json:"everywhere,omitempty"`
	// Tags are the card's tags, when the hub sends them. W6 looks for atrium:orchestrator.
	Tags []string `json:"tags,omitempty"`
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
	// kick starts a drain. Nil is `go drainRelays`, and a test sets it to do
	// nothing so it can drain when it chooses.
	kick func()
}

// kickRelays starts a drain in the background.
func (d *Daemon) kickRelays() {
	if k := d.relays.kick; k != nil {
		k()
		return
	}
	go d.drainRelays()
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
func (d *Daemon) RelayAttached() { d.kickRelays() }

// ── the doors ───────────────────────────────────────────────────────────────

// sayIn is the body every door takes.
type sayIn struct {
	From string `json:"from"`
	To   string `json:"to"`
	Text string `json:"text"`
	When string `json:"when"`
	// Reply asks for an answer, which stays owed on the receiver until given.
	Reply bool `json:"reply"`
	// Wake resumes a parked card so the message can be delivered, here or on the room it is for.
	Wake bool `json:"wake"`
	// Kind is `fyi` or `needs`. See fyi.go. Carried to another room too.
	Kind string `json:"kind"`
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
		code, body := d.sayAcross(r.Context(), strings.TrimSpace(in.From), name, other, in.Text, in.When, in.Kind, in.Reply, in.Wake)
		writeJSONCode(w, code, body)
		return
	}
	target, via := d.localTargetVia(name)
	if target == nil {
		from := strings.TrimSpace(in.From)
		done, note := d.sayEverywhere(w, r.Context(), from, name, in.Text, in.When, in.Kind, in.Reply, in.Wake, nil)
		if !done {
			d.writeMissNote(w, from, name, "say", in.Text, in.When, in.Reply, note)
		}
		return
	}
	// THE SAME HANDLER, over the same body, so there is one way a local message
	// is delivered. Its answer gains who it went to.
	raw, _ := json.Marshal(map[string]any{"text": in.Text, "from": strings.TrimSpace(in.From), "when": in.When,
		"reply": in.Reply, "wake": in.Wake, "kind": in.Kind})
	inner, err := http.NewRequestWithContext(withSayTrace(r.Context(), name, via), http.MethodPost,
		"/v1/tasks/"+target.ID+"/message", bytes.NewReader(raw))
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
	t, _ := d.localTargetVia(name)
	return t
}

// localTargetVia is localTarget and how it matched: `handle`, `alias` or `card`.
// All three are EXACT. A near miss is never resolved to. See saylog.go.
func (d *Daemon) localTargetVia(name string) (*store.Task, string) {
	if t, err := d.st.GetByWireName(d.st.Qualify(name)); err == nil {
		return t, "handle"
	}
	if t, err := d.st.GetByAlias(name); err == nil {
		return t, "alias"
	}
	if t, err := d.st.Get(name); err == nil {
		return t, "card"
	}
	return nil, ""
}

// sayAcross relays one message to `name` on `room`, and answers the sender.
func (d *Daemon) sayAcross(ctx context.Context, from, name, room, text, when, kind string, reply, wake bool) (int, map[string]any) {
	text = strings.TrimSpace(text)
	// The relay carries no reply flag, so an fyi that asks for one goes as the ordinary say.
	kind = promptKind(kind, reply)
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
	// The row, as far as this room can see the say. The other room writes its own
	// when it lands. See docs/runtime/say-lifecycle-design.md.
	rec := store.Say{FromWire: wire, FromTask: d.senderTask(from), ToInput: to, ToWire: to, Via: "remote",
		Room: room, Door: "say", When: whenWord(when == WhenDone), ReplyWant: reply}
	res, err := rl.Say(cctx, RelaySay{From: wire, Room: room, To: name, Text: text, When: when, Kind: kind, Wake: wake})
	switch {
	case errors.Is(err, ErrRelayOld):
		return http.StatusBadGateway, errBody(err.Error())
	case errors.Is(err, ErrRelayDown), err == nil && res.Unreachable:
		why := res.Error
		if err != nil {
			why = err.Error()
		}
		if wake {
			// NOT HELD: the outbox row has no wake, so it would arrive at a parked card and be
			// refused there, and the sender would have been told it was sent.
			// A 424, NOT A 503. The hub's own atrium_say reads a 502, 503 or 504 from this room as
			// "it may still deliver", which is the opposite of this: nothing was sent. 424 is the
			// request's dependency (the hub or the room) failing, a code nothing else here uses
			// for an in-flight message, and 409 is already this door's "two cards match".
			return http.StatusFailedDependency, errBody("could not reach " + room + " (" + why +
				"). nothing was sent or held, since a held message cannot wake a card. send it again with wake=true " +
				"when the room is back")
		}
		held, herr := d.holdRelay(sender, wire, name, room, "", text, when, kind, store.RelaySourceSay)
		if herr != nil {
			return http.StatusInternalServerError, errBody("could not reach " + room + " (" + why +
				") and could not hold the message either: " + herr.Error())
		}
		d.reportedAcross(sender, to, "", text)
		log.Printf("[atrium] %s's message to %s is held: %s", wire, to, why)
		// Held on this room, and moved on by the drain. See docs/runtime/say-lifecycle-design.md.
		rec.State, rec.RelayID, rec.Note = store.SayHeld, held.ID, "not answering: "+why
		return http.StatusOK, map[string]any{
			"delivered": "held", "to": to, "when": whenWord(when == WhenDone),
			"say": d.recordSay(rec, text), "via": "remote",
			"note": "the hub or room " + room + " is not answering (" + why + "). held on this room and sent " +
				"when it answers, for up to 24 hours. nothing is queued on the hub.",
		}
	case errors.Is(err, ErrRelayUnconfirmed), err == nil && res.Unconfirmed:
		why := res.Error
		if err != nil {
			why = err.Error()
		}
		log.Printf("[atrium] %s's message to %s is unconfirmed: %s", wire, to, why)
		rec.State, rec.Note = store.SayUnconfirmed, why
		return http.StatusOK, map[string]any{
			"delivered": "unconfirmed", "to": to, "say": d.recordSay(rec, text), "via": "remote",
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
	if wake && res.Delivered == "parked" {
		// THE HUB DROPPED THE WAKE. An older hub ignores the field, and the target answered as it
		// does to any say without one. Sending again with wake=true would loop. A 424 for the
		// reason given at the refusal above.
		log.Printf("[atrium] %s's wake say to %s came back parked: the hub is older than cross-room wake", wire, to)
		rec.State, rec.Note = store.SayRefused, "parked: the hub is older than cross-room wake"
		d.recordSay(rec, text)
		return http.StatusFailedDependency, errBody("not delivered: " + to + " is parked, and the hub is older than " +
			"cross-room wake, so it did not resume it. update the hub, or ask someone on " + room + " to resume it")
	}
	if res.To != "" {
		to = res.To
	}
	d.reportedAcross(sender, to, res.Card, text)
	log.Printf("[atrium] %s told %s something across rooms (%d chars, %s)", wire, to, len(text), res.Delivered)
	rec.ToWire, rec.State, rec.Note = to, store.SayHanded, "the hub took it: "+res.Delivered
	if res.Delivered == "terminal" {
		rec.State, rec.Channel = store.SayDelivered, store.SayViaTerminal
	}
	out := map[string]any{"delivered": res.Delivered, "to": to, "card": res.Card, "when": res.When,
		"say": d.recordSay(rec, text), "via": "remote"}
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
func (d *Daemon) reportedAcross(sender *store.Task, to, card, text string) {
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
	d.doneBySay(sender, text)
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
func (d *Daemon) holdRelay(sender *store.Task, wire, name, room, card, text, when, kind, source string) (*store.RelayRow, error) {
	row := store.RelayRow{FromWire: wire, ToRoom: room, ToName: name, ToCard: card, Text: text,
		When: when, Kind: kind, Source: source}
	if sender != nil {
		row.FromTask = sender.ID
	}
	held, err := d.st.HoldRelay(row)
	if err != nil {
		return nil, err
	}
	d.kickRelays()
	return held, nil
}

// drainRelays sends what is owed. One at a time, and a kick that arrives
// during one runs it again rather than being lost.
//
// THE FLAG IS READ AFTER THE UNLOCK, not before. A kick that lands between
// the last pass and the unlock finds the lock held and sets the flag, and this
// sees it. One that lands after the unlock takes the lock and drains itself.
func (d *Daemon) drainRelays() {
	for {
		if !d.relays.draining.TryLock() {
			d.relays.again.Store(true)
			return
		}
		d.relays.again.Store(false)
		d.drainOnce()
		d.relays.draining.Unlock()
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
		res, err := rl.Say(ctx, RelaySay{From: r.FromWire, Room: r.ToRoom, To: to, Text: r.Text, When: r.When, Kind: r.Kind})
		cancel()
		switch {
		case err == nil && res.OK:
			if err := d.st.RelaySent(r.ID); err != nil {
				log.Printf("[atrium] sent a held message to %s@%s but could not clear it: %v", r.ToName, r.ToRoom, err)
			}
			log.Printf("[atrium] sent %s's held message to %s@%s (%s)", r.FromWire, r.ToName, r.ToRoom, res.Delivered)
			_ = d.st.SayRelayed(r.ID, store.SayHanded, "sent after being held: the hub took it: "+res.Delivered)
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
	_ = d.st.SayRelayed(r.ID, store.SayRefused, "given up: "+why)
	if r.FromTask == "" {
		return
	}
	if err := d.st.AppendEvent(r.FromTask, store.EventNotified, map[string]any{
		"kind": "relay-dropped", "to": r.ToName + "@" + r.ToRoom, "why": why, "text": r.Text,
	}); err != nil {
		log.Printf("[atrium] could not record the dropped message on %s: %v", r.FromTask, err)
		return
	}
	// The words go back to the sender as a message on its card, so they are kept and counted as undelivered until it
	// has read them, not only written in a timeline event.
	note := fmt.Sprintf("your message to %s@%s was not delivered (%s). what you said: %s", r.ToName, r.ToRoom, why, r.Text)
	if _, err := d.st.QueuePeerKind(r.FromTask, note, "atrium", store.PromptFYI, true); err != nil {
		log.Printf("[atrium] could not hand the dropped message back to %s: %v", r.FromTask, err)
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
	everywhere := r.URL.Query().Get("everywhere") != "" && r.URL.Query().Get("everywhere") != "0"
	ctx, cancel := context.WithTimeout(r.Context(), relayWait)
	defer cancel()
	list, note, err := rl.Peers(ctx, all, everywhere)
	if err != nil {
		writeJSONErr(w, http.StatusBadGateway, err)
		return
	}
	if everywhere {
		list = onlyEverywhere(list)
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

// onlyEverywhere keeps the rows that carry the flag. A hub that predates the
// field ignores the ask and answers every room's sessions, none of them marked,
// and this is what keeps those out of the list.
func onlyEverywhere(list []RemotePeer) []RemotePeer {
	var out []RemotePeer
	for _, p := range list {
		if p.Everywhere {
			out = append(out, p)
		}
	}
	return out
}

// ── a bare name on another room ─────────────────────────────────────────────

// What a miss says when the hub could not be asked.
const (
	hubSilentNote = "the hub is not answering, so no card on another room was looked for."
	hubOldNote    = "the hub is older than cards on every room, so no card on another room was looked for."
)

// findEverywhere asks the hub for the card tagged atrium:everywhere that `name`
// means. On a match `room` and `handle` name it. Otherwise `code` is 409 for
// two or more, and `note` is what to tell the sender: the hub's own sentence for
// a miss or a 409, or why it could not be asked. Nothing is held, since a name
// this room cannot resolve may be a typo.
func (d *Daemon) findEverywhere(ctx context.Context, name string) (room, handle string, code int, note string) {
	rl := d.relay()
	if rl == nil {
		return "", "", 0, ""
	}
	cctx, cancel := context.WithTimeout(ctx, relayWait)
	defer cancel()
	res, err := rl.Find(cctx, name)
	switch {
	case errors.Is(err, ErrRelayOld):
		return "", "", 0, hubOldNote
	case err != nil:
		return "", "", 0, hubSilentNote
	case !res.OK && strings.Contains(res.Error, "does not know the relay op"):
		return "", "", 0, hubOldNote
	case !res.OK && res.Unreachable:
		return "", "", 0, hubSilentNote
	case !res.OK:
		return "", "", res.Code, res.Error
	}
	i := strings.LastIndex(res.To, "@")
	if i <= 0 || i == len(res.To)-1 {
		return "", "", 0, ""
	}
	return res.To[i+1:], res.To[:i], 0, ""
}

// sayEverywhere is the fall-through of a say or a tell whose bare name missed on
// this room. One match goes on through sayAcross with its room written out, so
// holding, `unconfirmed` and the outbox are what they are for a typed
// `name@room`. Two or more is a 409. It reports whether it answered, and
// otherwise leaves the miss to the caller, with the note to put on it.
func (d *Daemon) sayEverywhere(w http.ResponseWriter, ctx context.Context, from, name, text, when, kind string,
	reply, wake bool, after func(int, map[string]any)) (bool, string) {

	// A say with no sender or no words is refused by sayAcross as it would be
	// for a typed address, and is not worth a trip to the hub to find out.
	if from == "" || strings.TrimSpace(text) == "" {
		return false, ""
	}
	room, handle, code, note := d.findEverywhere(ctx, name)
	switch {
	case room != "":
		c, body := d.sayAcross(ctx, from, handle, room, text, when, kind, reply, wake)
		if c < 400 {
			// SAID, because the sender typed a bare name and should learn where it went.
			routed := fmt.Sprintf("%q is not on this room. it went to %s on room %s, the one card that answers to it.",
				name, handle, room)
			if n, _ := body["note"].(string); n != "" {
				routed += " " + n
			}
			body["note"] = routed
		}
		if after != nil {
			after(c, body)
		}
		writeJSONCode(w, c, body)
		return true, ""
	case code == http.StatusConflict:
		writeJSONCode(w, code, errBody(note))
		return true, ""
	}
	return false, note
}

// ── one card on another room ────────────────────────────────────────────────

// handleRoomCard is `GET /v1/peers/card?to=<address>`: one card on another
// room, asked of the hub, for atrium_task. `events=1` adds its recent events.
// An address on this room answers `{"local": name}`, and the caller reads the
// card here the way it always has. See item 68 in docs/backlog-2.md.
func (d *Daemon) handleRoomCard(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	events := q.Get("events") != "" && q.Get("events") != "0"
	d.acrossRoom(w, r.Context(), q.Get("to"), func(ctx context.Context, rl Relay, name, room string) (RelayResult, error) {
		return rl.Card(ctx, room, name, events)
	}, func(res RelayResult) map[string]any {
		return map[string]any{"task": res.Task}
	})
}

// handleRoomExit is `POST /v1/peers/exit` with `{"to": <address>}`: ask a card
// on another room to leave, through the hub, for atrium_exit. An address on
// this room answers `{"local": name}`, as handleRoomCard does.
func (d *Daemon) handleRoomExit(w http.ResponseWriter, r *http.Request) {
	var in struct {
		To    string `json:"to"`
		From  string `json:"from"`
		Force bool   `json:"force"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err)
		return
	}
	d.acrossRoom(w, r.Context(), in.To, func(ctx context.Context, rl Relay, name, room string) (RelayResult, error) {
		return rl.Exit(ctx, room, name, strings.TrimSpace(in.From), in.Force)
	}, func(res RelayResult) map[string]any {
		return map[string]any{"asked": true, "card": res.Card, "handle": res.To}
	})
}

// acrossRoom is the part the two share: split the address, answer a local one
// as local, relay the rest and turn the hub's answer into this room's.
func (d *Daemon) acrossRoom(w http.ResponseWriter, ctx context.Context, to string,
	ask func(context.Context, Relay, string, string) (RelayResult, error), ok func(RelayResult) map[string]any) {

	name, room, err := SplitAddress(to)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err)
		return
	}
	other := d.otherRoom(room)
	if other == "" {
		writeJSONCode(w, http.StatusOK, map[string]any{"local": name})
		return
	}
	rl := d.relay()
	if rl == nil {
		writeJSONCode(w, http.StatusServiceUnavailable, errBody("this atrium is not a room linked to a hub, so it "+
			"cannot reach room "+other))
		return
	}
	cctx, cancel := context.WithTimeout(ctx, relayWait)
	defer cancel()
	res, err := ask(cctx, rl, name, other)
	switch {
	case errors.Is(err, ErrRelayUnconfirmed):
		writeJSONCode(w, http.StatusGatewayTimeout, errBody(err.Error()+". it may have gone through: look "+
			"before asking again"))
		return
	case err != nil:
		writeJSONCode(w, http.StatusBadGateway, errBody(err.Error()))
		return
	case !res.OK && strings.Contains(res.Error, "does not know the relay op"):
		// A HUB OLDER THAN THIS. It carries a say, not this.
		writeJSONCode(w, http.StatusBadGateway, errBody("the hub is older than reaching a card on another "+
			"room, so it cannot carry this. update the hub"))
		return
	case !res.OK:
		code := res.Code
		if code < 400 {
			code = http.StatusBadGateway
		}
		writeJSONCode(w, code, errBody(res.Error))
		return
	}
	writeJSONCode(w, http.StatusOK, ok(res))
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
