package conn

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
const relayKind = wire.RelayKind

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
	// RelayHub asks the hub's own backlog or reports endpoint, for a room's atrium_backlog and
	// atrium_reports. The hub answers it itself, with no other room in it. See hubask.go.
	RelayHub = "hub"
)

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
	// Data is the hub endpoint's own JSON answer, for `hub`.
	Data json.RawMessage `json:"data,omitempty"`
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
func (r *Room) Relay(ctx context.Context, req wire.RelayRequest) (RelayAnswer, error) {
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
	if err := writeJSONLimit(conn, req, announceMax); err != nil {
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
	var req wire.RelayRequest
	if err := readJSONLimit(br, &req, announceMax); err != nil {
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

// controlTimeout bounds every call these tools make to the hub's own board.
//
// Short. All of them are local HTTP against a listener in this process, and the
// failure worth reporting quickly is "the board is down", not "a slow one".
const controlTimeout = 8 * time.Second
