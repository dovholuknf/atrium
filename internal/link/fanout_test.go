package link

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
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

// THE BOARD DECIDES ATRIUM IS UP FROM THIS, so a hub with four rooms must not
// answer it the way a hub with none does.
func TestHealthIsAnsweredAcrossRooms(t *testing.T) {
	well := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true,"build":"room-hash","halted":false}`)
	})
	sick := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true,"build":"room-hash","halted":true,"cause":"disk full"}`)
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hub := NewHub(Timings{Beat: 200 * time.Millisecond, Silence: 2 * time.Second, Warm: 2})
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go func() { _ = hub.Serve(ctx, ln) }()
	for name, h := range map[string]http.Handler{"alpha": well, "beta": sick} {
		r := &Room{Name: name, Dial: plain{addr: ln.Addr().String()}, Handler: h,
			T: Timings{Beat: 200 * time.Millisecond, Warm: 2, Backoff: 50 * time.Millisecond}}
		go func(r *Room) { _ = r.Run(ctx) }(r)
	}
	waitFor(t, 5*time.Second, func() bool { return hub.Has("alpha") && hub.Has("beta") })

	front := httptest.NewServer(NewProxy(hub, nil, "hub-hash", nil))
	defer front.Close()

	res, err := http.Get(front.URL + "/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("the board was told atrium is down: %d", res.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	// THE HUB'S BOARD HASH, or every tab reload-loops.
	if body["build"] != "hub-hash" {
		t.Errorf("health reported build %v", body["build"])
	}
	// ONE HALTED ROOM HALTS THE BOARD. Hiding it behind a healthy room is a
	// lie at the one moment it matters.
	if body["halted"] != true {
		t.Errorf("a halted room was hidden: %v", body)
	}
	if !strings.Contains(fmt.Sprint(body["cause"]), "disk full") {
		t.Errorf("the cause was lost: %v", body["cause"])
	}
}

// THE TAG HAS TO SURVIVE THE ROUND TRIP, or it survives exactly one hop.
//
// A merged list hands the board `alpha~card1`. The board asks for that card,
// the hub strips the tag on the way in because the room minted the bare id, and
// the room answers with its own `id`. Without putting the tag back, the board
// now holds a bare id and every url it builds from it names no room.
func TestASingleCardComesBackTagged(t *testing.T) {
	one := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"card1","title":"a card","task_id":"card1"}`)
	})
	front, _, done := two(t, one, one)
	defer done()

	res, err := http.Get(front.URL + "/v1/tasks/alpha~card1")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var got map[string]any
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got["id"] != "alpha~card1" {
		t.Errorf("the card came back as %q, so the next url the board builds names no room", got["id"])
	}
	if got["task_id"] != "alpha~card1" {
		t.Errorf("task_id came back as %q", got["task_id"])
	}
	if got["room"] != "alpha" {
		t.Errorf("the card lost its room: %v", got)
	}
}

// The board cannot draw without these, so one room answers rather than the hub
// refusing and leaving panes empty.
func TestTheBoardCanStillReadItsSettings(t *testing.T) {
	front, _, done := two(t, cards("alpha", "card1"), cards("beta", "card2"))
	defer done()

	for _, path := range []string{"/v1/settings", "/v1/themes", "/v1/rooms"} {
		res, err := http.Get(front.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Errorf("%s answered %d, so the board draws that pane empty", path, res.StatusCode)
		}
	}

	// A WRITE STILL ASKS. Reading one machine's answer to draw a page is not
	// the same as saving a setting onto a machine you did not choose.
	res, err := http.Post(front.URL+"/v1/settings", "application/json",
		strings.NewReader(`{"editor_command":"code"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("a write answered %d, expected 409", res.StatusCode)
	}
}

