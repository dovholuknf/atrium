package link

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Two rooms, so the aggregate view has something to aggregate.
func two(t *testing.T, a, b http.Handler) (*httptest.Server, *Hub, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hub := NewHub(Timings{Beat: 200 * time.Millisecond, Silence: 2 * time.Second, Warm: 2})
	ctx, stop := context.WithCancel(context.Background())
	go func() { _ = hub.Serve(ctx, ln) }()

	for name, h := range map[string]http.Handler{"alpha": a, "beta": b} {
		r := &Room{
			Name: name, Dial: plain{addr: ln.Addr().String()}, Handler: h,
			T: Timings{Beat: 200 * time.Millisecond, Warm: 2, Backoff: 50 * time.Millisecond},
		}
		go func(r *Room) { _ = r.Run(ctx) }(r)
	}
	waitFor(t, 5*time.Second, func() bool { return hub.Has("alpha") && hub.Has("beta") })

	front := httptest.NewServer(NewProxy(hub, nil, "", nil))
	return front, hub, func() { front.Close(); stop(); ln.Close() }
}

// cards answers `/v1/tasks` with one card, and says which room served every
// other request, so a test can tell where a click landed.
func cards(room, id string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/tasks" {
			fmt.Fprintf(w, `{"tasks":[{"id":%q,"title":%q}]}`, id, room+"-card")
			return
		}
		fmt.Fprintf(w, `{"served_by":%q,"path":%q}`, room, r.URL.Path)
	})
}

// THE AGGREGATE VIEW, which is the mode the hub cannot be a pipe for.
func TestTheAggregateViewMergesEveryRoom(t *testing.T) {
	front, _, done := two(t, cards("alpha", "card1"), cards("beta", "card2"))
	defer done()

	res, err := http.Get(front.URL + "/v1/tasks")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body struct {
		Tasks []map[string]any `json:"tasks"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Tasks) != 2 {
		t.Fatalf("expected a card from each room, got %d: %v", len(body.Tasks), body.Tasks)
	}
	// EVERY ROW CARRIES ITS ROOM, which is the tag the board draws and the
	// thing that will be filterable.
	seen := map[string]string{}
	for _, row := range body.Tasks {
		seen[row["room"].(string)] = row["id"].(string)
	}
	if seen["alpha"] == "" || seen["beta"] == "" {
		t.Fatalf("a row lost its room: %v", body.Tasks)
	}
	// AND THE ID IS TAGGED, which is what makes the row clickable.
	if seen["alpha"] != "alpha~card1" {
		t.Errorf("alpha's id came back as %q", seen["alpha"])
	}
}

// The point of tagging: a click on an aggregate row reaches the room that owns
// it, and the room sees the id IT minted.
func TestATaggedIdRoutesToItsRoomAndArrivesBare(t *testing.T) {
	front, _, done := two(t, cards("alpha", "card1"), cards("beta", "card2"))
	defer done()

	res, err := http.Get(front.URL + "/v1/tasks/beta~card2/asks")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var got struct {
		By   string `json:"served_by"`
		Path string `json:"path"`
	}
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.By != "beta" {
		t.Fatalf("the click landed in %q, not beta", got.By)
	}
	// THE ROOM MUST NOT SEE THE TAG. It minted `card2` and knows nothing about
	// rooms, so a tagged path would be a 404 on every aggregate click.
	if got.Path != "/v1/tasks/card2/asks" {
		t.Fatalf("the room was given %q, so the tag was not stripped", got.Path)
	}
}

// A header names a room, and then the hub is a pipe again.
func TestAHeaderScopesToOneRoom(t *testing.T) {
	front, _, done := two(t, cards("alpha", "card1"), cards("beta", "card2"))
	defer done()

	req, _ := http.NewRequest(http.MethodGet, front.URL+"/v1/tasks", nil)
	req.Header.Set(RoomHeader, "beta")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(raw), "beta-card") {
		t.Fatalf("scoped to beta, got %s", raw)
	}
	// SCOPED IS A PIPE: the room's own answer, untouched, so no `room` field
	// and no tagged id.
	if strings.Contains(string(raw), `"room"`) {
		t.Errorf("a scoped answer was rewritten: %s", raw)
	}
	if strings.Contains(string(raw), "beta~") {
		t.Errorf("a scoped answer had its id tagged: %s", raw)
	}
}

// ONE ROOM BEING DOWN IS NOT THE BOARD BEING DOWN. A blank board because one of
// four machines is busy is worse than a board that is briefly short and says so.
func TestAQuietRoomDoesNotEmptyTheBoard(t *testing.T) {
	slow := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not today", http.StatusInternalServerError)
	})
	front, _, done := two(t, cards("alpha", "card1"), slow)
	defer done()

	res, err := http.Get(front.URL + "/v1/tasks")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body struct {
		Tasks []map[string]any `json:"tasks"`
		Quiet []string         `json:"rooms_quiet"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Tasks) != 1 {
		t.Fatalf("the working room's card was lost: %v", body.Tasks)
	}
	if len(body.Quiet) != 1 || body.Quiet[0] != "beta" {
		t.Fatalf("the failure was swallowed rather than reported: %v", body.Quiet)
	}
}

// A machine-shaped write with no room to land in is a question, not a guess
// spread across every machine.
func TestAWriteWithNoRoomIsRefusedWithTheRooms(t *testing.T) {
	front, _, done := two(t, cards("alpha", "card1"), cards("beta", "card2"))
	defer done()

	res, err := http.Post(front.URL+"/v1/settings", "application/json",
		strings.NewReader(`{"editor_command":"code"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("got %d, expected 409", res.StatusCode)
	}
	raw, _ := io.ReadAll(res.Body)
	for _, want := range []string{"pick a room", "alpha", "beta"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("the refusal does not mention %q: %s", want, raw)
		}
	}
}

// With one room, nothing is asked and nothing is tagged. The operator was
// explicit about this.
func TestOneRoomIsNeverAskedAbout(t *testing.T) {
	front, _, done := pair(t, cards("solo", "card1"))
	defer done()

	res, err := http.Get(front.URL + "/v1/tasks")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if strings.Contains(string(raw), "solo~") {
		t.Fatalf("one room should not tag anything: %s", raw)
	}

	// And a write goes straight through rather than asking which room.
	res2, err := http.Post(front.URL+"/v1/settings", "application/json",
		strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res2.Body.Close()
	if res2.StatusCode == http.StatusConflict {
		t.Fatal("a single room was asked which room to use")
	}
}
