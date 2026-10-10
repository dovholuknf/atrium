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
)

func launchSpec() *RelayLaunchSpec {
	return &RelayLaunchSpec{Cwd: "/srv/atrium", Title: "review", Why: "check it", Prompt: "do the review",
		Brief: "the whole brief", Tags: []string{"mine"}, Model: "opus", Env: map[string]string{"A": "b"}}
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

// THE FEATURE. m1mini launches on sg4 through the hub. The session lands on sg4 with the
// lineage `sa1@m1mini` and `m1mini~m1`, so its reports and notices come back across to
// that card, the brief is in the body for sg4 to write on its own disk, and the card comes
// back named across.
func TestARelayedLaunchLandsOnTheTargetWithTheLaunchersLineage(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()

	ans, err := x.miniR.Relay(relayCtx(t), RelayRequest{Op: RelayLaunch, From: "sa1", Room: "sg4", Launch: launchSpec()})
	if err != nil || !ans.OK {
		t.Fatalf("launch = %+v, %v", ans, err)
	}
	if ans.Card != "sg4~kid" || ans.To != "kid@sg4" || ans.Task == nil || ans.Task.Status != "running" ||
		ans.Task.Card != "sg4~kid" || ans.Model != "" {
		t.Fatalf("answer = %+v (task %+v)", ans, ans.Task)
	}
	if !strings.HasSuffix(ans.Watch, "/#term=kid") || ans.Brief != "/srv/atrium/BRIEF.md" {
		t.Fatalf("watch %q, brief %q", ans.Watch, ans.Brief)
	}
	if x.mini.launchBody() != nil {
		t.Fatal("the launch landed on the launcher's own room")
	}
	got := x.sg4.launchBody()
	if got["spawned_by"] != "sa1@m1mini" || got["spawned_by_id"] != "m1mini~m1" {
		t.Fatalf("lineage = %v / %v", got["spawned_by"], got["spawned_by_id"])
	}
	if got["brief"] != "the whole brief" || got["cwd"] != "/srv/atrium" || got["model"] != "opus" ||
		got["lean"] != true || got["harness"] != "claude" {
		t.Fatalf("sg4 was sent %+v", got)
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
	ans, err := x.miniR.Relay(relayCtx(t), RelayRequest{Op: RelayLaunch, From: "sa1", Room: "sg4", Launch: spec})
	if err != nil || !ans.OK {
		t.Fatalf("relay = %+v, %v", ans, err)
	}
	viaRelay := x.sg4.launchBody()
	x.sg4.mu.Lock()
	x.sg4.launched = nil
	x.sg4.mu.Unlock()

	_, out, err := x.control.launchHandler(relayCtx(t), ctlReq("sa1", "m1mini"), launchInput{
		Cwd: spec.Cwd, Title: spec.Title, Why: spec.Why, Prompt: spec.Prompt, Brief: spec.Brief, Tags: spec.Tags,
		Model: spec.Model, Env: spec.Env, Room: "sg4"})
	if err != nil {
		t.Fatalf("hub launch: %v", err)
	}
	if direct := x.sg4.launchBody(); !reflect.DeepEqual(viaRelay, direct) {
		t.Fatalf("relay sent %+v, the hub's own launch sent %+v", viaRelay, direct)
	}
	if out.Card != ans.Card || out.Handle != ans.To || out.Brief != ans.Brief || out.Watch != ans.Watch {
		t.Fatalf("hub says %+v, relay says %+v", out, ans)
	}
}

// THE LAUNCHING ROOM COMES FROM THE CONNECTION. sg4 launching on m1mini is `orch@sg4`,
// found on sg4's own list, whatever the request claims.
func TestARelayedLaunchNamesTheLauncherOnTheRoomItCameFrom(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()

	ans, err := x.sg4R.Relay(relayCtx(t), RelayRequest{Op: RelayLaunch, From: "orch", Room: "M1MINI", Launch: launchSpec()})
	if err != nil || !ans.OK || ans.Card != "M1MINI~kid" {
		t.Fatalf("launch = %+v, %v", ans, err)
	}
	got := x.mini.launchBody()
	if got["spawned_by"] != "orch@sg4" || got["spawned_by_id"] != "sg4~s1" {
		t.Fatalf("lineage = %v / %v", got["spawned_by"], got["spawned_by_id"])
	}
	if x.sg4.launchBody() != nil {
		t.Fatal("the launch landed on the launcher's own room")
	}
}

// Its own room, no directory, and a room nobody knows are each refused before anything is posted.
// The unknown room's refusal lists the rooms the hub does know.
func TestARelayedLaunchRefusesItsOwnRoomNoDirectoryAndAnUnknownRoom(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()

	ans, _ := x.miniR.Relay(relayCtx(t), RelayRequest{Op: RelayLaunch, From: "sa1", Room: "M1Mini", Launch: launchSpec()})
	if ans.OK || ans.Code != http.StatusBadRequest {
		t.Fatalf("own room = %+v", ans)
	}
	ans, _ = x.miniR.Relay(relayCtx(t), RelayRequest{Op: RelayLaunch, From: "sa1", Room: "sg4",
		Launch: &RelayLaunchSpec{Brief: "b"}})
	if ans.OK || ans.Code != http.StatusBadRequest || !strings.Contains(ans.Error, "where to run it") {
		t.Fatalf("no cwd = %+v", ans)
	}
	ans, _ = x.miniR.Relay(relayCtx(t), RelayRequest{Op: RelayLaunch, From: "sa1", Room: "sg4"})
	if ans.OK || ans.Code != http.StatusBadRequest {
		t.Fatalf("no launch = %+v", ans)
	}
	ans, _ = x.miniR.Relay(relayCtx(t), RelayRequest{Op: RelayLaunch, From: "sa1", Room: "atlantis", Launch: launchSpec()})
	if ans.OK || ans.Unreachable || ans.Code != http.StatusNotFound || !strings.Contains(ans.Error, `"atlantis"`) ||
		!strings.Contains(ans.Error, "m1mini") || !strings.Contains(ans.Error, "sg4") {
		t.Fatalf("unknown room = %+v, want it to name atlantis and list the rooms", ans)
	}
	if x.mini.launchBody() != nil || x.sg4.launchBody() != nil {
		t.Fatal("a refused launch reached a room")
	}
}

// THE CAP APPLIES TO THE TARGET ROOM and to nothing else. sg4 is full and m1mini has room: a
// launch onto sg4 is refused naming sg4 and never posted, one onto m1mini goes, and the refusal is
// the hub's own sentence.
func TestARelayedLaunchIsCappedPerTargetRoom(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	two := func(room string) int {
		if room == "sg4" {
			return 2
		}
		return 10
	}
	x.proxy.mu.Lock()
	x.proxy.ctl.capFor = two
	x.proxy.mu.Unlock()
	x.sg4.mu.Lock()
	for _, id := range []string{"w1", "w2"} {
		x.sg4.tasks = append(x.sg4.tasks, map[string]any{"id": id, "wire_name": id, "status": "working",
			"supervised": true, "tags": []string{OriginTag, SubagentTag}})
	}
	x.sg4.mu.Unlock()

	ans, err := x.miniR.Relay(relayCtx(t), RelayRequest{Op: RelayLaunch, From: "sa1", Room: "sg4", Launch: launchSpec()})
	if err != nil || ans.OK || ans.Unreachable || ans.Unconfirmed || ans.Code != http.StatusConflict ||
		!strings.Contains(ans.Error, "at the launch cap of 2 running workers on room sg4") {
		t.Fatalf("over the cap = %+v, %v", ans, err)
	}
	if x.sg4.launchBody() != nil {
		t.Fatal("a launch over the cap was posted to the room")
	}
	// The other direction, to a room with room to spare: sg4 being full is no matter to it.
	ans, err = x.sg4R.Relay(relayCtx(t), RelayRequest{Op: RelayLaunch, From: "orch", Room: "m1mini", Launch: launchSpec()})
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
		x.sg4.mu.Lock()
		x.sg4.launchCode = code
		x.sg4.mu.Unlock()
		ans, err := x.miniR.Relay(relayCtx(t), RelayRequest{Op: RelayLaunch, From: "sa1", Room: "sg4", Launch: launchSpec()})
		if err != nil || ans.OK || ans.Unreachable || !ans.Unconfirmed || ans.Code != code {
			t.Fatalf("%d = %+v, %v", code, ans, err)
		}
	}
	x.sg4.mu.Lock()
	x.sg4.launchCode = http.StatusBadRequest
	x.sg4.mu.Unlock()
	ans, _ := x.miniR.Relay(relayCtx(t), RelayRequest{Op: RelayLaunch, From: "sa1", Room: "sg4", Launch: launchSpec()})
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
			_, _ = w.Write([]byte(`{"durable":true,"rooms":[{"name":"sg3"},{"name":"sg4"}]}`))
		case r.Method == http.MethodPost:
			posts++
		}
	}))
	defer srv.Close()
	hub := NewHub(Timings{Beat: 200 * time.Millisecond, Silence: time.Second, Warm: 1})
	c := newControl(srv.URL, hub, nil)

	ans := c.relay(relayCtx(t), "m1mini", RelayRequest{Op: RelayLaunch, From: "sa1", Room: "SG3", Launch: launchSpec()})
	if ans.OK || !ans.Unreachable || ans.Code != http.StatusServiceUnavailable || !strings.Contains(ans.Error, "SG3") ||
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

	if ans, err := x.miniR.Relay(relayCtx(t), RelayRequest{Op: RelayLaunch, From: "sa1", Room: "sg4", Launch: launchSpec()}); err != nil || !ans.OK {
		t.Fatalf("launch = %+v, %v", ans, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(details) != 1 || rooms[0] != "sg4" || kinds[0] != "ctl-launch" ||
		details[0] != "by sa1@m1mini (claimed): launch claude as sg4~kid, ok" {
		t.Fatalf("audit = %v %v %v", rooms, kinds, details)
	}
	if strings.Contains(details[0], "whole brief") || strings.Contains(details[0], "do the review") {
		t.Fatal("the audit line carries a payload")
	}
}
