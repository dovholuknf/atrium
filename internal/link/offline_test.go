package link

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// What the board sees when a room is not answering.
//
// The rule underneath every test here is one sentence: the only authoritative
// answer about a room comes from that room. What the hub remembers is shown as
// what it is, cannot be acted on, and is never counted anywhere.

// remembering is an Inventory holding cards for rooms that are not here.
type remembering struct {
	rooms []Known
	cards map[string][]CardState
	asked map[string]int
}

func (r *remembering) Known() ([]Known, error) { return r.rooms, nil }

func (r *remembering) MarkRoom(string, bool) error { return nil }

func (r *remembering) Remembered(name string) ([]CardState, error) {
	if r.asked == nil {
		r.asked = map[string]int{}
	}
	r.asked[name]++
	return r.cards[name], nil
}

func (r *remembering) Holding() ([]string, error) {
	var out []string
	for name := range r.cards {
		out = append(out, name)
	}
	return out, nil
}

func card(id, title, status string) CardState {
	raw, _ := json.Marshal(map[string]any{"id": id, "title": title, "status": status})
	return CardState{ID: id, Status: status, Payload: raw}
}

// tasksFrom reads the merged card list off a proxy.
func tasksFrom(t *testing.T, front string) []map[string]any {
	t.Helper()
	res, err := http.Get(front + "/v1/tasks")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var body struct {
		Tasks []map[string]any `json:"tasks"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("could not read the board's answer: %v (%s)", err, raw)
	}
	return body.Tasks
}

// A MACHINE SOMEBODY SHUT STILL HAS WORK ON IT, and a board that dropped those
// rows would be saying the work does not exist.
func TestTheBoardStillShowsWhatAnOfflineRoomWasHolding(t *testing.T) {
	seen := time.Now().Add(-time.Hour)
	stock := &remembering{
		rooms: []Known{{Name: "athens", FirstSeen: &seen, LastSeen: &seen}},
		cards: map[string][]CardState{
			"athens": {card("c1", "the work left on athens", "running")},
		},
	}

	front, _, done := pair(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []any{
			map[string]any{"id": "live1", "title": "the work here", "status": "running"},
		}})
	}))
	defer done()
	front.Config.Handler.(*Proxy).SetInventory(stock)

	tasks := tasksFrom(t, front.URL)
	if len(tasks) != 2 {
		t.Fatalf("the board drew %d card(s), wanted the live one and the remembered one: %+v",
			len(tasks), tasks)
	}

	var gone map[string]any
	for _, row := range tasks {
		if row["offline"] == true {
			gone = row
		}
	}
	if gone == nil {
		t.Fatalf("nothing was marked as being on a room that is not answering: %+v", tasks)
	}
	if gone["room"] != "athens" {
		t.Fatalf("the remembered card says it is on %v", gone["room"])
	}
	// TAGGED LIKE A LIVE CARD, so every url the board builds from it carries
	// the room and is refused by name rather than going somewhere else.
	if gone["id"] != "athens~c1" {
		t.Fatalf("the remembered card's id is %v, which does not route", gone["id"])
	}
	// DERIVED, NOT CACHED. The stored row has no display title, and a card with
	// no title is one nobody can recognise.
	if gone["display_title"] != "the work left on athens" {
		t.Fatalf("the remembered card has no title to draw: %v", gone["display_title"])
	}
}

// A ROOM THAT IS HERE ANSWERS FOR ITSELF, and its remembered cards are never
// drawn beside its real ones. That is the whole of decision 15: there is no
// third state where some rows are fresh and some are remembered.
func TestALiveRoomsCacheIsNeverRead(t *testing.T) {
	seen := time.Now()
	stock := &remembering{
		rooms: []Known{{Name: "testroom", Attached: true, FirstSeen: &seen, LastSeen: &seen}},
		cards: map[string][]CardState{
			// What it said an hour ago, which must not appear while it is here.
			"testroom": {card("stale", "what it said an hour ago", "running")},
		},
	}

	front, _, done := pair(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []any{
			map[string]any{"id": "fresh", "title": "what it says now", "status": "running"},
		}})
	}))
	defer done()
	front.Config.Handler.(*Proxy).SetInventory(stock)

	tasks := tasksFrom(t, front.URL)
	for _, row := range tasks {
		if row["offline"] == true {
			t.Fatalf("a remembered card was drawn for a room that is attached: %+v", row)
		}
	}
	if len(tasks) != 1 || tasks[0]["title"] != "what it says now" {
		t.Fatalf("the live room's own answer is not what was drawn: %+v", tasks)
	}
}

// A ROOM THAT HAS NEVER CONNECTED DRAWS NOTHING, because it cannot have cards.
// It exists in the rooms tab, which is the inventory, and that is the only
// place it belongs.
func TestARoomThatNeverConnectedIsNotOnTheBoard(t *testing.T) {
	stock := &remembering{
		rooms: []Known{{Name: "byzantium"}},
		// Nothing in `cards`, which is what `Holding` reads: a room that never
		// connected cannot have any.
		cards: map[string][]CardState{},
	}

	front, _, done := pair(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []any{
			map[string]any{"id": "live1", "title": "the work here", "status": "running"},
		}})
	}))
	defer done()
	front.Config.Handler.(*Proxy).SetInventory(stock)

	tasks := tasksFrom(t, front.URL)
	if len(tasks) != 1 {
		t.Fatalf("a room that has never connected put %d card(s) on the board: %+v",
			len(tasks)-1, tasks)
	}
}

// A ROOM MARKED FOR DELETION STARTS NOTHING NEW, and that is the whole of what
// marking does.
//
// Everything already running carries on, the room stays on every list, and
// nothing is destroyed. The one thing that changes is that work stops arriving,
// which is what makes marking safe to press and useful at all.
func TestAMarkedRoomStartsNoNewWork(t *testing.T) {
	seen := time.Now()
	stock := &remembering{
		rooms: []Known{{
			Name: "testroom", Attached: true, State: "marked-for-deletion",
			FirstSeen: &seen, LastSeen: &seen,
		}},
		cards: map[string][]CardState{},
	}

	var reached bool
	front, _, done := pair(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			reached = true
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer done()
	front.Config.Handler.(*Proxy).SetInventory(stock)

	// EVERY WAY WORK ARRIVES, not only the one a person clicks. A source
	// posting into the inbox and a dispatch queued by a script both put new
	// work on a machine, and a room being decommissioned through one of those
	// is the case nobody is watching for.
	for _, path := range []string{"/v1/launch", "/v1/tasks", "/v1/intake", "/v1/dispatch"} {
		reached = false
		res, err := http.Post(front.URL+path, "application/json", strings.NewReader("{}"))
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusConflict {
			t.Fatalf("%s on a room marked for deletion answered %d", path, res.StatusCode)
		}
		if reached {
			t.Fatalf("%s reached the room anyway", path)
		}
		if !strings.Contains(string(raw), "marked for deletion") {
			t.Fatalf("%s was refused without saying why: %s", path, raw)
		}
	}

	// AND EVERYTHING ELSE ABOUT THAT ROOM STILL WORKS. Marking is not read-only
	// mode: a card already there can be renamed, answered, shelved and
	// finished, because the work in flight is meant to be worked out normally.
	res2, err := http.Get(front.URL + "/v1/tasks")
	if err != nil {
		t.Fatal(err)
	}
	defer res2.Body.Close()
	if res2.StatusCode != http.StatusOK {
		t.Fatalf("reading a marked room's board answered %d", res2.StatusCode)
	}

	// A CHANGE TO WORK ALREADY THERE IS NOT ARRIVING WORK. This is the half
	// that makes marking safe to press: press it and the machine finishes what
	// it has.
	reached = false
	req, err := http.NewRequest(http.MethodPatch, front.URL+"/v1/tasks/abc",
		strings.NewReader(`{"title":"still allowed"}`))
	if err != nil {
		t.Fatal(err)
	}
	res3, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res3.Body.Close()
	if res3.StatusCode == http.StatusConflict {
		t.Fatal("marking a room stopped a change to a card already on it")
	}
}

// EVERY OPERATION ON AN OFFLINE ROOM'S CARD IS REFUSED, BY NAME.
//
// "No room is attached" is wrong here in the way that matters: there are rooms
// attached, just not that one, and the board shows this sentence to whoever
// clicked.
func TestActingOnAnOfflineRoomIsRefusedByName(t *testing.T) {
	seen := time.Now().Add(-time.Hour)
	stock := &remembering{
		rooms: []Known{{Name: "athens", FirstSeen: &seen, LastSeen: &seen}},
		cards: map[string][]CardState{"athens": {card("c1", "work", "running")}},
	}

	front, _, done := pair(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer done()
	front.Config.Handler.(*Proxy).SetInventory(stock)

	req, err := http.NewRequest(http.MethodPatch, front.URL+"/v1/tasks/athens~c1", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("changing a card on an offline room answered %d", res.StatusCode)
	}
	raw, _ := io.ReadAll(res.Body)
	var body struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(raw, &body)
	if !strings.Contains(body.Error, "athens") {
		t.Fatalf("the refusal does not name the room: %s", body.Error)
	}
	if !strings.Contains(body.Error, "not answering") {
		t.Fatalf("the refusal does not say what is wrong: %s", body.Error)
	}
}
