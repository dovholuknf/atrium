//go:build integration

package link

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// named is a room on an older build: it lists its cards, answers a card read
// only by id, and answers anything else naming a card it does not hold with the
// 500 rooms gave before R1. Every other request says where it landed. lists
// counts its list requests.
type named struct {
	room  string
	cards []ctlCard
	lists atomic.Int32
}

func (n *named) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/v1/tasks" {
		n.lists.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"tasks": n.cards})
		return
	}
	var body struct {
		TaskID string `json:"task_id"`
	}
	raw, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(raw, &body)
	id := cardIDIn(r.URL.Path)
	if id == "" {
		id = body.TaskID
	}
	held := false
	for _, c := range n.cards {
		held = held || c.ID == id
	}
	if id != "" && !held {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":"sql: no rows in result set"}`)
		return
	}
	fmt.Fprintf(w, `{"id":%q,"served_by":%q,"path":%q,"saw_task_id":%q}`, id, n.room, r.URL.Path, body.TaskID)
}

func agentCard(id, wire, alias, status string) ctlCard {
	return ctlCard{ID: id, Wire: wire, Alias: alias, Status: status, Created: "2026-09-30T10:00:00Z"}
}

const (
	idA = "01a0aaaa-0000-7000-8000-000000000001"
	idB = "01a0bbbb-0000-7000-8000-000000000002"
)

// ask sends one request and reads the answer whole, headers included.
func ask(t *testing.T, method, url, body string) (int, http.Header, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var m map[string]any
	raw, _ := io.ReadAll(res.Body)
	_ = json.Unmarshal(raw, &m)
	return res.StatusCode, res.Header, m
}

// TWO LIVE CARDS WITH ONE ALIAS ON TWO ROOMS IS A 409 NAMING BOTH. Never
// whichever room answered first.
func TestANameOnTwoRoomsIsAConflict(t *testing.T) {
	a := &named{room: "alpha", cards: []ctlCard{agentCard(idA, "alpha-rnd", "rnd", "running")}}
	b := &named{room: "beta", cards: []ctlCard{agentCard(idB, "beta-rnd", "rnd", "needs-input")}}
	front, _, done := two(t, a, b)
	defer done()

	code, _, body := ask(t, http.MethodGet, front.URL+"/v1/tasks/rnd", "")
	cands, _ := body["candidates"].([]any)
	if code != http.StatusConflict || len(cands) != 2 {
		t.Fatalf("rnd on two rooms answered %d %v", code, body)
	}
	if !strings.HasPrefix(fmt.Sprint(cands[0]), "rnd@alpha") || !strings.HasPrefix(fmt.Sprint(cands[1]), "rnd@beta") {
		t.Fatalf("the candidates are %v, want rnd@alpha and rnd@beta", cands)
	}
	// Each choice carries what the board's chooser draws, so it need not fetch each card.
	choices, _ := body["choices"].([]any)
	if len(choices) != 2 {
		t.Fatalf("choices are %v", body["choices"])
	}
	first, _ := choices[0].(map[string]any)
	second, _ := choices[1].(map[string]any)
	if first["room"] != "alpha" || first["status"] != "running" || first["created_at"] != "2026-09-30T10:00:00Z" ||
		first["handle"] != "alpha-rnd@alpha" || first["card"] != "alpha~"+idA || first["spelled"] != cands[0] {
		t.Fatalf("the first choice is %v", first)
	}
	if second["room"] != "beta" || second["status"] != "needs-input" {
		t.Fatalf("the second choice is %v", second)
	}
	// Named with its room, it is one card again.
	code, hdr, body := ask(t, http.MethodPost, front.URL+"/v1/tasks/rnd@beta/exit", "{}")
	if code != http.StatusOK || body["served_by"] != "beta" || body["path"] != "/v1/tasks/"+idB+"/exit" {
		t.Fatalf("rnd@beta answered %d %v", code, body)
	}
	if hdr.Get("X-Atrium-Card") != "beta~"+idB || hdr.Get("X-Atrium-Handle") != "beta-rnd@beta" {
		t.Fatalf("the answer named %q %q", hdr.Get("X-Atrium-Card"), hdr.Get("X-Atrium-Handle"))
	}
}

