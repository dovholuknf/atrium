package link

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// FE1, the hub's half: a room asks `find` and is told the one card.
func TestFindAnswersTheOneCardOnAnotherRoom(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	x.hub.IndexEverywhere("sg4", []CardState{everyRow("s1", "running", "atrium-87300", "orchestrator", EverywhereTag)})

	ans, err := x.miniR.Relay(relayCtx(t), RelayRequest{Op: RelayFind, To: "@Orchestrator"})
	if err != nil || !ans.OK {
		t.Fatalf("find = %+v, %v", ans, err)
	}
	if ans.To != "atrium-87300@sg4" || ans.Card != "sg4~s1" {
		t.Fatalf("find answered to=%q card=%q", ans.To, ans.Card)
	}
}

// The asking room's own card is never an answer, and a card id never is either.
func TestFindExcludesTheAskingRoomAndNeverMatchesAnId(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	x.hub.IndexEverywhere("sg4", []CardState{everyRow("s1", "running", "atrium-87300", "orchestrator", EverywhereTag)})

	ans, err := x.sg4R.Relay(relayCtx(t), RelayRequest{Op: RelayFind, To: "orchestrator"})
	if err != nil || ans.OK || ans.Code != http.StatusNotFound {
		t.Fatalf("find from its own room = %+v, %v", ans, err)
	}
	ans, _ = x.miniR.Relay(relayCtx(t), RelayRequest{Op: RelayFind, To: "s1"})
	if ans.OK || ans.Code != http.StatusNotFound {
		t.Fatalf("find by card id = %+v", ans)
	}
}

// FE3, the hub's half: two rooms, one alias, a 409 naming both.
func TestFindRefusesWhenTwoRoomsMatchAndNamesBoth(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	x.hub.IndexEverywhere("sg4", []CardState{everyRow("s1", "running", "atrium-87300", "orchestrator", EverywhereTag)})
	x.hub.IndexEverywhere("sg3", []CardState{everyRow("t1", "running", "atrium-5120", "orchestrator", EverywhereTag)})

	ans, err := x.miniR.Relay(relayCtx(t), RelayRequest{Op: RelayFind, To: "orchestrator"})
	if err != nil || ans.OK || ans.Code != http.StatusConflict {
		t.Fatalf("find = %+v, %v", ans, err)
	}
	for _, want := range []string{"atrium-87300@sg4 (@orchestrator)", "atrium-5120@sg3 (@orchestrator)"} {
		if !strings.Contains(ans.Error, want) {
			t.Errorf("the 409 does not name %q: %s", want, ans.Error)
		}
	}
}

// A miss is a 404 that lists what is on every room, as handle@room.
func TestFindMissCarriesTheEverywhereList(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	x.hub.IndexEverywhere("sg4", []CardState{everyRow("s1", "running", "atrium-87300", "orchestrator", EverywhereTag)})

	ans, err := x.miniR.Relay(relayCtx(t), RelayRequest{Op: RelayFind, To: "nobody"})
	if err != nil || ans.OK || ans.Code != http.StatusNotFound {
		t.Fatalf("find = %+v, %v", ans, err)
	}
	if !strings.Contains(ans.Error, "atrium-87300@sg4") {
		t.Fatalf("the 404 does not list the everywhere cards: %s", ans.Error)
	}
}

// indexSG4 tags sg4's orchestrator, which the fake room already holds as s1.
func indexSG4(x *relayPair) {
	x.hub.IndexEverywhere("sg4", []CardState{everyRow("s1", "running", "atrium-87300", "orchestrator", EverywhereTag)})
}

// FE1, hub-side entry: a bare name that misses locally goes on as handle@room,
// through the sender's own room.
func TestHubSideSayFallsThroughToTheEverywhereCard(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	indexSG4(x)

	_, out, err := x.control.sayHandler(relayCtx(t), ctlReq("sa1", "m1mini"),
		sayInput{To: "orchestrator", Text: "hello"})
	if err != nil {
		t.Fatalf("say: %v", err)
	}
	said := x.mini.says()
	if len(said) != 1 || said[0]["to"] != "atrium-87300@sg4" || said[0]["from"] != "sa1" {
		t.Fatalf("m1mini's /v1/say got %+v", said)
	}
	if out.Delivered != "queued" {
		t.Fatalf("out = %+v", out)
	}
}

