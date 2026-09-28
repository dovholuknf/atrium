package link

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// A fake room's board: some cards, the messages posted to them, and what a
// `/v1/say` was asked. Enough of a room for the hub to resolve a name on it and
// deliver to it, which is all the relay asks of a room.
type relayRoom struct {
	mu    sync.Mutex
	tasks []map[string]any
	got   []map[string]string // messages posted, with the card id under "id"
	said  []map[string]string // bodies of POST /v1/say
	// noSay is a room older than /v1/say, and msgCode a room whose message
	// post fails with this status.
	noSay   bool
	msgCode int
	// launched is the body of the last POST /v1/launch.
	launched map[string]any
}

func (f *relayRoom) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Path == "/v1/tasks" && r.Method == http.MethodGet:
		_ = json.NewEncoder(w).Encode(map[string]any{"tasks": f.tasks})
	case r.URL.Path == "/v1/launch" && r.Method == http.MethodPost:
		_ = json.NewDecoder(r.Body).Decode(&f.launched)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "kid", "wire_name": "kid", "status": "running"})
	case r.URL.Path == "/v1/say" && r.Method == http.MethodPost && !f.noSay:
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.said = append(f.said, body)
		_ = json.NewEncoder(w).Encode(map[string]any{"delivered": "queued", "to": body["to"], "card": "x~1",
			"when": "immediate"})
	case strings.HasPrefix(r.URL.Path, "/v1/tasks/") && strings.HasSuffix(r.URL.Path, "/message"):
		if f.msgCode != 0 {
			w.WriteHeader(f.msgCode)
			_, _ = w.Write([]byte(`{"error":"the room fell over"}`))
			return
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		body["id"] = strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/tasks/"), "/message")
		f.got = append(f.got, body)
		_ = json.NewEncoder(w).Encode(map[string]any{"delivered": "queued", "when": "immediate"})
	default:
		http.NotFound(w, r)
	}
}

func (f *relayRoom) messages() []map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]string(nil), f.got...)
}

func (f *relayRoom) says() []map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]string(nil), f.said...)
}

// relayPair is a hub serving its board and control tools on loopback, and two
// fake rooms attached to it: m1mini and sg4.
type relayPair struct {
	hub         *Hub
	proxy       *Proxy
	board       string
	mini, sg4   *relayRoom
	miniR, sg4R *Room
	stop        func()
	control     *controlMCP
}

func newRelayPair(t *testing.T) *relayPair {
	t.Helper()
	hub := NewHub(Timings{Beat: 200 * time.Millisecond, Silence: time.Second, Warm: 1})
	p := NewProxy(hub, nil, "", nil)
	boardLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = http.Serve(boardLn, p) }()
	p.SetControl(boardLn.Addr().String())

	hubLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = hub.Serve(ctx, hubLn) }()

	x := &relayPair{hub: hub, proxy: p, board: "http://" + boardLn.Addr().String()}
	x.mini = &relayRoom{tasks: []map[string]any{
		{"id": "m1", "wire_name": "sa1", "status": "working", "alias": "sa1"},
	}}
	x.sg4 = &relayRoom{tasks: []map[string]any{
		{"id": "s1", "wire_name": "atrium-87300", "status": "needs-input", "alias": "orch"},
		{"id": "s2", "wire_name": "gone", "status": "done"},
	}}
	room := func(name string, h http.Handler) *Room {
		r := &Room{Name: name, Dial: plain{addr: hubLn.Addr().String()}, Handler: h,
			T: Timings{Beat: 200 * time.Millisecond, Warm: 1, Backoff: 50 * time.Millisecond}}
		go func() { _ = r.Run(ctx) }()
		return r
	}
	x.miniR, x.sg4R = room("m1mini", x.mini), room("sg4", x.sg4)
	waitFor(t, 5*time.Second, func() bool { return hub.Has("m1mini") && hub.Has("sg4") })
	waitFor(t, 5*time.Second, func() bool { return x.miniR.State().Up && x.sg4R.State().Up })
	x.control = newControl(x.board, hub, nil)
	x.stop = func() { cancel(); hubLn.Close(); boardLn.Close() }
	return x
}

func relayCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// THE FEATURE. A card on m1mini names a card on sg4 as `name@room`, the hub
// carries it, and it arrives as a PEER message from `sa1@m1mini`, never as the
// operator.
func TestARelayedMessageArrivesFromTheSenderAtItsRoom(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()

	ans, err := x.miniR.Relay(relayCtx(t), RelayRequest{Op: RelaySay, From: "sa1", Room: "sg4",
		To: "atrium-87300", Text: "the build is green"})
	if err != nil {
		t.Fatalf("relay: %v", err)
	}
	if !ans.OK || ans.To != "atrium-87300@sg4" || ans.Card != "sg4~s1" || ans.Delivered != "queued" {
		t.Fatalf("answer = %+v", ans)
	}
	got := x.sg4.messages()
	if len(got) != 1 || got[0]["id"] != "s1" || got[0]["from"] != "sa1@m1mini" || got[0]["text"] != "the build is green" {
		t.Fatalf("sg4 got %+v, want one message to s1 from sa1@m1mini", got)
	}
}

// An alias works on the far side as it does at home, and the room part folds
// case the way the hub folds room names.
func TestARelayResolvesAnAliasAndFoldsTheRoomName(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()

	ans, err := x.miniR.Relay(relayCtx(t), RelayRequest{Op: RelaySay, From: "sa1", Room: "SG4", To: "@orch", Text: "hi"})
	if err != nil || !ans.OK || ans.To != "atrium-87300@SG4" {
		t.Fatalf("relay by alias = %+v, %v", ans, err)
	}
}

// The name comes from the connection, so a room cannot speak as another. The
// sender's handle carries the asking room's name, whatever the body says.
func TestTheSendersRoomComesFromTheConnection(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()

	_, _ = x.miniR.Relay(relayCtx(t), RelayRequest{Op: RelaySay, From: "sa1@sg4", Room: "sg4", To: "atrium-87300", Text: "x"})
	got := x.sg4.messages()
	if len(got) != 1 || !strings.HasSuffix(got[0]["from"], "@m1mini") {
		t.Fatalf("from = %+v, want it to end in the asking room @m1mini", got)
	}
}

// RULE 3. A message with no sender would be typed as the operator, so the relay
// refuses it.
func TestARelayWithNoSenderIsRefused(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()

	ans, err := x.miniR.Relay(relayCtx(t), RelayRequest{Op: RelaySay, Room: "sg4", To: "atrium-87300", Text: "x"})
	if err != nil || ans.OK || ans.Code != http.StatusBadRequest || !strings.Contains(ans.Error, "operator") {
		t.Fatalf("answer = %+v, %v, want a 400 naming the operator", ans, err)
	}
	if len(x.sg4.messages()) != 0 {
		t.Fatal("nothing should have been delivered")
	}
}

// A name nobody has on that room is a refusal that lists who is there. Not
// unreachable, so the sender's room will not hold it.
func TestANameTheTargetDoesNotHaveIsARefusalThatListsWhoIs(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()

	ans, err := x.miniR.Relay(relayCtx(t), RelayRequest{Op: RelaySay, From: "sa1", Room: "sg4", To: "nobody", Text: "x"})
	if err != nil || ans.OK || ans.Unreachable || ans.Code != http.StatusNotFound || !strings.Contains(ans.Error, "atrium-87300") {
		t.Fatalf("answer = %+v, %v", ans, err)
	}
}

// A room this hub has never heard of is a typo, said at once, not held.
func TestARoomTheHubDoesNotKnowIsARefusal(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()

	ans, err := x.miniR.Relay(relayCtx(t), RelayRequest{Op: RelaySay, From: "sa1", Room: "atlantis", To: "x", Text: "x"})
	if err != nil || ans.OK || ans.Unreachable || ans.Code != http.StatusNotFound || !strings.Contains(ans.Error, "atlantis") {
		t.Fatalf("answer = %+v, %v", ans, err)
	}
}

