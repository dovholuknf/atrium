package link

import (
	"encoding/json"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// link keeps its own copy of the board launcher's spawned_by so it need not
// import the store for one string. This is what stops the two drifting apart.
func TestHumanLauncherIsTheStoresHumanLauncher(t *testing.T) {
	if humanLauncher != store.HumanLauncher {
		t.Fatalf("link's humanLauncher is %q, store.HumanLauncher is %q", humanLauncher, store.HumanLauncher)
	}
}

// card is one announced card as a room writes it.
func everyRow(id, status, wire, alias string, tags ...string) CardState {
	p, _ := json.Marshal(map[string]any{"id": id, "status": status, "wire_name": wire, "alias": alias, "tags": tags})
	return CardState{ID: id, Status: status, Payload: p}
}

func TestEverywhereIndexesOnlyTaggedCardsThatAreNotEnded(t *testing.T) {
	h := NewHub(Timings{})
	h.IndexEverywhere("sg4", []CardState{
		everyRow("a", "running", "atrium-1", "orchestrator", EverywhereTag),
		everyRow("b", "done", "atrium-2", "old", EverywhereTag),
		everyRow("c", "dead", "atrium-3", "gone", EverywhereTag),
		everyRow("d", "running", "atrium-4", "plain", "other"),
		everyRow("e", "running", "atrium-5", ""),
	})
	got := h.every.all("")
	if len(got) != 1 || got[0].ID != "a" || got[0].Room != "sg4" {
		t.Fatalf("index = %+v, wanted only the running tagged card", got)
	}
}

// Only a card a human launched answers on every room. A worker inherits its
// launcher's tags, and an agent can pass the tag to atrium_launch.
func TestEverywhereIndexesOnlyCardsAHumanLaunched(t *testing.T) {
	launched := func(id, by string, tags ...string) CardState {
		p, _ := json.Marshal(map[string]any{"id": id, "status": "running", "wire_name": "atrium-" + id,
			"tags": tags, "spawned_by": by})
		return CardState{ID: id, Status: "running", Payload: p}
	}
	h := NewHub(Timings{})
	h.IndexEverywhere("sg4", []CardState{
		launched("hand", "", EverywhereTag),
		launched("board", "@human", EverywhereTag),
		launched("worker", "orchestrator@sg4", EverywhereTag),
		launched("agent", "", EverywhereTag, OriginTag),
		launched("agent2", "", OriginTag, EverywhereTag),
	})
	got := map[string]bool{}
	for _, c := range h.every.all("") {
		got[c.ID] = true
	}
	if len(got) != 2 || !got["hand"] || !got["board"] {
		t.Fatalf("index = %v, wanted the hand-started card and the board's", got)
	}
}

// A card that ends keeps its tag as a record and stops answering to it.
func TestEverywhereADoneCardDropsOutOfFind(t *testing.T) {
	h := NewHub(Timings{})
	h.IndexEverywhere("sg4", []CardState{everyRow("a", "running", "atrium-1", "orchestrator", EverywhereTag)})
	if got := h.every.find("sg3", "orchestrator"); len(got) != 1 {
		t.Fatalf("find before = %+v, wanted one", got)
	}
	h.IndexEverywhere("sg4", []CardState{everyRow("a", "done", "atrium-1", "orchestrator", EverywhereTag)})
	if got := h.every.find("sg3", "orchestrator"); len(got) != 0 {
		t.Fatalf("find after done = %+v, wanted none", got)
	}
}

func TestEverywhereAnnouncementReplacesOnlyThatRoom(t *testing.T) {
	h := NewHub(Timings{})
	h.IndexEverywhere("sg4", []CardState{everyRow("a", "running", "atrium-1", "", EverywhereTag)})
	h.IndexEverywhere("sg3", []CardState{everyRow("x", "running", "atrium-9", "", EverywhereTag)})
	if !h.IndexEverywhere("sg4", []CardState{everyRow("a", "done", "atrium-1", "", EverywhereTag)}) {
		t.Fatal("a card ending is a change to the index")
	}
	got := h.every.all("")
	if len(got) != 1 || got[0].Room != "sg3" {
		t.Fatalf("index = %+v, sg4's card should be gone and sg3's kept", got)
	}
	if h.IndexEverywhere("sg3", []CardState{everyRow("x", "running", "atrium-9", "", EverywhereTag)}) {
		t.Fatal("an identical announcement is not a change")
	}
}

func TestEverywhereFindMatchesHandleOrAliasAndExcludesTheAskingRoom(t *testing.T) {
	h := NewHub(Timings{})
	h.IndexEverywhere("sg4", []CardState{everyRow("a", "running", "atrium-87300", "Orchestrator", EverywhereTag)})
	for _, who := range []string{"orchestrator", "@ORCHESTRATOR", "atrium-87300", " @Atrium-87300 "} {
		if got := h.every.find("sg3", who); len(got) != 1 {
			t.Errorf("find(%q) = %+v, wanted one", who, got)
		}
	}
	if got := h.every.find("sg3", "a"); len(got) != 0 {
		t.Errorf("a card id matched: %+v", got)
	}
	if got := h.every.find("SG4", "orchestrator"); len(got) != 0 {
		t.Errorf("the asking room's own card matched: %+v", got)
	}
}

// What the hub was holding when it started is what the store had, so the same
// call that an announcement makes builds it.
func TestEverywhereAnAnnouncementReachesTheIndex(t *testing.T) {
	state := newRoomState(map[string]any{"id": "one", "status": "running", "wire_name": "atrium-1",
		"alias": "orchestrator", "tags": []string{EverywhereTag}})
	hub, _, _, stop := caching(t, state)
	defer stop()
	waitFor(t, announceWait, func() bool { return len(hub.every.find("elsewhere", "orchestrator")) == 1 })
}