// ledger answers `/v1/history` for one room: `n` rows, newest first, paged.
func ledger(room string, n int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/v1/history" {
			fmt.Fprint(w, `{}`)
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		if limit <= 0 {
			limit = 50
		}
		rows := []string{}
		for i := 0; i < n; i++ {
			if i < offset || len(rows) >= limit {
				continue
			}
			// Newest first, and interleaved between rooms: alpha's are on the
			// even minutes and beta's on the odd ones, so a merge that just
			// concatenated would be visibly out of order.
			min := 58 - i*2
			if room == "beta" {
				min--
			}
			rows = append(rows, fmt.Sprintf(
				`{"id":"%s-%d","created_at":"2026-09-17T12:%02d:00Z","display_title":"%s %d"}`,
				room, i, min, room, i))
		}
		fmt.Fprintf(w, `{"tasks":[%s],"total":%d,"offset":%d}`,
			strings.Join(rows, ","), n, offset)
	})
}

// HISTORY IS EVERY MACHINE'S, IN ORDER, AND PAGING IT MUST NOT LOSE ROWS.
//
// Asking four rooms for fifty rows each and concatenating gives two hundred,
// and asking again at offset fifty then skips whatever the first answer had
// already shown. The board would lose rows and nothing would look broken.
func TestHistoryMergesInOrderAndPagesWithoutLosingRows(t *testing.T) {
	front, _, done := two(t, ledger("alpha", 6), ledger("beta", 6))
	defer done()

	read := func(offset, limit int) ([]string, int) {
		t.Helper()
		res, err := http.Get(fmt.Sprintf("%s/v1/history?limit=%d&offset=%d", front.URL, limit, offset))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var body struct {
			Tasks []map[string]any `json:"tasks"`
			Total int              `json:"total"`
		}
		if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		ids := make([]string, 0, len(body.Tasks))
		for _, row := range body.Tasks {
			id, _ := row["id"].(string)
			if room, _ := row["room"].(string); room == "" {
				t.Errorf("a history row lost its room: %v", row)
			}
			ids = append(ids, id)
		}
		return ids, body.Total
	}

	// EVERY ROOM'S ROWS, INTERLEAVED BY TIME, not one room's then the other's.
	first, total := read(0, 4)
	if total != 12 {
		t.Errorf("the total was %d, expected both rooms counted", total)
	}
	want := []string{"alpha~alpha-0", "beta~beta-0", "alpha~alpha-1", "beta~beta-1"}
	if strings.Join(first, ",") != strings.Join(want, ",") {
		t.Errorf("page one came back as %v, expected %v", first, want)
	}

	// AND THE NEXT PAGE CARRIES ON rather than starting over. This is the one
	// that fails if each room is asked for its own page and the answers are
	// concatenated.
	second, _ := read(4, 4)
	seen := map[string]bool{}
	for _, id := range first {
		seen[id] = true
	}
	for _, id := range second {
		if seen[id] {
			t.Errorf("%s was shown on both pages", id)
		}
	}
	if len(second) != 4 {
		t.Errorf("page two had %d rows: %v", len(second), second)
	}
	if second[0] != "alpha~alpha-2" {
		t.Errorf("page two starts at %s, so rows between the pages were dropped", second[0])
	}
}

// THE CONNECTION POOL IS KEYED BY HOST, and every room used to share one, so a
// request for beta could reuse a connection already dialled to alpha. Sequential
// because that is what fills the pool: the first request leaves an idle
// connection behind for the second to find.
func TestScopedRequestsNeverReuseAnotherRoomsConnection(t *testing.T) {
	front, _, done := two(t, cards("alpha", "card1"), cards("beta", "card2"))
	defer done()

	ask := func(room string) string {
		req, _ := http.NewRequest(http.MethodGet, front.URL+"/v1/whoami", nil)
		req.Header.Set(RoomHeader, room)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var got struct {
			By string `json:"served_by"`
		}
		if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		return got.By
	}
	// Alternating, several times, so a pooled connection has every chance to
	// be handed to the wrong room.
	for i := 0; i < 6; i++ {
		if by := ask("alpha"); by != "alpha" {
			t.Fatalf("round %d: a request for alpha landed in %q", i, by)
		}
		if by := ask("beta"); by != "beta" {
			t.Fatalf("round %d: a request for beta landed in %q", i, by)
		}
	}
}

