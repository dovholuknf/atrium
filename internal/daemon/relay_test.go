package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// A fake second room, as far as this room can tell: a Relay that records what
// it was asked and answers what the test says. See docs/fabric/cross-room-say-design.md.
type fakeRelay struct {
	mu     sync.Mutex
	got    []RelaySay
	answer func(RelaySay) (RelayResult, error)
	peers  []RemotePeer
	// reaches is every Card and Exit asked, and reach answers them.
	reaches []RelaySay
	reach   func(RelaySay) (RelayResult, error)
	// found is every name Find was asked, and find answers it.
	found []string
	find  func(string) (RelayResult, error)
}

func (f *fakeRelay) Say(_ context.Context, s RelaySay) (RelayResult, error) {
	f.mu.Lock()
	f.got = append(f.got, s)
	answer := f.answer
	f.mu.Unlock()
	if answer == nil {
		return RelayResult{OK: true, Delivered: "queued", When: WhenImmediate, To: s.To + "@" + s.Room,
			Card: s.Room + "~L1"}, nil
	}
	return answer(s)
}

func (f *fakeRelay) Peers(_ context.Context, _, _ bool) ([]RemotePeer, string, error) {
	return f.peers, "", nil
}

func (f *fakeRelay) Find(_ context.Context, name string) (RelayResult, error) {
	f.mu.Lock()
	f.found = append(f.found, name)
	find := f.find
	f.mu.Unlock()
	if find != nil {
		return find(name)
	}
	return RelayResult{Code: 404, Error: "no card called " + name + " on another room"}, nil
}

// Card and Exit record the request as a RelaySay with Text "card" or "exit",
// and answer reach when it is set.
func (f *fakeRelay) Card(_ context.Context, room, to string, events bool) (RelayResult, error) {
	return f.reached(RelaySay{Room: room, To: to, Text: "card", When: fmt.Sprint(events)})
}

func (f *fakeRelay) Exit(_ context.Context, room, to string) (RelayResult, error) {
	return f.reached(RelaySay{Room: room, To: to, Text: "exit"})
}

func (f *fakeRelay) reached(s RelaySay) (RelayResult, error) {
	f.mu.Lock()
	f.reaches = append(f.reaches, s)
	reach := f.reach
	f.mu.Unlock()
	if reach != nil {
		return reach(s)
	}
	card, handle := s.Room+"~L1", s.To+"@"+s.Room
	return RelayResult{OK: true, To: handle, Card: card,
		Task: &RemoteTask{Card: card, Handle: handle, Status: "working"}}, nil
}

func (f *fakeRelay) reachedAll() []RelaySay {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]RelaySay(nil), f.reaches...)
}

func (f *fakeRelay) says() []RelaySay {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]RelaySay(nil), f.got...)
}

func (f *fakeRelay) set(answer func(RelaySay) (RelayResult, error)) {
	f.mu.Lock()
	f.answer = answer
	f.mu.Unlock()
}

// roomDaemon is a test daemon that is the room m1mini, linked to a fake hub.
func roomDaemon(t *testing.T) (*Daemon, *fakeRelay) {
	t.Helper()
	d := testDaemon(t)
	d.opts.Room = "m1mini"
	d.relays.kick = func() {}
	f := &fakeRelay{}
	d.SetRelay(f)
	return d, f
}

