//go:build integration

package link

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/link/wire"
)

func launchSpec() *wire.RelayLaunchSpec {
	return &wire.RelayLaunchSpec{Cwd: "/srv/atrium", Title: "review", Why: "check it", Prompt: "do the review",
		Brief: "the whole brief", Tags: []string{"mine", "atrium:item:f-test"}, Model: "opus", Env: map[string]string{"A": "b"}}
}

// copyOf is a room's last launch body, copied out from under its lock.
func (f *relayRoom) launchBody() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.launched == nil {
		return nil
	}
	raw, _ := json.Marshal(f.launched)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}

// THE FEATURE. room-a launches on room-e through the hub. The session lands on room-e with the
// lineage `sa1@room-a` and `room-a~m1`, so its reports and notices come back across to
// that card, the brief is in the body for room-e to write on its own disk, and the card comes
// back named across.
func TestARelayedLaunchLandsOnTheTargetWithTheLaunchersLineage(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()

	ans, err := x.miniR.Relay(relayCtx(t), wire.RelayRequest{Op: RelayLaunch, From: "sa1", Room: "room-e", Launch: launchSpec()})
	if err != nil || !ans.OK {
		t.Fatalf("launch = %+v, %v", ans, err)
	}
	if ans.Card != "room-e~kid" || ans.To != "kid@room-e" || ans.Task == nil || ans.Task.Status != "running" ||
		ans.Task.Card != "room-e~kid" || ans.Model != "" {
		t.Fatalf("answer = %+v (task %+v)", ans, ans.Task)
	}
	if !strings.HasSuffix(ans.Watch, "/room-e/terminal/kid") || ans.Brief != "/srv/atrium/BRIEF.md" {
		t.Fatalf("watch %q, brief %q", ans.Watch, ans.Brief)
	}
	if x.mini.launchBody() != nil {
		t.Fatal("the launch landed on the launcher's own room")
	}
	got := x.roomE.launchBody()
	if got["spawned_by"] != "sa1@room-a" || got["spawned_by_id"] != "room-a~m1" {
		t.Fatalf("lineage = %v / %v", got["spawned_by"], got["spawned_by_id"])
	}
	if got["brief"] != "the whole brief" || got["cwd"] != "/srv/atrium" || got["model"] != "opus" ||
		got["lean"] != true || got["harness"] != "" {
		t.Fatalf("room-e was sent %+v", got)
	}
	if p, _ := got["prompt"].(string); !strings.HasPrefix(p, "do the review") || !strings.HasSuffix(p, reportLine) {
		t.Fatalf("prompt = %q", p)
	}
	tags, _ := got["tags"].([]any)
	if !hasTag(toStrings(tags), "mine") || !hasTag(toStrings(tags), OriginTag) || !hasTag(toStrings(tags), SubagentTag) {
		t.Fatalf("tags = %v", tags)
	}
	if env, _ := got["env"].(map[string]any); env["A"] != "b" {
		t.Fatalf("env = %v", got["env"])
	}
}

func toStrings(in []any) []string {
	var out []string
	for _, v := range in {
		out = append(out, v.(string))
	}
	return out
}

// ONE LAUNCH, NOT TWO. The body the target gets from a relayed launch is, field for field,
// the one it gets from the hub's own atrium_launch with `room`, and the answer names the
// card the same way. A fork of the hub's handler would drift from this.
func TestARelayedLaunchIsTheHubsOwnLaunchByAnotherDoor(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()

	spec := launchSpec()
	ans, err := x.miniR.Relay(relayCtx(t), wire.RelayRequest{Op: RelayLaunch, From: "sa1", Room: "room-e", Launch: spec})
	if err != nil || !ans.OK {
		t.Fatalf("relay = %+v, %v", ans, err)
	}
	viaRelay := x.roomE.launchBody()
	x.roomE.mu.Lock()
	x.roomE.launched = nil
	x.roomE.mu.Unlock()

	_, out, err := x.control.launchHandler(relayCtx(t), ctlReq("sa1", "room-a"), launchInput{
		Cwd: spec.Cwd, Title: spec.Title, Why: spec.Why, Prompt: spec.Prompt, Brief: spec.Brief, Tags: spec.Tags,
		Model: spec.Model, Env: spec.Env, Room: "room-e"})
	if err != nil {
		t.Fatalf("hub launch: %v", err)
	}
	direct := x.roomE.launchBody()
	// Each launch makes its own random key.
	delete(viaRelay, "launch_key")
	delete(direct, "launch_key")
	if !reflect.DeepEqual(viaRelay, direct) {
		t.Fatalf("relay sent %+v, the hub's own launch sent %+v", viaRelay, direct)
	}
	if out.Card != ans.Card || out.Handle != ans.To || out.Brief != ans.Brief || out.Watch != ans.Watch {
		t.Fatalf("hub says %+v, relay says %+v", out, ans)
	}
}

