package link

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"time"
)

// The relay: a room asking its hub to carry one message, or one question, to
// another room. See docs/fabric/cross-room-say-design.md.
//
// ── dialled by the room, like everything else ───────────
//
// The hub never reaches into a room of its own accord, and this does not change
// that. A room with something to say dials a `relay` connection, says hello,
// sends one request and reads one answer. The hub answers by asking the target
// room over the link it already holds, exactly the way the board does.
//
// ── and the hub holds nothing ───────────────────────────
//
// The answer is the target room's answer, and the hub forgets the request the
// moment it has written it. A target that is not answering is said to be
// unreachable, and the SENDER'S room decides whether to keep the message. See
// the outbox in internal/daemon/relay.go.

// relayKind is the connection kind. An older hub refuses it with the sentence
// in hearHello, which is how a room finds out the hub cannot carry this.
const relayKind = "relay"

// relayWait bounds one relay exchange. The hub resolves the name and posts the
// message, two loopback calls into a proxied room, each bounded by
// controlTimeout.
const relayWait = 3 * controlTimeout

// Relay ops.
const (
	RelaySay   = "say"
	RelayPeers = "peers"
	// RelayCard reads one card on another room, and RelayExit asks one to
	// leave. What atrium_task and atrium_exit do across rooms. See item 68 in
	// docs/backlog-2.md.
	RelayCard = "card"
	RelayExit = "exit"
	// RelayFind looks a bare name up among the cards tagged atrium:everywhere
	// on the other rooms. A read, with no delivery in it. See everywhere.go.
	RelayFind = "find"
	// RelayLaunch starts a session on another room, for a room's atrium_launch with
	// `room`. Not idempotent, so nothing is ever held for it. See control_relay_launch.go.
	RelayLaunch = "launch"
)

// RelayRequest is what a room asks its hub to carry.
type RelayRequest struct {
	Op string `json:"op"`
	// From is the sender's handle ON THE ASKING ROOM. The hub adds the room from
	// the connection, never from here, so a room cannot speak for another.
	From string `json:"from,omitempty"`
	// Room and To are the target: a room other than the asking one, and a
	// handle, alias or card id on it.
	Room string `json:"room,omitempty"`
	To   string `json:"to,omitempty"`
	Text string `json:"text,omitempty"`
	When string `json:"when,omitempty"`
	// All includes cards with no session, for `peers`.
	All bool `json:"all,omitempty"`
	// Everywhere asks `peers` for the cards tagged atrium:everywhere on other
	// rooms and nothing else. An older hub ignores it.
	Everywhere bool `json:"everywhere,omitempty"`
	// Events includes the card's recent events, for `card`.
	Events bool `json:"events,omitempty"`
	// Launch is the session to start, for `launch`. An older hub ignores it and
	// refuses the op by name.
	Launch *RelayLaunchSpec `json:"launch,omitempty"`
}