// LIVE BEATS DONE across rooms, and the room that 500s on a name is reached by
// its id, because the hub resolved the name.
func TestALiveCardBeatsADoneOneOnAnotherRoom(t *testing.T) {
	a := &named{room: "alpha", cards: []ctlCard{agentCard(idA, "alpha-sa12", "sa12", "done")}}
	b := &named{room: "beta", cards: []ctlCard{agentCard(idB, "beta-sa12", "sa12", "running")}}
	front, _, done := two(t, a, b)
	defer done()

	code, hdr, body := ask(t, http.MethodPost, front.URL+"/v1/tasks/@sa12/new-context", "{}")
	if code != http.StatusOK || body["served_by"] != "beta" || body["path"] != "/v1/tasks/"+idB+"/new-context" {
		t.Fatalf("@sa12 answered %d %v, want the live one on beta", code, body)
	}
	if hdr.Get("X-Atrium-Handle") != "beta-sa12@beta" {
		t.Fatalf("the handle header is %q", hdr.Get("X-Atrium-Handle"))
	}
	// A launch onto it by name reaches the room with the bare id in the body.
	code, _, body = ask(t, http.MethodPost, front.URL+"/v1/launch", `{"harness":"claude","task_id":"sa12"}`)
	if code != http.StatusOK || body["served_by"] != "beta" || body["saw_task_id"] != idB {
		t.Fatalf("a launch onto sa12 answered %d %v", code, body)
	}
}

// A MISS IS A 404 THAT SAYS WHAT WOULD HAVE WORKED, and names a room that did
// not answer.
func TestAMissSaysWhatWouldHaveWorked(t *testing.T) {
	a := &named{room: "alpha", cards: []ctlCard{agentCard(idA, "alpha-ui", "ui", "running")}}
	quiet := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	front, _, done := two(t, a, quiet)
	defer done()

	code, _, body := ask(t, http.MethodGet, front.URL+"/v1/tasks/nobody", "")
	work, _ := body["would_work"].([]any)
	if code != http.StatusNotFound || len(work) != 1 || work[0] != "alpha-ui@alpha (@ui)" {
		t.Fatalf("a miss answered %d %v", code, body)
	}
	if !strings.Contains(fmt.Sprint(body["error"]), "not answering: beta") {
		t.Fatalf("the miss did not name the quiet room: %v", body["error"])
	}
	// The quiet room does not stop a name the other room holds.
	if code, _, body := ask(t, http.MethodGet, front.URL+"/v1/tasks/ui", ""); code != http.StatusOK ||
		body["served_by"] != "alpha" {
		t.Fatalf("ui with one room quiet answered %d %v", code, body)
	}
	// A WRITE DOES NOT GUESS PAST THE QUIET ROOM: the card meant may be there.
	code, _, body = ask(t, http.MethodPost, front.URL+"/v1/tasks/ui/exit", "{}")
	cands, _ := body["candidates"].([]any)
	if code != http.StatusConflict || len(cands) != 2 || !strings.Contains(fmt.Sprint(cands), "ui@beta (not answering)") {
		t.Fatalf("a write with a room quiet answered %d %v, want 409 naming both", code, body)
	}
	// Named with its room, it goes.
	if code, _, body := ask(t, http.MethodPost, front.URL+"/v1/tasks/ui@alpha/exit", "{}"); code != http.StatusOK ||
		body["served_by"] != "alpha" {
		t.Fatalf("ui@alpha with beta quiet answered %d %v", code, body)
	}
	// A name on a room that is not attached.
	if code, _, _ := ask(t, http.MethodGet, front.URL+"/v1/tasks/ui@gamma", ""); code != http.StatusNotFound {
		t.Fatalf("ui@gamma answered %d", code)
	}
}