// A WRITE IS NOT A LIST, even when it is the same path. `/v1/tasks` is where a
// card is made as well as where they are listed, and fanning a POST out as
// reads would answer 200 with a task list while the card was never created.
func TestAWriteToAMergedPathIsNotFannedOut(t *testing.T) {
	front, _, done := two(t, cards("alpha", "card1"), cards("beta", "card2"))
	defer done()

	res, err := http.Post(front.URL+"/v1/tasks", "application/json",
		strings.NewReader(`{"title":"a new card"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusConflict {
		raw, _ := io.ReadAll(res.Body)
		t.Fatalf("a POST to a merged path answered %d: %s", res.StatusCode, raw)
	}
}

// owns answers as a room that holds exactly the given card ids. It 200s for a
// card it owns - the card itself and any per-card sub-path - and 404s for one it
// does not, so the hub's bare-id resolver has something to probe.
func owns(room string, ids ...string) http.Handler {
	has := map[string]bool{}
	for _, id := range ids {
		has[id] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/tasks" {
			rows := make([]string, 0, len(ids))
			for _, id := range ids {
				rows = append(rows, fmt.Sprintf(`{"id":%q,"title":%q}`, id, room+"-card"))
			}
			fmt.Fprintf(w, `{"tasks":[%s]}`, strings.Join(rows, ","))
			return
		}
		id := cardIDIn(r.URL.Path)
		if id == "" || !has[id] {
			http.Error(w, `{"error":"no such card"}`, http.StatusNotFound)
			return
		}
		fmt.Fprintf(w, `{"served_by":%q,"path":%q,"id":%q}`, room, r.URL.Path, id)
	})
}

// A BARE CARD ID UNDER TWO ROOMS RESOLVES TO ITS OWNER, rather than asking.
//
// The paste-into-a-terminal case: a terminal that attached while one room was
// live holds a bare id, so its upload posts /v1/tasks/<bare>/files with no tag
// and no header. With two rooms that used to be the needsARoom 409. A card id is
// unique, so the hub finds which room holds it and routes there.
func TestABareCardIdRoutesToItsOwningRoom(t *testing.T) {
	front, _, done := two(t, owns("alpha", "acard"), owns("beta", "bcard"))
	defer done()

	res, err := http.Post(front.URL+"/v1/tasks/bcard/files", "application/json",
		strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusConflict {
		t.Fatalf("a bare card id was refused with needsARoom instead of resolved")
	}
	var got struct {
		By   string `json:"served_by"`
		Path string `json:"path"`
	}
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.By != "beta" {
		t.Fatalf("the write landed in %q, not beta", got.By)
	}
	// THE ROOM SEES THE BARE PATH. It minted the id and knows nothing of rooms.
	if got.Path != "/v1/tasks/bcard/files" {
		t.Fatalf("the room saw %q, want the bare per-card path", got.Path)
	}
}

// Every per-card verb, not just files: a bare-id message lands on the card's
// machine too, because the fix is in the routing rather than in one endpoint.
func TestABareCardMessageRoutesToItsOwningRoom(t *testing.T) {
	front, _, done := two(t, owns("alpha", "acard"), owns("beta", "bcard"))
	defer done()

	res, err := http.Post(front.URL+"/v1/tasks/acard/message", "application/json",
		strings.NewReader(`{"text":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var got struct {
		By string `json:"served_by"`
	}
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.By != "alpha" {
		t.Fatalf("the message landed in %q, not alpha", got.By)
	}
}

// A bare id no attached room holds is a card that is gone: a 404, not a 500 and
// not a room prompt.
func TestABareCardIdForNoRoomIs404(t *testing.T) {
	front, _, done := two(t, owns("alpha", "acard"), owns("beta", "bcard"))
	defer done()

	res, err := http.Get(front.URL + "/v1/tasks/ghost/files/list")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("a bare id no room holds answered %d, want 404", res.StatusCode)
	}
}

// THE FIX DOES NOT WEAKEN needsARoom. A machine-shaped write with no card in its
// path still has no room to land in, so it is still asked, not guessed at.
func TestAMachineWriteWithNoCardStillNeedsARoom(t *testing.T) {
	front, _, done := two(t, owns("alpha", "acard"), owns("beta", "bcard"))
	defer done()

	res, err := http.Post(front.URL+"/v1/harnesses", "application/json",
		strings.NewReader(`{"id":"claude"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("a machine-shaped write answered %d, want the 409 that asks which room",
			res.StatusCode)
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