// A relayed launch keeps repo, home, scratch and every other launch field, and sends room-e what the hub's own launch
// with `room` sends. A spec with no cwd is fine when it names repo or home.
func TestARelayedLaunchKeepsRepoHomeScratchAndTheRest(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()

	off := false
	spec := &wire.RelayLaunchSpec{Repo: "github.com/o/r", Branch: "claude/x", Base: "claude/main", Title: "t",
		Tags: []string{"atrium:item:f-test"}, Resident: true, Scratch: true, Lean: &off, MCP: []string{"m"}, Theme: "dusk",
		Chat: true, Item: true, ItemOwner: "human"}
	ans, err := x.miniR.Relay(relayCtx(t), wire.RelayRequest{Op: RelayLaunch, From: "sa1", Room: "room-e", Launch: spec})
	if err != nil || !ans.OK || !ans.Carried {
		t.Fatalf("relay = %+v, %v", ans, err)
	}
	viaRelay := x.roomE.launchBody()
	if viaRelay["repo"] != "github.com/o/r" || viaRelay["branch"] != "claude/x" || viaRelay["base"] != "claude/main" ||
		viaRelay["scratch"] != true || viaRelay["lean"] != false || viaRelay["theme"] != "dusk" || viaRelay["chat"] != true {
		t.Fatalf("room-e was sent %+v", viaRelay)
	}
	tags := toStrings(viaRelay["tags"].([]any))
	if !hasTag(tags, ResidentTag) || !hasTag(tags, ItemOwnerTag+"human") || !hasTag(tags, ItemTag) {
		t.Fatalf("tags = %v", tags)
	}
	x.roomE.mu.Lock()
	x.roomE.launched = nil
	x.roomE.mu.Unlock()
	if _, _, err := x.control.launchHandler(relayCtx(t), ctlReq("sa1", "room-a"), launchInput{Repo: spec.Repo,
		Branch: spec.Branch, Base: spec.Base, Title: spec.Title, Tags: spec.Tags, Resident: true, Scratch: true, Lean: &off,
		MCP: spec.MCP, Theme: spec.Theme, Chat: true, Isolated: true, ItemOwner: "human", Room: "room-e"}); err != nil {
		t.Fatalf("hub launch: %v", err)
	}
	direct := x.roomE.launchBody()
	delete(viaRelay, "launch_key")
	delete(direct, "launch_key")
	if !reflect.DeepEqual(viaRelay, direct) {
		t.Fatalf("relay sent %+v, the hub's own launch sent %+v", viaRelay, direct)
	}

	ans, err = x.miniR.Relay(relayCtx(t), wire.RelayRequest{Op: RelayLaunch, From: "sa1", Room: "room-e",
		Launch: &wire.RelayLaunchSpec{Home: true, Tags: []string{"atrium:item:f-test"}}})
	if err != nil || !ans.OK || x.roomE.launchBody()["home"] != true {
		t.Fatalf("home = %+v, %v, room-e got %+v", ans, err, x.roomE.launchBody())
	}
	ans, _ = x.miniR.Relay(relayCtx(t), wire.RelayRequest{Op: RelayLaunch, From: "sa1", Room: "room-e",
		Launch: &wire.RelayLaunchSpec{Cwd: "/w", Repo: "github.com/o/r"}})
	if ans.OK || ans.Code != http.StatusBadRequest || !strings.Contains(ans.Error, "not both") {
		t.Fatalf("cwd and repo = %+v", ans)
	}
}