// THE BOARD'S ID PATH IS UNTOUCHED: an id goes by roomHolding and no room is
// asked for its list.
func TestAnIDNeverAsksForAList(t *testing.T) {
	a := &named{room: "alpha", cards: []ctlCard{agentCard(idA, "alpha-x", "x", "running")}}
	b := &named{room: "beta", cards: []ctlCard{agentCard(idB, "beta-y", "y", "running")}}
	front, _, done := two(t, a, b)
	defer done()

	for _, path := range []string{"/v1/tasks/" + idB, "/v1/tasks/beta~" + idB} {
		code, hdr, body := ask(t, http.MethodGet, front.URL+path, "")
		if code != http.StatusOK || body["served_by"] != "beta" {
			t.Fatalf("%s answered %d %v", path, code, body)
		}
		if hdr.Get("X-Atrium-Handle") != "" {
			t.Fatalf("%s was resolved as a name", path)
		}
	}
	if n := a.lists.Load() + b.lists.Load(); n != 0 {
		t.Fatalf("an id asked the rooms for %d lists", n)
	}
}

// ONE ROOM: a name resolves there too, and a segment no list carries goes on
// to the room as it always did.
func TestANameOnTheOnlyRoom(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hub := NewHub(Timings{Beat: 200 * time.Millisecond, Silence: 2 * time.Second, Warm: 2})
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	defer ln.Close()
	go func() { _ = hub.Serve(ctx, ln) }()
	a := &named{room: "alpha", cards: []ctlCard{agentCard(idA, "alpha-merge", "merge", "running")}}
	room := &Room{Name: "alpha", Dial: plain{addr: ln.Addr().String()}, Handler: a,
		T: Timings{Beat: 200 * time.Millisecond, Warm: 2, Backoff: 50 * time.Millisecond}}
	go func() { _ = room.Run(ctx) }()
	waitFor(t, 5*time.Second, func() bool { return hub.Has("alpha") })
	front := httptest.NewServer(NewProxy(hub, nil, "", nil))
	defer front.Close()

	code, hdr, body := ask(t, http.MethodGet, front.URL+"/v1/tasks/merge", "")
	if code != http.StatusOK || body["path"] != "/v1/tasks/"+idA || hdr.Get("X-Atrium-Card") != idA {
		t.Fatalf("merge on the only room answered %d %v %q", code, body, hdr.Get("X-Atrium-Card"))
	}
	if code, _, _ := ask(t, http.MethodGet, front.URL+"/v1/tasks/odd-id", ""); code != http.StatusInternalServerError {
		t.Fatalf("a segment no list carries answered %d, want the room's own answer", code)
	}
}

// THE RESOLVER, on its own.
func TestResolveAcross(t *testing.T) {
	lists := map[string][]ctlCard{
		"alpha": {agentCard("a1", "alpha-w", "w", "done"), agentCard("a2", "alpha-v", "v", "dead")},
		"beta":  {agentCard("b1", "beta-w", "w", "done")},
	}
	var amb *errAmbiguous
	if _, err := resolveAcross(lists, nil, "w"); !errors.As(err, &amb) || len(amb.candidates) != 2 {
		t.Fatalf("two done matches answered %v, want ambiguous", err)
	}
	var none *errNoCard
	if _, err := resolveAcross(lists, nil, "v"); !errors.As(err, &none) {
		t.Fatalf("a dead card's alias answered %v, want no card", err)
	}
	got, err := resolveAcross(lists, nil, "alpha-w")
	if err != nil || got.Room != "alpha" || got.Card.ID != "a1" {
		t.Fatalf("a wire name answered %+v %v", got, err)
	}
	// Inside one room, live before done, then newest.
	one := []ctlCard{agentCard("old", "r-old", "sa1", "done"), agentCard("new", "r-new", "sa1", "running")}
	if c, ok := matchCard(one, "@SA1"); !ok || c.ID != "new" {
		t.Fatalf("matchCard picked %+v", c)
	}
}