func say(t *testing.T, d *Daemon, body map[string]string) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	d.handleSay(rec, httptest.NewRequest(http.MethodPost, "/v1/say", bytes.NewReader(raw)))
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func owed(t *testing.T, d *Daemon) []store.RelayRow {
	t.Helper()
	rows, err := d.st.OwedRelays(0)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// settle runs the drain a hold would have started. roomDaemon turns the
// background kick off, so every drain in these tests runs here, in order.
func settle(d *Daemon) { d.drainRelays() }

// THE FEATURE, ROOM SIDE. `name@room` is relayed with this room's handle for
// the sender, and the answer names the target as `name@room`.
func TestASayToAnotherRoomIsRelayed(t *testing.T) {
	d, f := roomDaemon(t)
	peerCard(t, d, "sa1")

	code, out := say(t, d, map[string]string{"from": "sa1", "to": "atrium-87300@claude-sg4", "text": "green"})
	if code != http.StatusOK || out["delivered"] != "queued" || out["to"] != "atrium-87300@claude-sg4" {
		t.Fatalf("%d %+v", code, out)
	}
	got := f.says()
	if len(got) != 1 || got[0].From != "sa1" || got[0].Room != "claude-sg4" || got[0].To != "atrium-87300" ||
		got[0].Text != "green" {
		t.Fatalf("relay got %+v", got)
	}
	if len(owed(t, d)) != 0 {
		t.Fatal("a delivered message was held")
	}
}

// RULE 3. No sender, no cross-room message: it would be typed as the operator.
func TestASayToAnotherRoomNeedsASender(t *testing.T) {
	d, f := roomDaemon(t)
	code, out := say(t, d, map[string]string{"to": "x@claude-sg4", "text": "hi"})
	if code != http.StatusBadRequest || !strings.Contains(out["error"].(string), "operator") {
		t.Fatalf("%d %+v", code, out)
	}
	if len(f.says()) != 0 {
		t.Fatal("relayed an unnamed message")
	}
}

// A bare name, and this room's own name as the room part, stay here, and go
// the way every local message goes: queued from the sender, as a peer.
func TestASayToThisRoomIsLocal(t *testing.T) {
	d, f := roomDaemon(t)
	peerCard(t, d, "sa1")
	target := peerCard(t, d, "sa2")

	for _, to := range []string{"sa2", "@sa2", "sa2@M1MINI"} {
		code, out := say(t, d, map[string]string{"from": "sa1", "to": to, "text": "hi " + to})
		if code != http.StatusOK || out["card"] != target.ID || out["to"] != "sa2" {
			t.Fatalf("say %q: %d %+v", to, code, out)
		}
	}
	msgs := pendingFrom(t, d, target.ID)
	if len(msgs) != 3 || msgs[0].FromPeer != "sa1" {
		t.Fatalf("sa2 has %+v, want three messages from sa1", msgs)
	}
	if len(f.says()) != 0 {
		t.Fatal("a local say went to the hub")
	}
}

// Unreachable is held on this room, the sender is told `held`, and it goes
// when the hub answers.
func TestAnUnreachableRoomIsHeldAndSentLater(t *testing.T) {
	d, f := roomDaemon(t)
	sender := peerCard(t, d, "sa1")
	f.set(func(RelaySay) (RelayResult, error) { return RelayResult{}, ErrRelayDown })

	code, out := say(t, d, map[string]string{"from": "sa1", "to": "orch@claude-sg4", "text": "later"})
	if code != http.StatusOK || out["delivered"] != "held" {
		t.Fatalf("%d %+v", code, out)
	}
	settle(d)
	rows := owed(t, d)
	if len(rows) != 1 || rows[0].FromTask != sender.ID || rows[0].ToRoom != "claude-sg4" || rows[0].Text != "later" ||
		rows[0].Source != store.RelaySourceSay || rows[0].Attempts == 0 {
		t.Fatalf("outbox = %+v, want the message held with a failed try", rows)
	}

	// The hub is back.
	f.set(nil)
	d.drainRelays()
	if len(owed(t, d)) != 0 {
		t.Fatal("the held message was not sent when the hub answered")
	}
	got := f.says()
	if last := got[len(got)-1]; last.To != "orch" || last.Room != "claude-sg4" || last.From != "sa1" {
		t.Fatalf("drain sent %+v", last)
	}
}

// The hub saying the target room is not attached is held the same way.
func TestATargetRoomNotAttachedIsHeld(t *testing.T) {
	d, f := roomDaemon(t)
	peerCard(t, d, "sa1")
	f.set(func(RelaySay) (RelayResult, error) {
		return RelayResult{Unreachable: true, Code: 503, Error: "the room claude-sg4 is not attached"}, nil
	})
	_, out := say(t, d, map[string]string{"from": "sa1", "to": "orch@claude-sg4", "text": "x"})
	if out["delivered"] != "held" {
		t.Fatalf("%+v", out)
	}
	settle(d)
	if len(owed(t, d)) != 1 {
		t.Fatal("not held")
	}
}

// C1. Unconfirmed is said, and not held, so it cannot arrive twice.
func TestAnUnconfirmedSayIsNotHeld(t *testing.T) {
	d, f := roomDaemon(t)
	peerCard(t, d, "sa1")
	f.set(func(RelaySay) (RelayResult, error) { return RelayResult{}, ErrRelayUnconfirmed })
	_, out := say(t, d, map[string]string{"from": "sa1", "to": "orch@claude-sg4", "text": "x"})
	if out["delivered"] != "unconfirmed" {
		t.Fatalf("%+v", out)
	}
	if len(owed(t, d)) != 0 {
		t.Fatal("an unconfirmed message was held, so it could arrive twice")
	}
}

// An old hub is said, and not held, since nothing would ever send it.
func TestAnOldHubIsNotHeldFor(t *testing.T) {
	d, f := roomDaemon(t)
	peerCard(t, d, "sa1")
	f.set(func(RelaySay) (RelayResult, error) { return RelayResult{}, ErrRelayOld })
	code, out := say(t, d, map[string]string{"from": "sa1", "to": "orch@claude-sg4", "text": "x"})
	if code != http.StatusBadGateway || !strings.Contains(out["error"].(string), "older") {
		t.Fatalf("%d %+v", code, out)
	}
	if len(owed(t, d)) != 0 {
		t.Fatal("held for a hub that cannot carry it")
	}
}

// A refusal from the far room comes back with its code and sentence.
func TestARefusalFromTheOtherRoomIsPassedBack(t *testing.T) {
	d, f := roomDaemon(t)
	peerCard(t, d, "sa1")
	f.set(func(RelaySay) (RelayResult, error) {
		return RelayResult{Code: 404, Error: "no session called \"nobody\". these would have worked: orch"}, nil
	})
	code, out := say(t, d, map[string]string{"from": "sa1", "to": "nobody@claude-sg4", "text": "x"})
	if code != http.StatusNotFound || !strings.Contains(out["error"].(string), "orch") {
		t.Fatalf("%d %+v", code, out)
	}
	if len(owed(t, d)) != 0 {
		t.Fatal("a refusal was held")
	}
}

// `atrium tell` takes the same grammar.
func TestTellReachesAnotherRoom(t *testing.T) {
	d, f := roomDaemon(t)
	peerCard(t, d, "sa1")
	raw, _ := json.Marshal(map[string]string{"from": "sa1", "to": "orch@claude-sg4", "text": "hello"})
	rec := httptest.NewRecorder()
	d.handleTell(rec, httptest.NewRequest(http.MethodPost, "/tell", bytes.NewReader(raw)))
	if rec.Code != http.StatusOK || len(f.says()) != 1 {
		t.Fatalf("%d %s, relayed %+v", rec.Code, rec.Body.String(), f.says())
	}
}

// A room with no hub says so rather than pretending.
func TestASayFromADaemonWithNoHubIsRefused(t *testing.T) {
	d := testDaemon(t)
	d.opts.Room = "m1mini"
	peerCard(t, d, "sa1")
	code, out := say(t, d, map[string]string{"from": "sa1", "to": "orch@claude-sg4", "text": "x"})
	if code != http.StatusServiceUnavailable || !strings.Contains(out["error"].(string), "hub") {
		t.Fatalf("%d %+v", code, out)
	}
}

// remoteWorker is a worker launched by `orch` on claude-sg4, card L1 there.
func remoteWorker(t *testing.T, d *Daemon) *store.Task {
	t.Helper()
	w := peerCard(t, d, "worker")
	if err := d.st.SetTags(w.ID, []string{"sdk", OriginAgentTag}); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetLineage(w.ID, "orch@claude-sg4", "claude-sg4~L1"); err != nil {
		t.Fatal(err)
	}
	prompt(t, d, w.ID)
	got, err := d.st.Get(w.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// RULE 5. A silent stop reaches a launcher on another room, addressed to its
// CARD so a later card with the same handle cannot take it (C2).
func TestASilentStopReachesALauncherOnAnotherRoom(t *testing.T) {
	d, f := roomDaemon(t)
	remoteWorker(t, d)
	f.set(func(RelaySay) (RelayResult, error) { return RelayResult{}, ErrRelayDown })

	stopTurn(t, d, "worker")
	settle(d)
	rows := owed(t, d)
	if len(rows) != 1 || rows[0].Source != store.RelaySourceNotice || rows[0].ToCard != "L1" ||
		rows[0].ToName != "orch" || !strings.Contains(rows[0].Text, "without reporting") {
		t.Fatalf("outbox = %+v", rows)
	}
	f.set(nil)
	d.drainRelays()
	got := f.says()
	if last := got[len(got)-1]; last.To != "L1" || last.Room != "claude-sg4" || last.From != "worker" {
		t.Fatalf("the notice went as %+v, want to card L1 on claude-sg4 from worker", last)
	}
	if len(owed(t, d)) != 0 {
		t.Fatal("the notice was not cleared once it went")
	}
	// The same stop again is not a second notice.
	stopTurn(t, d, "worker")
	settle(d)
	if n := len(f.says()); n != len(got) {
		t.Fatalf("a second notice for one stop: %d sends", n)
	}
}

// Item 62. The ledger's `ended` notice reaches a launcher on another room. Its
// arbiter is `claude-sg4~L1`, which is no card here, and the notice was
// logged as having nowhere to go.
func TestAnEndedNoticeReachesALauncherOnAnotherRoom(t *testing.T) {
	d, f := roomDaemon(t)
	w := remoteWorker(t, d)
	f.set(func(RelaySay) (RelayResult, error) { return RelayResult{}, ErrRelayDown })
	if made, err := d.st.CreateWorkItem(w, store.NewWorkItem{Brief: "item 58"}); err != nil || !made {
		t.Fatalf("work item: %v %v", made, err)
	}
	exit := func() {
		t.Helper()
		if err := d.st.AppendEvent(w.ID, store.EventExited, map[string]any{"by": "supervisor", "exit_code": 0}); err != nil {
			t.Fatal(err)
		}
	}
	exit()
	settle(d)
	rows := owed(t, d)
	if len(rows) != 1 || rows[0].Source != store.RelaySourceNotice || rows[0].ToCard != "L1" ||
		rows[0].ToRoom != "claude-sg4" || !strings.Contains(rows[0].Text, "ended without a final report") {
		t.Fatalf("outbox = %+v", rows)
	}
	// The same end seen again is not a second notice.
	exit()
	settle(d)
	if n := len(owed(t, d)); n != 1 {
		t.Fatalf("%d rows owed after the same end twice, want 1", n)
	}
}

// RULE 5. A report to a launcher on another room is held in the same
// transaction as the report, and the worker is told its launcher was told.
func TestAReportReachesALauncherOnAnotherRoom(t *testing.T) {
	d, f := roomDaemon(t)
	w := remoteWorker(t, d)
	f.set(func(RelaySay) (RelayResult, error) { return RelayResult{}, ErrRelayDown })

	rec, out := finishWith(t, d, FinishRequest{Agent: "worker", Status: "progress", Recap: "half way"})
	if rec.Code != http.StatusOK || out["launcher_told"] != true {
		t.Fatalf("%d %+v", rec.Code, out)
	}
	settle(d)
	rows := owed(t, d)
	if len(rows) != 1 || rows[0].FromTask != w.ID || !strings.Contains(rows[0].Text, "half way") {
		t.Fatalf("outbox = %+v", rows)
	}
}

// A worker's own say to its launcher across rooms pays the turn, the way a say
// to a launcher on this room does.
func TestSayingToARemoteLauncherCountsAsReporting(t *testing.T) {
	d, f := roomDaemon(t)
	w := remoteWorker(t, d)
	f.set(func(s RelaySay) (RelayResult, error) {
		return RelayResult{OK: true, Delivered: "queued", To: "orch@claude-sg4", Card: "claude-sg4~L1"}, nil
	})
	if code, out := say(t, d, map[string]string{"from": "worker", "to": "@orch@claude-sg4", "text": "done soon"}); code != 200 {
		t.Fatalf("%d %+v", code, out)
	}
	got, _ := d.st.Get(w.ID)
	if got.OwesReport() {
		t.Fatal("the worker told its launcher and still owes a report")
	}
}

// C1 on the drain. A held say that turns out unconfirmed is dropped with an
// event, never sent twice. A held notice is kept and tried again.
func TestTheDrainDropsAnUnconfirmedSayAndKeepsANotice(t *testing.T) {
	d, f := roomDaemon(t)
	sender := peerCard(t, d, "sa1")
	f.set(func(RelaySay) (RelayResult, error) { return RelayResult{}, ErrRelayDown })
	if _, err := d.holdRelay(sender, "sa1", "orch", "claude-sg4", "", "a say", "", store.RelaySourceSay); err != nil {
		t.Fatal(err)
	}
	if _, err := d.holdRelay(sender, "sa1", "orch", "claude-sg4", "L1", "a notice", "", store.RelaySourceNotice); err != nil {
		t.Fatal(err)
	}
	settle(d)
	f.set(func(RelaySay) (RelayResult, error) { return RelayResult{}, ErrRelayUnconfirmed })
	d.drainRelays()
	rows := owed(t, d)
	if len(rows) != 1 || rows[0].Source != store.RelaySourceNotice {
		t.Fatalf("outbox = %+v, want only the notice kept", rows)
	}
	evs, err := d.st.Events(sender.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range evs {
		if e.Kind == store.EventNotified && strings.Contains(string(e.Payload), "relay-dropped") {
			found = true
		}
	}
	if !found {
		t.Fatal("the dropped say left no event on the sender's card")
	}
}

// Kept a day, then given up on.
func TestAHeldMessageExpires(t *testing.T) {
	d, f := roomDaemon(t)
	sender := peerCard(t, d, "sa1")
	f.set(func(RelaySay) (RelayResult, error) { return RelayResult{}, ErrRelayDown })
	if _, err := d.holdRelay(sender, "sa1", "orch", "claude-sg4", "", "old", "", store.RelaySourceSay); err != nil {
		t.Fatal(err)
	}
	settle(d)
	was := RelayKeep
	RelayKeep = time.Nanosecond
	defer func() { RelayKeep = was }()
	time.Sleep(2 * time.Millisecond)
	d.drainRelays()
	if len(owed(t, d)) != 0 {
		t.Fatal("an expired message was kept")
	}
}

// A launch from a session on another room keeps its tagged card as the
// parent. A bare spawned_by_id cannot point at a card on this room, and a
// same-room launch is resolved here as before.
func TestALaunchFromAnotherRoomKeepsTheTaggedLauncher(t *testing.T) {
	d, _ := roomDaemon(t)
	h := slowHarness(t, d)
	task, err := d.Launch(LaunchRequest{Harness: h, Cwd: t.TempDir(), Tags: []string{OriginAgentTag},
		SpawnedBy: "orch@claude-sg4", SpawnedByID: "claude-sg4~L1"})
	if err != nil {
		t.Skipf("could not spawn a test runner on this machine: %v", err)
	}
	t.Cleanup(func() { _ = d.StopRunner(task.ID) })
	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SpawnedBy != "orch@claude-sg4" || got.SpawnedByID != "claude-sg4~L1" {
		t.Fatalf("lineage %q / %q", got.SpawnedBy, got.SpawnedByID)
	}
	if d.launcherOf(got) != nil {
		t.Fatal("a launcher on another room was found on this one")
	}
	if name, room, ok := d.remoteLauncher(got); !ok || name != "orch" || room != "claude-sg4" {
		t.Fatalf("remote launcher = %q %q %v", name, room, ok)
	}

	local := peerCard(t, d, "here")
	task2, err := d.Launch(LaunchRequest{Harness: h, Cwd: t.TempDir(), Tags: []string{OriginAgentTag},
		SpawnedBy: "here", SpawnedByID: local.ID + "x"})
	if err != nil {
		t.Skipf("could not spawn a test runner on this machine: %v", err)
	}
	t.Cleanup(func() { _ = d.StopRunner(task2.ID) })
	got2, _ := d.st.Get(task2.ID)
	if got2.SpawnedByID != local.ID {
		t.Fatalf("a same-room launch took spawned_by_id %q from the request, want %q resolved here", got2.SpawnedByID, local.ID)
	}
}

// RULE 4, room side.
func TestRoomPeersComeFromTheHub(t *testing.T) {
	d, f := roomDaemon(t)
	f.peers = []RemotePeer{{Handle: "orch@claude-sg4", Room: "claude-sg4", Card: "claude-sg4~L1", Status: "working"}}
	rec := httptest.NewRecorder()
	d.handleRoomPeers(rec, httptest.NewRequest(http.MethodGet, "/v1/peers/rooms", nil))
	var out struct {
		Peers []RemotePeer `json:"peers"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if rec.Code != 200 || len(out.Peers) != 1 || out.Peers[0].Room != "claude-sg4" {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

func roomCard(d *Daemon, to string, events bool) (int, map[string]any) {
	path := "/v1/peers/card?to=" + url.QueryEscape(to)
	if events {
		path += "&events=1"
	}
	rec := httptest.NewRecorder()
	d.handleRoomCard(rec, httptest.NewRequest(http.MethodGet, path, nil))
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func roomExit(d *Daemon, to string) (int, map[string]any) {
	raw, _ := json.Marshal(map[string]string{"to": to})
	rec := httptest.NewRecorder()
	d.handleRoomExit(rec, httptest.NewRequest(http.MethodPost, "/v1/peers/exit", bytes.NewReader(raw)))
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// ITEM 68, ROOM SIDE. A card on another room is read and exited through the
// hub, by `name@room` or `room~id`, and comes back named across.
func TestACardOnAnotherRoomIsReadAndExitedThroughTheHub(t *testing.T) {
	d, f := roomDaemon(t)

	code, out := roomCard(d, "orch@claude-sg4", true)
	task, _ := out["task"].(map[string]any)
	if code != http.StatusOK || task == nil || task["card"] != "claude-sg4~L1" || task["handle"] != "orch@claude-sg4" {
		t.Fatalf("card: %d %+v", code, out)
	}
	code, out = roomExit(d, "claude-sg4~01ABC")
	if code != http.StatusOK || out["asked"] != true || out["card"] != "claude-sg4~L1" {
		t.Fatalf("exit: %d %+v", code, out)
	}
	got := f.reachedAll()
	if len(got) != 2 || got[0] != (RelaySay{Room: "claude-sg4", To: "orch", Text: "card", When: "true"}) ||
		got[1] != (RelaySay{Room: "claude-sg4", To: "01ABC", Text: "exit"}) {
		t.Fatalf("hub asked %+v", got)
	}
}

// This room's own name, and no room at all, are answered as local, and the
// hub is not asked.
func TestACardOnThisRoomIsAnsweredLocal(t *testing.T) {
	d, f := roomDaemon(t)
	for _, to := range []string{"sa1@M1MINI", "m1mini~01ABC", "sa1"} {
		code, out := roomCard(d, to, false)
		if code != http.StatusOK || out["local"] == nil || out["local"] == "" {
			t.Fatalf("card %q: %d %+v", to, code, out)
		}
		if code, out = roomExit(d, to); code != http.StatusOK || out["local"] == nil {
			t.Fatalf("exit %q: %d %+v", to, code, out)
		}
	}
	if len(f.reachedAll()) != 0 {
		t.Fatal("a card on this room went to the hub")
	}
}

// A refusal from the far side is passed back in its words with its code. A hub
// older than this is told apart from one that simply refused.
func TestACardAcrossRoomsPassesRefusalsBack(t *testing.T) {
	d, f := roomDaemon(t)
	f.mu.Lock()
	f.reach = func(RelaySay) (RelayResult, error) {
		return RelayResult{Code: http.StatusNotFound, Error: "no session called \"nobody\". these would have worked: orch"}, nil
	}
	f.mu.Unlock()
	code, out := roomExit(d, "nobody@claude-sg4")
	if code != http.StatusNotFound || !strings.Contains(fmt.Sprint(out["error"]), "orch") {
		t.Fatalf("refusal: %d %+v", code, out)
	}
	f.mu.Lock()
	f.reach = func(RelaySay) (RelayResult, error) {
		return RelayResult{Code: http.StatusBadRequest, Error: "this hub does not know the relay op \"exit\""}, nil
	}
	f.mu.Unlock()
	code, out = roomExit(d, "orch@claude-sg4")
	if code != http.StatusBadGateway || !strings.Contains(fmt.Sprint(out["error"]), "hub is older") {
		t.Fatalf("old hub: %d %+v", code, out)
	}
	f.mu.Lock()
	f.reach = func(RelaySay) (RelayResult, error) { return RelayResult{}, ErrRelayDown }
	f.mu.Unlock()
	if code, _ = roomCard(d, "orch@claude-sg4", false); code != http.StatusBadGateway {
		t.Fatalf("hub down: %d", code)
	}
}

// A room with no hub says so rather than pretending the card is not there.
func TestACardAcrossRoomsWithNoHubIsRefused(t *testing.T) {
	d := testDaemon(t)
	d.opts.Room = "m1mini"
	code, out := roomCard(d, "orch@claude-sg4", false)
	if code != http.StatusServiceUnavailable || !strings.Contains(fmt.Sprint(out["error"]), "not a room linked") {
		t.Fatalf("%d %+v", code, out)
	}
}
