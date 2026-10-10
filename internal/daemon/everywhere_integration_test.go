//go:build integration

package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// found answers Find with the one match the design describes.
func foundSG4(name string) (RelayResult, error) {
	return RelayResult{OK: true, To: "atrium-87300@claude-sg4", Card: "claude-sg4~s1"}, nil
}

// FE1, room side: a bare name that misses here is looked up, and the say goes on
// through the ordinary cross-room path with the room written out.
func TestABareNameThatMissesHereFallsThroughToTheEverywhereCard(t *testing.T) {
	d, f := roomDaemon(t)
	peerCard(t, d, "sa1")
	f.find = foundSG4

	code, out := say(t, d, map[string]string{"from": "sa1", "to": "orchestrator", "text": "hello"})
	if code != http.StatusOK || out["delivered"] != "queued" || out["to"] != "atrium-87300@claude-sg4" {
		t.Fatalf("%d %+v", code, out)
	}
	got := f.says()
	if len(got) != 1 || got[0].Room != "claude-sg4" || got[0].To != "atrium-87300" || got[0].From != "sa1" {
		t.Fatalf("relayed %+v", got)
	}
	if len(f.found) != 1 || f.found[0] != "orchestrator" {
		t.Fatalf("find asked %v", f.found)
	}
}

// FE2: a card here wins, and the hub is not asked.
func TestALocalCardShadowsTheEverywhereOne(t *testing.T) {
	d, f := roomDaemon(t)
	peerCard(t, d, "sa1")
	peerCard(t, d, "orchestrator")
	f.find = foundSG4

	code, out := say(t, d, map[string]string{"from": "sa1", "to": "orchestrator", "text": "hello"})
	if code != http.StatusOK || out["to"] != "orchestrator" {
		t.Fatalf("%d %+v", code, out)
	}
	if len(f.found) != 0 || len(f.says()) != 0 {
		t.Fatalf("crossed rooms: found %v, relayed %+v", f.found, f.says())
	}
}

// FE3: two rooms match, so it is refused naming both and nothing is sent.
func TestTwoEverywhereMatchesAreRefusedAndNothingIsSent(t *testing.T) {
	d, f := roomDaemon(t)
	peerCard(t, d, "sa1")
	f.find = func(string) (RelayResult, error) {
		return RelayResult{Code: http.StatusConflict, Error: `"orchestrator" names a card on more than one room: ` +
			"atrium-87300@claude-sg4 (@orchestrator), atrium-5120@sg3 (@orchestrator). say which, as name@room"}, nil
	}
	code, out := say(t, d, map[string]string{"from": "sa1", "to": "orchestrator", "text": "hello"})
	if code != http.StatusConflict || !strings.Contains(out["error"].(string), "atrium-5120@sg3") {
		t.Fatalf("%d %+v", code, out)
	}
	if len(f.says()) != 0 || len(owed(t, d)) != 0 {
		t.Fatal("something was sent or held")
	}
}

// FE4: the owning room offline. The hub answers find, and the say after it is
// held the way a typed name@room is.
func TestAnEverywhereCardOnAnOfflineRoomIsHeld(t *testing.T) {
	d, f := roomDaemon(t)
	peerCard(t, d, "sa1")
	f.find = foundSG4
	f.set(func(RelaySay) (RelayResult, error) {
		return RelayResult{Unreachable: true, Code: 503, Error: "the room claude-sg4 is not attached"}, nil
	})
	_, out := say(t, d, map[string]string{"from": "sa1", "to": "orchestrator", "text": "x"})
	if out["delivered"] != "held" {
		t.Fatalf("%+v", out)
	}
	settle(d)
	if len(owed(t, d)) != 1 {
		t.Fatal("not held")
	}
}

// FE5: the hub not answering is today's 404 with the note, and nothing held.
func TestAHubThatDoesNotAnswerIsANoteOnTheMiss(t *testing.T) {
	d, f := roomDaemon(t)
	peerCard(t, d, "sa1")
	f.find = func(string) (RelayResult, error) { return RelayResult{}, ErrRelayDown }
	code, out := say(t, d, map[string]string{"from": "sa1", "to": "orchestrator", "text": "x"})
	if code != http.StatusNotFound || !strings.Contains(out["error"].(string), "the hub is not answering, so no card") {
		t.Fatalf("%d %+v", code, out)
	}
	if len(f.says()) != 0 || len(owed(t, d)) != 0 {
		t.Fatal("something was sent or held")
	}
}