// RelayLaunchSpec is one launch for another room, field for field what the hub's own
// atrium_launch takes beside `room`. Room is the target, From (on the request) the
// launcher on the asking room.
type RelayLaunchSpec struct {
	Cwd        string            `json:"cwd"`
	Title      string            `json:"title,omitempty"`
	Why        string            `json:"why,omitempty"`
	Prompt     string            `json:"prompt,omitempty"`
	Brief      string            `json:"brief,omitempty"`
	Runner     string            `json:"runner,omitempty"`
	Tags       []string          `json:"tags,omitempty"`
	Model      string            `json:"model,omitempty"`
	Effort     string            `json:"effort,omitempty"`
	Args       []string          `json:"args,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	LeanAgents []string          `json:"lean_agents,omitempty"`
	LeanSkills []string          `json:"lean_skills,omitempty"`
}

// RelayAnswer is the hub's answer.
type RelayAnswer struct {
	OK bool `json:"ok"`
	// Code and Error are a refusal, in the target room's words where it gave
	// some. Unreachable marks one the sender's room may hold and try again,
	// rather than one that will never succeed.
	Code        int    `json:"code,omitempty"`
	Error       string `json:"error,omitempty"`
	Unreachable bool   `json:"unreachable,omitempty"`
	// Unconfirmed is a failure AFTER the message may have reached the target,
	// so sending it again could deliver it twice. Never held for a say.
	Unconfirmed bool `json:"unconfirmed,omitempty"`
	// What the target room said about a delivered message.
	Delivered string `json:"delivered,omitempty"`
	When      string `json:"when,omitempty"`
	Warning   string `json:"warning,omitempty"`
	// To is the resolved `handle@room` and Card the tagged `room~id`.
	To    string      `json:"to,omitempty"`
	Card  string      `json:"card,omitempty"`
	Peers []RelayPeer `json:"peers,omitempty"`
	// Task is the card `card` read, named across, or the one `launch` started.
	Task *RelayTask `json:"task,omitempty"`
	// What `launch` started: where to watch it, where its briefing was written on the
	// target's disk, and the model and effort the room says it ran with.
	Watch  string `json:"watch,omitempty"`
	Brief  string `json:"brief,omitempty"`
	Model  string `json:"model,omitempty"`
	Effort string `json:"effort,omitempty"`
}

// RelayTask is one card on another room, as atrium_task reports it. Card is
// `room~id` and Handle `name@room`.
type RelayTask struct {
	Card    string       `json:"card"`
	Handle  string       `json:"handle,omitempty"`
	Title   string       `json:"title,omitempty"`
	Status  string       `json:"status"`
	Doing   string       `json:"doing,omitempty"`
	Where   string       `json:"where,omitempty"`
	Why     string       `json:"why,omitempty"`
	Idle    int          `json:"idle_seconds,omitempty"`
	Waiting int          `json:"waiting_seconds,omitempty"`
	Owned   bool         `json:"atrium_owns_terminal"`
	Events  []RelayEvent `json:"events,omitempty"`
}

// RelayEvent is one event on a RelayTask.
type RelayEvent struct {
	At   string `json:"at"`
	Kind string `json:"kind"`
}

// RelayPeer is one session on another room.
type RelayPeer struct {
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
	// Everywhere marks a card that is here because it carries atrium:everywhere.
	Everywhere bool `json:"everywhere,omitempty"`
}

// ErrRelayOld is a hub older than the relay. Nothing held for it would ever go.
var ErrRelayOld = errors.New("the hub is older than cross-room say, so it cannot carry this. update the hub")

// ErrRelayDown is a hub this room cannot reach right now, found out before the
// request was written. Worth holding for.
var ErrRelayDown = errors.New("the hub is not answering")

// ErrRelayUnconfirmed is the request written and no answer read. The hub may
// have delivered it, so it must not be sent again as though it had not.
var ErrRelayUnconfirmed = errors.New("the hub took the message and did not say what became of it")

// ── the room's side ─────────────────────────────────────

// Relay asks the hub to carry one request, and answers what it said.
//
// An error is ErrRelayOld, or wraps ErrRelayDown or ErrRelayUnconfirmed. A refusal from the target is
// not an error: it is an answer with OK false, which the caller reads.
func (r *Room) Relay(ctx context.Context, req RelayRequest) (RelayAnswer, error) {
	var ans RelayAnswer
	r.mu.Lock()
	up, session := r.up, r.session
	r.mu.Unlock()
	if !up {
		return ans, fmt.Errorf("%w: this room is not attached to it", ErrRelayDown)
	}
	dialCtx, cancel := context.WithTimeout(ctx, r.T.DialWait)
	conn, err := r.Dial.Dial(dialCtx)
	cancel()
	if err != nil {
		return ans, fmt.Errorf("%w: %v", ErrRelayDown, err)
	}
	defer conn.Close()

	br := bufio.NewReader(conn)
	w, err := sayHello(conn, br, hello{Kind: relayKind, Room: r.Name, Session: session})
	if err != nil {
		// THE ONE REFUSAL THAT IS NOT WORTH WAITING OUT. An older hub names the
		// kinds it knows, and relay is not one of them. A hub that names relay
		// among them is not older, whatever else it refused.
		if !w.OK && strings.Contains(w.Error, "a connection is control") && !strings.Contains(w.Error, relayKind) {
			return ans, ErrRelayOld
		}
		return ans, fmt.Errorf("%w: %v", ErrRelayDown, err)
	}
	if err := conn.SetDeadline(time.Now().Add(relayWait)); err != nil {
		return ans, fmt.Errorf("%w: %v", ErrRelayDown, err)
	}
	if err := writeJSON(conn, req); err != nil {
		return ans, fmt.Errorf("%w: %v", ErrRelayDown, err)
	}
	// NOT `readJSON`: a peers answer from a busy hub is more than one frame.
	// Bounded all the same, the way an announcement is.
	if err := json.NewDecoder(io.LimitReader(br, announceMax)).Decode(&ans); err != nil {
		return ans, fmt.Errorf("%w: %v", ErrRelayUnconfirmed, err)
	}
	return ans, nil
}

// ── the hub's side ──────────────────────────────────────

// serveRelay reads one request from an attached room and answers it.
func (h *Hub) serveRelay(ctx context.Context, name string, conn net.Conn, br *bufio.Reader) {
	if h.Relay == nil {
		_ = writeJSON(conn, welcome{OK: false, Error: "this hub carries nothing between rooms"})
		return
	}
	// ONLY FROM A ROOM THAT IS HERE. A certificate that is not attached right
	// now could be anything, including a room forced out of the inventory.
	if !h.Has(name) {
		_ = writeJSON(conn, welcome{OK: false, Error: "attach to this hub before asking it to carry anything"})
		return
	}
	// The welcome first, then the body, the same two frames as an announcement.
	if err := writeJSON(conn, welcome{OK: true}); err != nil {
		return
	}
	if err := conn.SetDeadline(time.Now().Add(handshakeWait)); err != nil {
		return
	}
	var req RelayRequest
	if err := readJSON(br, &req); err != nil {
		log.Printf("[hub] could not read %q's relay: %v", name, err)
		return
	}
	if err := conn.SetDeadline(time.Now().Add(relayWait)); err != nil {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, relayWait)
	defer cancel()
	// THE ASKING ROOM COMES FROM THE CONNECTION, which came from the
	// certificate. See `take`.
	ans := h.Relay(cctx, name, req)
	_ = json.NewEncoder(conn).Encode(ans)
}