// THE LAUNCHING ROOM COMES FROM THE CONNECTION. room-e launching on room-a is `orch@room-e`,
// found on room-e's own list, whatever the request claims.
func TestARelayedLaunchNamesTheLauncherOnTheRoomItCameFrom(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()

	ans, err := x.roomER.Relay(relayCtx(t), wire.RelayRequest{Op: RelayLaunch, From: "orch", Room: "ROOM-A", Launch: launchSpec()})
	if err != nil || !ans.OK || ans.Card != "ROOM-A~kid" {
		t.Fatalf("launch = %+v, %v", ans, err)
	}
	got := x.mini.launchBody()
	if got["spawned_by"] != "orch@room-e" || got["spawned_by_id"] != "room-e~s1" {
		t.Fatalf("lineage = %v / %v", got["spawned_by"], got["spawned_by_id"])
	}
	if x.roomE.launchBody() != nil {
		t.Fatal("the launch landed on the launcher's own room")
	}
}

// Its own room, no directory, and a room nobody knows are each refused before anything is posted.
// The unknown room's refusal lists the rooms the hub does know.
func TestARelayedLaunchRefusesItsOwnRoomNoDirectoryAndAnUnknownRoom(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()

	ans, _ := x.miniR.Relay(relayCtx(t), wire.RelayRequest{Op: RelayLaunch, From: "sa1", Room: "Room-a", Launch: launchSpec()})
	if ans.OK || ans.Code != http.StatusBadRequest {
		t.Fatalf("own room = %+v", ans)
	}
	ans, _ = x.miniR.Relay(relayCtx(t), wire.RelayRequest{Op: RelayLaunch, From: "sa1", Room: "room-e",
		Launch: &wire.RelayLaunchSpec{Brief: "b"}})
	if ans.OK || ans.Code != http.StatusBadRequest || !strings.Contains(ans.Error, "where to run it") {
		t.Fatalf("no cwd = %+v", ans)
	}
	ans, _ = x.miniR.Relay(relayCtx(t), wire.RelayRequest{Op: RelayLaunch, From: "sa1", Room: "room-e"})
	if ans.OK || ans.Code != http.StatusBadRequest {
		t.Fatalf("no launch = %+v", ans)
	}
	ans, _ = x.miniR.Relay(relayCtx(t), wire.RelayRequest{Op: RelayLaunch, From: "sa1", Room: "atlantis", Launch: launchSpec()})
	if ans.OK || ans.Unreachable || ans.Code != http.StatusNotFound || !strings.Contains(ans.Error, `"atlantis"`) ||
		!strings.Contains(ans.Error, "room-a") || !strings.Contains(ans.Error, "room-e") {
		t.Fatalf("unknown room = %+v, want it to name atlantis and list the rooms", ans)
	}
	if x.mini.launchBody() != nil || x.roomE.launchBody() != nil {
		t.Fatal("a refused launch reached a room")
	}
}

// THE CAP APPLIES TO THE TARGET ROOM and to nothing else. room-e is full and room-a has room: a
// launch onto room-e is refused naming room-e and never posted, one onto room-a goes, and the refusal is
// the hub's own sentence.
func TestARelayedLaunchIsCappedPerTargetRoom(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	two := func(room string) int {
		if room == "room-e" {
			return 2
		}
		return 10
	}
	x.proxy.mu.Lock()
	x.proxy.ctl.capFor = two
	x.proxy.mu.Unlock()
	x.roomE.mu.Lock()
	for _, id := range []string{"w1", "w2"} {
		x.roomE.tasks = append(x.roomE.tasks, map[string]any{"id": id, "wire_name": id, "status": "running",
			"supervised": true, "tags": []string{OriginTag, SubagentTag}})
	}
	x.roomE.mu.Unlock()

	ans, err := x.miniR.Relay(relayCtx(t), wire.RelayRequest{Op: RelayLaunch, From: "sa1", Room: "room-e", Launch: launchSpec()})
	if err != nil || ans.OK || ans.Unreachable || ans.Unconfirmed || ans.Code != http.StatusConflict ||
		!strings.Contains(ans.Error, "at the launch cap of 2 running workers on room room-e") {
		t.Fatalf("over the cap = %+v, %v", ans, err)
	}
	if x.roomE.launchBody() != nil {
		t.Fatal("a launch over the cap was posted to the room")
	}
	// The other direction, to a room with room to spare: room-e being full is no matter to it.
	ans, err = x.roomER.Relay(relayCtx(t), wire.RelayRequest{Op: RelayLaunch, From: "orch", Room: "room-a", Launch: launchSpec()})
	if err != nil || !ans.OK {
		t.Fatalf("under the cap = %+v, %v", ans, err)
	}
}