// An old hub refuses the op, which is the same 404 with its own note.
func TestAnOldHubIsANoteOnTheMiss(t *testing.T) {
	for name, find := range map[string]func(string) (RelayResult, error){
		"refused the kind": func(string) (RelayResult, error) { return RelayResult{}, ErrRelayOld },
		"unknown op": func(string) (RelayResult, error) {
			return RelayResult{Code: 400, Error: `this hub does not know the relay op "find"`}, nil
		},
	} {
		d, f := roomDaemon(t)
		peerCard(t, d, "sa1")
		f.find = find
		code, out := say(t, d, map[string]string{"from": "sa1", "to": "orchestrator", "text": "x"})
		if code != http.StatusNotFound || !strings.Contains(out["error"].(string), "the hub is older than cards on every room") {
			t.Fatalf("%s: %d %+v", name, code, out)
		}
		if len(owed(t, d)) != 0 {
			t.Fatalf("%s: held", name)
		}
	}
}

// A miss the hub also has no card for carries the hub's list.
func TestAMissCarriesTheHubsEverywhereList(t *testing.T) {
	d, f := roomDaemon(t)
	peerCard(t, d, "sa1")
	f.find = func(string) (RelayResult, error) {
		return RelayResult{Code: 404, Error: `no card called "zzz" on another room. cards on every room: atrium-87300@claude-sg4`}, nil
	}
	code, out := say(t, d, map[string]string{"from": "sa1", "to": "zzz", "text": "x"})
	if code != http.StatusNotFound || !strings.Contains(out["error"].(string), "atrium-87300@claude-sg4") {
		t.Fatalf("%d %+v", code, out)
	}
}

// The tell door falls through the same way.
func TestTellFallsThroughToTheEverywhereCard(t *testing.T) {
	d, f := roomDaemon(t)
	peerCard(t, d, "sa1")
	f.find = foundSG4
	raw, _ := json.Marshal(map[string]string{"from": "sa1", "to": "orchestrator", "text": "hello"})
	rec := httptest.NewRecorder()
	d.handleTell(rec, httptest.NewRequest(http.MethodPost, "/tell", bytes.NewReader(raw)))
	got := f.says()
	if rec.Code != http.StatusOK || len(got) != 1 || got[0].To != "atrium-87300" || got[0].Room != "claude-sg4" {
		t.Fatalf("%d %s, relayed %+v", rec.Code, rec.Body.String(), got)
	}
}

func roomPeers(t *testing.T, d *Daemon, query string) []RemotePeer {
	t.Helper()
	rec := httptest.NewRecorder()
	d.handleRoomPeers(rec, httptest.NewRequest(http.MethodGet, "/v1/peers/rooms"+query, nil))
	var out struct {
		Peers []RemotePeer `json:"peers"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	return out.Peers
}

// FE10. A hub that predates the field answers `peers` with every room's
// sessions and none of them carries `everywhere`. Asked for the everywhere rows,
// the room adds none of them. Today's `rooms:true` answer returns exactly those
// rows, so forgetting this filter would list every remote peer.
func TestAnOldHubsPeersAddNothingToTheLocalList(t *testing.T) {
	d, f := roomDaemon(t)
	f.peers = []RemotePeer{
		{Handle: "a@claude-sg4", Room: "claude-sg4", Card: "claude-sg4~1", Status: "working"},
		{Handle: "b@sg3", Room: "sg3", Card: "sg3~2", Status: "working"},
	}
	if got := roomPeers(t, d, "?everywhere=1"); len(got) != 0 {
		t.Fatalf("an old hub's rows were listed: %+v", got)
	}
	// The same rows are what `rooms` has always shown.
	if got := roomPeers(t, d, ""); len(got) != 2 {
		t.Fatalf("rooms:true lost its rows: %+v", got)
	}
}

// A new hub's rows carry the flag and are kept, the rest dropped.
func TestOnlyTheRowsThatCarryEverywhereAreAppended(t *testing.T) {
	d, f := roomDaemon(t)
	f.peers = []RemotePeer{
		{Handle: "a@claude-sg4", Room: "claude-sg4", Card: "claude-sg4~1", Status: "working", Everywhere: true},
		{Handle: "b@sg3", Room: "sg3", Card: "sg3~2", Status: "working"},
	}
	got := roomPeers(t, d, "?everywhere=1")
	if len(got) != 1 || got[0].Handle != "a@claude-sg4" {
		t.Fatalf("got %+v", got)
	}
}