// C1 from the design review. A message post that fails after it may have
// landed is UNCONFIRMED, never unreachable, so it is not sent twice.
func TestAFailedPostIsUnconfirmedNotUnreachable(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	x.sg4.mu.Lock()
	x.sg4.msgCode = http.StatusBadGateway
	x.sg4.mu.Unlock()

	ans, err := x.miniR.Relay(relayCtx(t), RelayRequest{Op: RelaySay, From: "sa1", Room: "sg4", To: "atrium-87300", Text: "x"})
	if err != nil || ans.OK || ans.Unreachable || !ans.Unconfirmed {
		t.Fatalf("answer = %+v, %v, want unconfirmed", ans, err)
	}
}

// RULE 4. Peers on other rooms come back marked with their room, and the
// asking room's own are left out.
func TestPeersAcrossRoomsAreMarkedWithTheirRoom(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()

	ans, err := x.miniR.Relay(relayCtx(t), RelayRequest{Op: RelayPeers})
	if err != nil || !ans.OK {
		t.Fatalf("peers = %+v, %v", ans, err)
	}
	if len(ans.Peers) != 1 || ans.Peers[0].Handle != "atrium-87300@sg4" || ans.Peers[0].Room != "sg4" ||
		ans.Peers[0].Card != "sg4~s1" || ans.Peers[0].Alias != "orch" {
		t.Fatalf("peers = %+v, want only atrium-87300@sg4 (the done card and m1mini's own left out)", ans.Peers)
	}
}

// A room that is not attached right now has no link to relay over, and says
// so as ErrRelayDown, which is the one worth holding for.
func TestARelayFromADetachedRoomIsDown(t *testing.T) {
	r := &Room{Name: "lonely", Dial: plain{addr: "127.0.0.1:1"}}
	_, err := r.Relay(context.Background(), RelayRequest{Op: RelaySay})
	if !errors.Is(err, ErrRelayDown) {
		t.Fatalf("err = %v, want ErrRelayDown", err)
	}
}

// VERSION SKEW. A hub older than the relay refuses the kind with the sentence
// every hub has sent since the kind switch, and the room reads that as
// ErrRelayOld, which is not held. The fake answers exactly as an old hub.
func TestAnOldHubIsToldApartAndNotHeldFor(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		br := bufio.NewReader(c)
		var h hello
		_ = readJSON(br, &h)
		_ = writeJSON(c, welcome{OK: false, Error: "a connection is control, data, enrol, upgrade or announce"})
	}()
	r := &Room{Name: "m1mini", Dial: plain{addr: ln.Addr().String()}, T: Timings{}.fill()}
	r.up = true
	_, err = r.Relay(context.Background(), RelayRequest{Op: RelaySay})
	if !errors.Is(err, ErrRelayOld) {
		t.Fatalf("err = %v, want ErrRelayOld", err)
	}
}

// A new hub knows the kind: the unknown-kind refusal must not be what a new
// hub says, or every room would think its hub was old.
func TestTheRelayKindIsKnown(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	go func() { _ = writeJSON(a, hello{V: Version, Kind: relayKind, Room: "x"}) }()
	if _, err := hearHello(b, bufio.NewReader(b)); err != nil {
		t.Fatalf("hearHello refused the relay kind: %v", err)
	}
}

// THE HUB-SIDE DOOR. A card on the hub's machine saying `name@room` is
// forwarded to its OWN room's /v1/say, which keeps the record and relays it.
func TestAHubSideSayToAnotherRoomGoesThroughTheSendersRoom(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()

	_, out, err := x.control.sayHandler(relayCtx(t), ctlReq("sa1", "m1mini"),
		sayInput{To: "atrium-87300@sg4", Text: "hello"})
	if err != nil {
		t.Fatalf("say: %v", err)
	}
	said := x.mini.says()
	if len(said) != 1 || said[0]["from"] != "sa1" || said[0]["to"] != "atrium-87300@sg4" || said[0]["text"] != "hello" {
		t.Fatalf("m1mini's /v1/say got %+v", said)
	}
	if out.Delivered != "queued" || out.To != "atrium-87300@sg4" {
		t.Fatalf("out = %+v", out)
	}
	if len(x.sg4.messages()) != 0 {
		t.Fatal("the hub delivered it itself, when the sender's room should have")
	}
}