// A post that fails after it may have started the session is unconfirmed, never unreachable,
// so nothing launches it again. A room that answers a refusal gives it back in its own words.
func TestARelayedLaunchThatFailsAfterThePostIsUnconfirmed(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()

	for _, code := range []int{http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
		x.roomE.mu.Lock()
		x.roomE.launchCode = code
		x.roomE.mu.Unlock()
		ans, err := x.miniR.Relay(relayCtx(t), wire.RelayRequest{Op: RelayLaunch, From: "sa1", Room: "room-e", Launch: launchSpec()})
		if err != nil || ans.OK || ans.Unreachable || !ans.Unconfirmed || ans.Code != code {
			t.Fatalf("%d = %+v, %v", code, ans, err)
		}
	}
	x.roomE.mu.Lock()
	x.roomE.launchCode = http.StatusBadRequest
	x.roomE.mu.Unlock()
	ans, _ := x.miniR.Relay(relayCtx(t), wire.RelayRequest{Op: RelayLaunch, From: "sa1", Room: "room-e", Launch: launchSpec()})
	if ans.OK || ans.Unconfirmed || ans.Unreachable || ans.Code != http.StatusBadRequest ||
		!strings.Contains(ans.Error, "the room fell over") {
		t.Fatalf("a refusal = %+v", ans)
	}
}

// A room the hub knows and is not holding is not answering: unreachable, and nothing is posted.
func TestARelayedLaunchOnAKnownRoomThatIsDownIsUnreachable(t *testing.T) {
	var mu sync.Mutex
	var posts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/_hub/inventory":
			_, _ = w.Write([]byte(`{"durable":true,"rooms":[{"name":"room-d"},{"name":"room-e"}]}`))
		case r.Method == http.MethodPost:
			posts++
		}
	}))
	defer srv.Close()
	hub := NewHub(Timings{Beat: 200 * time.Millisecond, Silence: time.Second, Warm: 1})
	c := newControl(srv.URL, hub, nil)

	ans := c.Relay(relayCtx(t), "room-a", wire.RelayRequest{Op: RelayLaunch, From: "sa1", Room: "ROOM-D", Launch: launchSpec()})
	if ans.OK || !ans.Unreachable || ans.Code != http.StatusServiceUnavailable || !strings.Contains(ans.Error, "ROOM-D") ||
		!strings.Contains(ans.Error, "not attached") {
		t.Fatalf("down room = %+v", ans)
	}
	mu.Lock()
	defer mu.Unlock()
	if posts != 0 {
		t.Fatalf("%d posts reached the board for a room that is down", posts)
	}
}

// What the hub-side audit log says: the target room, the launcher on the asking room and the
// card, never a prompt or a brief.
func TestARelayedLaunchIsAuditedOnTheTargetWithNoPayload(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	var mu sync.Mutex
	var rooms, kinds, details []string
	x.proxy.mu.Lock()
	x.proxy.ctl.audit = func(room, kind, detail string) {
		mu.Lock()
		defer mu.Unlock()
		rooms, kinds, details = append(rooms, room), append(kinds, kind), append(details, detail)
	}
	x.proxy.mu.Unlock()

	if ans, err := x.miniR.Relay(relayCtx(t), wire.RelayRequest{Op: RelayLaunch, From: "sa1", Room: "room-e", Launch: launchSpec()}); err != nil || !ans.OK {
		t.Fatalf("launch = %+v, %v", ans, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(details) != 1 || rooms[0] != "room-e" || kinds[0] != "ctl-launch" ||
		details[0] != "by sa1@room-a (claimed): launch as room-e~kid, ok" {
		t.Fatalf("audit = %v %v %v", rooms, kinds, details)
	}
	if strings.Contains(details[0], "whole brief") || strings.Contains(details[0], "do the review") {
		t.Fatal("the audit line carries a payload")
	}
}