// FE2: a card on the caller's own room answers first, and nothing crosses.
func TestHubSideLocalCardShadowsTheEverywhereOne(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	x.hub.IndexEverywhere("sg4", []CardState{everyRow("s1", "running", "atrium-87300", "sa1", EverywhereTag)})

	if _, _, err := x.control.sayHandler(relayCtx(t), ctlReq("other", "m1mini"),
		sayInput{To: "sa1", Text: "hello"}); err != nil {
		t.Fatalf("say: %v", err)
	}
	if len(x.mini.says()) != 0 || len(x.mini.messages()) != 1 {
		t.Fatalf("the local card was not used: says=%+v messages=%+v", x.mini.says(), x.mini.messages())
	}
}

// FE3: two rooms match, so nothing is sent and both are named.
func TestHubSideSayRefusesTwoEverywhereMatches(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	indexSG4(x)
	x.hub.IndexEverywhere("sg3", []CardState{everyRow("t1", "running", "atrium-5120", "orchestrator", EverywhereTag)})

	_, _, err := x.control.sayHandler(relayCtx(t), ctlReq("sa1", "m1mini"), sayInput{To: "orchestrator", Text: "x"})
	if err == nil || !strings.Contains(err.Error(), "atrium-87300@sg4 (@orchestrator)") ||
		!strings.Contains(err.Error(), "atrium-5120@sg3 (@orchestrator)") {
		t.Fatalf("err = %v", err)
	}
	if len(x.mini.says()) != 0 {
		t.Fatal("something was sent")
	}
}

// A miss with nothing matching keeps the local sentence and adds the list.
func TestHubSideSayMissListsTheEverywhereCards(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	indexSG4(x)

	_, _, err := x.control.sayHandler(relayCtx(t), ctlReq("sa1", "m1mini"), sayInput{To: "nobody", Text: "x"})
	if err == nil || !strings.Contains(err.Error(), `no session called "nobody"`) ||
		!strings.Contains(err.Error(), "atrium-87300@sg4") {
		t.Fatalf("err = %v", err)
	}
}

// atrium_task reads the everywhere card, named across.
func TestHubSideTaskFallsThroughToTheEverywhereCard(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	indexSG4(x)

	_, out, err := x.control.taskHandler(relayCtx(t), ctlReq("sa1", "m1mini"), taskInput{Card: "orchestrator"})
	if err != nil {
		t.Fatalf("task: %v", err)
	}
	if out.Card != "sg4~s1" || out.Handle != "atrium-87300@sg4" || out.Status != "needs-input" {
		t.Fatalf("out = %+v", out)
	}
}

// FE6: exit, cull and alias do not fall through.
func TestHubSideExitDoesNotFallThrough(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	indexSG4(x)

	_, _, err := x.control.exitHandler(relayCtx(t), ctlReq("sa1", "m1mini"), exitInput{Card: "orchestrator"})
	if err == nil {
		t.Fatal("a bare name reached another room's card to end it")
	}
	if len(x.sg4.exits()) != 0 {
		t.Fatalf("sg4 was asked to exit: %v", x.sg4.exits())
	}
	if _, _, err := x.control.exitHandler(relayCtx(t), ctlReq("sa1", "m1mini"),
		exitInput{Card: "atrium-87300@sg4"}); err != nil {
		t.Fatalf("name@room stopped working: %v", err)
	}
}

// atrium_peers without rooms lists the everywhere cards after the local ones.
func TestHubSidePeersAppendTheEverywhereCards(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	indexSG4(x)

	_, out, err := x.control.peersHandler(relayCtx(t), ctlReq("other", "m1mini"), peersInput{})
	if err != nil {
		t.Fatal(err)
	}
	last := out.Peers[len(out.Peers)-1]
	if last.Handle != "atrium-87300@sg4" || last.Alias != "orchestrator" || last.Card != "sg4~s1" ||
		last.Room != "sg4" || !last.Everywhere {
		t.Fatalf("peers = %+v", out.Peers)
	}
	for _, p := range out.Peers[:len(out.Peers)-1] {
		if p.Everywhere {
			t.Fatalf("a local row is marked everywhere: %+v", p)
		}
	}
}

// The relay `peers` op with everywhere set answers only those rows.
func TestRelayPeersEverywhereAnswersOnlyTheTaggedCards(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	indexSG4(x)

	ans, err := x.miniR.Relay(relayCtx(t), RelayRequest{Op: RelayPeers, Everywhere: true})
	if err != nil || !ans.OK || len(ans.Peers) != 1 || !ans.Peers[0].Everywhere ||
		ans.Peers[0].Handle != "atrium-87300@sg4" || ans.Peers[0].Alias != "orchestrator" ||
		ans.Peers[0].Card != "sg4~s1" {
		t.Fatalf("peers = %+v, %v", ans, err)
	}
}