// A sender's room older than /v1/say: the hub delivers it itself, as
// `sa1@m1mini`, and says the ledger did not see it.
func TestAHubSideSayFromAnOldRoomIsDeliveredDirectly(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	x.mini.mu.Lock()
	x.mini.noSay = true
	x.mini.mu.Unlock()

	_, out, err := x.control.sayHandler(relayCtx(t), ctlReq("sa1", "m1mini"),
		sayInput{To: "atrium-87300@sg4", Text: "hello"})
	if err != nil {
		t.Fatalf("say: %v", err)
	}
	got := x.sg4.messages()
	if len(got) != 1 || got[0]["from"] != "sa1@m1mini" {
		t.Fatalf("sg4 got %+v", got)
	}
	if !strings.Contains(out.Note, "older") {
		t.Fatalf("note = %q, want it to say the room is older", out.Note)
	}
}

// RULE 3 at the hub-side door: an unnamed caller cannot reach another room.
func TestAHubSideSayToAnotherRoomNeedsTheSender(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()

	_, _, err := x.control.sayHandler(relayCtx(t), ctlReq("", "m1mini"), sayInput{To: "atrium-87300@sg4", Text: "x"})
	if err == nil || !strings.Contains(err.Error(), "operator") {
		t.Fatalf("err = %v, want a refusal naming the operator", err)
	}
}

// A bare name is still the caller's own room, and naming the caller's own
// room is the same as naming none.
func TestAHubSideSayToOwnRoomIsLocal(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()

	for _, to := range []string{"atrium-87300", "atrium-87300@sg4", "@orch@SG4"} {
		_, out, err := x.control.sayHandler(relayCtx(t), ctlReq("other", "sg4"), sayInput{To: to, Text: "x"})
		if err != nil || out.Card != "s1" {
			t.Fatalf("say %q = %+v, %v", to, out, err)
		}
	}
	if len(x.mini.says()) != 0 || len(x.sg4.says()) != 0 {
		t.Fatal("a same-room say took the cross-room path")
	}
}

// RULE 5's lineage. A launch onto another room records the launcher as
// `me@myroom` and its card as `myroom~id`, lands on the named room, and hands
// back the new card's handle as `name@room`.
func TestALaunchOntoAnotherRoomRecordsTheLauncherAcross(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()

	_, out, err := x.control.launchHandler(relayCtx(t), ctlReq("sa1", "m1mini"), launchInput{Cwd: "/w", Room: "sg4"})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	x.sg4.mu.Lock()
	got := x.sg4.launched
	x.sg4.mu.Unlock()
	x.mini.mu.Lock()
	wrong := x.mini.launched
	x.mini.mu.Unlock()
	if wrong != nil || got["spawned_by"] != "sa1@m1mini" || got["spawned_by_id"] != "m1mini~m1" {
		t.Fatalf("sg4 launched with %+v, m1mini with %+v", got, wrong)
	}
	if out.Handle != "kid@sg4" || out.Card != "sg4~kid" {
		t.Fatalf("out = %+v, want the handle and card named across", out)
	}
}

// RULE 4 at the hub-side door.
func TestHubSidePeersListOtherRoomsWhenAsked(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()

	_, out, err := x.control.peersHandler(relayCtx(t), ctlReq("sa1", "m1mini"), peersInput{Rooms: true})
	if err != nil {
		t.Fatalf("peers: %v", err)
	}
	var far *peer
	for i := range out.Peers {
		if out.Peers[i].Room == "sg4" {
			far = &out.Peers[i]
		}
	}
	if far == nil || far.Handle != "atrium-87300@sg4" {
		t.Fatalf("peers = %+v, want atrium-87300@sg4 marked with its room", out.Peers)
	}
	_, out, _ = x.control.peersHandler(relayCtx(t), ctlReq("sa1", "m1mini"), peersInput{})
	for _, p := range out.Peers {
		if p.Room != "" {
			t.Fatalf("without rooms, no other room's peer should be listed: %+v", out.Peers)
		}
	}
}
