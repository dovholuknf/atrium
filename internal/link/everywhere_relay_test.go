package link

import (
	"net/http"
	"strings"
	"testing"
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