// A room removed from the store keeps no place in the index. The rooms CLI does
// that in another process, so the index is checked against what the store still
// holds, and a forget through the hub drops it at once.
func TestARoomTheStoreNoLongerHoldsLeavesTheIndex(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	indexSG4(x)
	stock := &remembering{cards: map[string][]CardState{"sg4": {card("s1", "work", "running")}}}
	x.proxy.SetInventory(stock)

	if got := x.hub.every.find("m1mini", "orchestrator"); len(got) != 1 {
		t.Fatalf("while held: %+v", got)
	}
	delete(stock.cards, "sg4") // what store.Remove or Force cascades
	if got := x.hub.every.find("m1mini", "orchestrator"); len(got) != 0 {
		t.Fatalf("after the store dropped the room: %+v", got)
	}
	ans, _ := x.miniR.Relay(relayCtx(t), RelayRequest{Op: RelayFind, To: "orchestrator"})
	if ans.OK || ans.Code != http.StatusNotFound {
		t.Fatalf("find answered with a room the hub no longer has: %+v", ans)
	}
}

func TestForgettingARoomDropsItFromTheIndex(t *testing.T) {
	seen := time.Now().Add(-time.Hour)
	stock := &remembering{
		rooms: []Known{{Name: "athens", FirstSeen: &seen, LastSeen: &seen}},
		cards: map[string][]CardState{"athens": {card("c1", "work", "running")}},
	}
	front, hub, done := pair(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer done()
	front.Config.Handler.(*Proxy).SetInventory(stock)
	hub.IndexEverywhere("athens", []CardState{everyRow("c1", "running", "atrium-1", "orchestrator", EverywhereTag)})
	if len(hub.every.find("elsewhere", "orchestrator")) != 1 {
		t.Fatal("not indexed")
	}
	// The fake keeps its cards when forgotten, so this is the endpoint's own drop.
	if code, body := forget(t, front.URL, "athens"); code != http.StatusOK {
		t.Fatalf("forget = %d %s", code, body)
	}
	if got := hub.every.byRoom["athens"]; len(got) != 0 {
		t.Fatalf("still indexed after the forget: %+v", got)
	}
}

// A bare name with no tag on the card at all still routes when exactly one
// card on another room answers to it, and the miss lists untagged cards too.
func TestHubSideSayRoutesToTheOneUntaggedCardOnAnotherRoom(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	x.hub.IndexEverywhere("sg4", []CardState{everyRow("s1", "running", "atrium-87300", "orchestrator")})

	if _, _, err := x.control.sayHandler(relayCtx(t), ctlReq("sa1", "m1mini"),
		sayInput{To: "orchestrator", Text: "hello"}); err != nil {
		t.Fatalf("say: %v", err)
	}
	said := x.mini.says()
	if len(said) != 1 || said[0]["to"] != "atrium-87300@sg4" {
		t.Fatalf("m1mini's /v1/say got %+v", said)
	}
	_, _, err := x.control.sayHandler(relayCtx(t), ctlReq("sa1", "m1mini"), sayInput{To: "nobody", Text: "x"})
	if err == nil || !strings.Contains(err.Error(), "atrium-87300@sg4") {
		t.Fatalf("the miss does not list the card on the other room: %v", err)
	}
}

// Two untagged matches on two rooms are refused, both named as name@room.
func TestHubSideSayRefusesTwoUntaggedMatches(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	x.hub.IndexEverywhere("sg4", []CardState{everyRow("s1", "running", "atrium-87300", "review")})
	x.hub.IndexEverywhere("sg3", []CardState{everyRow("t1", "running", "atrium-5120", "review")})

	_, _, err := x.control.sayHandler(relayCtx(t), ctlReq("sa1", "m1mini"), sayInput{To: "review", Text: "x"})
	if err == nil || !strings.Contains(err.Error(), "atrium-87300@sg4") ||
		!strings.Contains(err.Error(), "atrium-5120@sg3") {
		t.Fatalf("err = %v", err)
	}
	if len(x.mini.says()) != 0 {
		t.Fatal("something was sent")
	}
}

// A local match still wins over an untagged card elsewhere.
func TestHubSideLocalCardShadowsAnUntaggedOneElsewhere(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	x.hub.IndexEverywhere("sg4", []CardState{everyRow("s1", "running", "atrium-87300", "sa1")})

	if _, _, err := x.control.sayHandler(relayCtx(t), ctlReq("other", "m1mini"),
		sayInput{To: "sa1", Text: "hello"}); err != nil {
		t.Fatalf("say: %v", err)
	}
	if len(x.mini.says()) != 0 || len(x.mini.messages()) != 1 {
		t.Fatalf("the local card was not used: says=%+v messages=%+v", x.mini.says(), x.mini.messages())
	}
}
