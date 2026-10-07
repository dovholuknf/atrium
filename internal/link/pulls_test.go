package link

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// The pulls API through the hub. The room side is another branch, so the rooms here are fakes that
// answer what docs/review/pulls-api.md says a room answers.

// seenReq is one request a fake room received, as it arrived.
type seenReq struct {
	Method, URI, Body, INM, Type string
}

// pullsRoom is a fake room serving the pulls routes. `rows` is what `GET /v1/prs` lists.
type pullsRoom struct {
	name string
	// rows are the list's rows, bare ids. Counts and nav_count are worked out from them.
	rows []map[string]any
	// old is a room on a build with no /v1/prs: every pulls route answers a bare 404.
	old bool
	// sick answers every pulls route with a 500.
	sick bool

	mu   sync.Mutex
	seen []seenReq
}

func (f *pullsRoom) requests() []seenReq {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]seenReq(nil), f.seen...)
}

func (f *pullsRoom) count(method, uri string) int {
	n := 0
	for _, q := range f.requests() {
		if q.Method == method && q.URI == uri {
			n++
		}
	}
	return n
}

// findingsETag is the validator the fake's findings carry.
const findingsETag = `"fnd-7f1c"`

// findingsRaw is a findings body with bytes a re-marshal would change: an ampersand and angle brackets, which Go's
// encoder escapes as & and <, and a key order that is not alphabetical.
func findingsRaw(pr string) string {
	return `{"pr":{"id":"` + pr + `","state":"ready","walker_task":""},"findings":[{"key":"f-3a9c01d4e2",` +
		`"text":"a && b < c","hash":"7f1c","position":1}]}`
}

func (f *pullsRoom) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.seen = append(f.seen, seenReq{r.Method, r.URL.RequestURI(), string(body), r.Header.Get("If-None-Match"),
		r.Header.Get("Content-Type")})
	f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if !strings.HasPrefix(r.URL.Path, "/v1/prs") {
		w.Write([]byte(`{"tasks":[]}`))
		return
	}
	if f.old {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("404 page not found\n"))
		return
	}
	if f.sick {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"the store fell over","code":"internal"}`))
		return
	}
	rest := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/v1/prs"), "/")
	parts := strings.Split(rest, "/")
	switch {
	case rest == "" && r.Method == http.MethodGet:
		counts := map[string]int{"queued": 0, "fetching": 0, "running": 0, "ready": 0, "failed": 0, "aborted": 0}
		nav := 0
		for _, row := range f.rows {
			st, _ := row["state"].(string)
			counts[st]++
			if st == "ready" || st == "failed" {
				nav++
			}
		}
		rows := f.rows
		if rows == nil {
			rows = []map[string]any{}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"prs": rows, "counts": counts, "nav_count": nav})
	case rest == "" && r.Method == http.MethodPost:
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"pr":{"id":"pr_01k8x2m4q7","walker_task":""},"created":true}`))
	case parts[0] == "pr_halted":
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":"atrium is halted and will not recover without a restart","cause":"disk","halted":true}`))
	case len(parts) == 1 && r.Method == http.MethodGet:
		for _, row := range f.rows {
			if row["id"] == parts[0] {
				_ = json.NewEncoder(w).Encode(map[string]any{"pr": row, "run_log": "14:02:11 fetch start\n"})
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"no such pr","code":"not_found"}`))
	case len(parts) == 2 && parts[1] == "findings" && r.Method == http.MethodGet:
		if parts[0] == "pr_outside" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"outside the review folder","code":"outside"}`))
			return
		}
		w.Header().Set("ETag", findingsETag)
		if r.Header.Get("If-None-Match") == findingsETag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Write([]byte(findingsRaw(parts[0])))
	case len(parts) == 3 && parts[1] == "findings" && r.Method == http.MethodPut:
		var in struct {
			Hash string `json:"hash"`
		}
		_ = json.Unmarshal(body, &in)
		if in.Hash != "7f1c" {
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte(`{"error":"that file changed while you were editing it. nothing was written.",` +
				`"code":"changed","text":"a && b < c\nnow","hash":"9e02"}`))
			return
		}
		w.Write([]byte(`{"ok":true,"hash":"4d4d","key":"` + parts[2] + `"}`))
	case len(parts) == 2 && parts[1] == "walker" && r.Method == http.MethodPost:
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"pr":{"id":"` + parts[0] + `","walker_task":"0f3a-card"},"task":"0f3a-card","launched":true}`))
	case len(parts) == 2 && r.Method == http.MethodPost:
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"pr":{"id":"` + parts[0] + `","walker_task":""}}`))
	default:
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"no such route","code":"not_found"}`))
	}
}

// pullsRow is a row as the room lists it.
func pullsRow(id, state, created string) map[string]any {
	return map[string]any{
		"id": id, "url": "https://github.com/openziti/tlsuv/pull/378", "org_repo": "openziti/tlsuv", "number": 378,
		"state": state, "created_at": created, "walker_task": "",
	}
}

// pullsDo sends a request through the hub and answers its status, body and headers.
func pullsDo(t *testing.T, method, url, body string, hdr map[string]string) (int, string, http.Header) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rd)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(raw), res.Header
}

// A SCOPED OR SINGLE-ROOM REQUEST FOR /v1/prs IS A BYTE PIPE: the method, the query, the body, If-None-Match and
// ETag, a 304 and an error body with its status all arrive as the room made them.
func TestPullsRoutesPassThroughToTheRoomUnchanged(t *testing.T) {
	room := &pullsRoom{rows: []map[string]any{pullsRow("pr_01k8x2m4q7", "ready", "2026-10-01T14:02:10Z")}}
	front, _, done := pair(t, room)
	defer done()

	// The list, with a query the hub must not touch.
	code, body, _ := pullsDo(t, http.MethodGet, front.URL+"/v1/prs?state=ready&state=failed&org_repo=openziti%2Ftlsuv", "", nil)
	if code != 200 || !strings.Contains(body, `"pr_01k8x2m4q7"`) || strings.Contains(body, `"room"`) {
		t.Fatalf("list = %d %s", code, body)
	}
	if room.count(http.MethodGet, "/v1/prs?state=ready&state=failed&org_repo=openziti%2Ftlsuv") != 1 {
		t.Fatalf("the query did not arrive as sent: %+v", room.requests())
	}

	// A POST with its body, and the room's 201.
	post := `{"url":"https://github.com/openziti/tlsuv/pull/378","why":"a security look","head":""}`
	code, body, _ = pullsDo(t, http.MethodPost, front.URL+"/v1/prs", post, map[string]string{"Content-Type": "application/json"})
	if code != http.StatusCreated || body != `{"pr":{"id":"pr_01k8x2m4q7","walker_task":""},"created":true}` {
		t.Fatalf("post = %d %s", code, body)
	}
	if got := room.requests()[len(room.requests())-1]; got.Body != post || got.Type != "application/json" {
		t.Fatalf("the room got %+v", got)
	}

	// Findings: the ETag arrives, a repeat with it is a 304 with no body, and the body is the room's bytes.
	code, body, h := pullsDo(t, http.MethodGet, front.URL+"/v1/prs/pr_01k8x2m4q7/findings", "", nil)
	if code != 200 || h.Get("ETag") != findingsETag || body != findingsRaw("pr_01k8x2m4q7") {
		t.Fatalf("findings = %d etag %q %s", code, h.Get("ETag"), body)
	}
	code, body, h = pullsDo(t, http.MethodGet, front.URL+"/v1/prs/pr_01k8x2m4q7/findings", "",
		map[string]string{"If-None-Match": findingsETag})
	if code != http.StatusNotModified || body != "" || h.Get("ETag") != findingsETag {
		t.Fatalf("repeat = %d etag %q body %q, want a bare 304", code, h.Get("ETag"), body)
	}
	last := room.requests()[len(room.requests())-1]
	if last.INM != findingsETag {
		t.Fatalf("If-None-Match did not reach the room: %+v", last)
	}

	// A PUT with a stale hash gets the room's 409 body, byte for byte, and the room saw the body byte for byte.
	put := `{"text":"a && b < c","hash":"stale","eol":"\n"}`
	code, body, _ = pullsDo(t, http.MethodPut, front.URL+"/v1/prs/pr_01k8x2m4q7/findings/f-3a9c01d4e2", put, nil)
	want := `{"error":"that file changed while you were editing it. nothing was written.",` +
		`"code":"changed","text":"a && b < c\nnow","hash":"9e02"}`
	if code != http.StatusConflict || body != want {
		t.Fatalf("stale put = %d %s", code, body)
	}
	if got := room.requests()[len(room.requests())-1]; got.Method != http.MethodPut || got.Body != put {
		t.Fatalf("the room got %+v", got)
	}

	// 403 and 503 keep their status and their JSON.
	code, body, _ = pullsDo(t, http.MethodGet, front.URL+"/v1/prs/pr_outside/findings", "", nil)
	if code != http.StatusForbidden || !strings.Contains(body, `"code":"outside"`) {
		t.Fatalf("outside = %d %s", code, body)
	}
	code, body, _ = pullsDo(t, http.MethodGet, front.URL+"/v1/prs/pr_halted", "", nil)
	if code != http.StatusServiceUnavailable || !strings.Contains(body, `"halted":true`) {
		t.Fatalf("halted = %d %s", code, body)
	}

	// The per-PR writes.
	for _, verb := range []string{"retry", "abort", "start"} {
		code, _, _ = pullsDo(t, http.MethodPost, front.URL+"/v1/prs/pr_01k8x2m4q7/"+verb, "", nil)
		if code != http.StatusAccepted {
			t.Fatalf("%s = %d", verb, code)
		}
	}
	code, body, _ = pullsDo(t, http.MethodPost, front.URL+"/v1/prs/pr_01k8x2m4q7/walker", `{"action":"launch"}`, nil)
	if code != http.StatusCreated || !strings.Contains(body, `"task":"0f3a-card"`) {
		t.Fatalf("walker = %d %s", code, body)
	}
}

// A ROOM MARKED FOR DELETION STARTS NO NEW REVIEW, like it starts no new launch. What is already on it carries on,
// so a retry, an abort and a walk mark are not refused.
func TestAMarkedRoomRefusesANewPullReviewAndNothingElse(t *testing.T) {
	seen := time.Now()
	stock := &remembering{
		rooms: []Known{{Name: "testroom", Attached: true, State: "marked-for-deletion", FirstSeen: &seen, LastSeen: &seen}},
		cards: map[string][]CardState{},
	}
	room := &pullsRoom{rows: []map[string]any{pullsRow("pr_01k8x2m4q7", "failed", "2026-10-01T14:02:10Z")}}
	front, _, done := pair(t, room)
	defer done()
	front.Config.Handler.(*Proxy).SetInventory(stock)

	code, body, _ := pullsDo(t, http.MethodPost, front.URL+"/v1/prs", `{"url":"x"}`, nil)
	if code != http.StatusConflict || !strings.Contains(body, "marked for deletion") {
		t.Fatalf("a new review on a marked room = %d %s", code, body)
	}
	if room.count(http.MethodPost, "/v1/prs") != 0 {
		t.Fatal("the refused review reached the room")
	}
	for _, path := range []string{"/v1/prs/pr_01k8x2m4q7/retry", "/v1/prs/pr_01k8x2m4q7/abort",
		"/v1/prs/pr_01k8x2m4q7/start"} {
		if code, body, _ := pullsDo(t, http.MethodPost, front.URL+path, "", nil); code >= 400 {
			t.Fatalf("%s on a marked room = %d %s, want the room's answer", path, code, body)
		}
	}
	// A walker LAUNCH makes a card, so it is new work: an empty body is a launch, as is an explicit one. `set` and
	// `clear` only record a card that exists and carry on.
	const walker = "/v1/prs/pr_01k8x2m4q7/walker"
	for _, body := range []string{"", `{"action":"launch"}`, `{}`} {
		code, answer, _ := pullsDo(t, http.MethodPost, front.URL+walker, body, nil)
		if code != http.StatusConflict || !strings.Contains(answer, "marked for deletion") {
			t.Fatalf("walker launch %q on a marked room = %d %s", body, code, answer)
		}
	}
	if room.count(http.MethodPost, walker) != 0 {
		t.Fatal("a refused walker launch reached the room")
	}
	for _, body := range []string{`{"action":"set","task":"card-1"}`, `{"action":"clear"}`} {
		if code, answer, _ := pullsDo(t, http.MethodPost, front.URL+walker, body, nil); code >= 400 {
			t.Fatalf("walker %s on a marked room = %d %s, want the room's answer", body, code, answer)
		}
	}
	if room.count(http.MethodPost, walker) != 2 {
		t.Fatalf("set and clear did not both reach the room with their bodies: %+v", room.requests())
	}
	if code, _, _ := pullsDo(t, http.MethodGet, front.URL+"/v1/prs", "", nil); code != 200 {
		t.Fatalf("the list on a marked room = %d", code)
	}
}

// The body is not capped or rewritten on the way in: a 3 MiB PUT reaches the room whole, and the room's own limit
// answers for it.
func TestAPullsWriteIsNotCappedByTheHub(t *testing.T) {
	room := &pullsRoom{}
	front, _, done := pair(t, room)
	defer done()
	big := `{"text":"` + strings.Repeat("x", 3<<20) + `","hash":"stale"}`
	code, _, _ := pullsDo(t, http.MethodPut, front.URL+"/v1/prs/pr_a/findings/f-1", big, nil)
	if code != http.StatusConflict {
		t.Fatalf("a big put = %d", code)
	}
	got := room.requests()
	if len(got) == 0 || !bytes.Equal([]byte(got[len(got)-1].Body), []byte(big)) {
		t.Fatal("the room did not get the whole body")
	}
}

// pump says a line to a room's stream until the test is done, because the hub's pump takes a moment to attach and
// nothing is replayed.
func pump(say chan<- string, lines ...string) (stop func()) {
	quit := make(chan struct{})
	go func() {
		for i := 0; i < 60; i++ {
			for _, l := range lines {
				select {
				case say <- l:
				case <-quit:
					return
				}
			}
			select {
			case <-quit:
				return
			case <-time.After(100 * time.Millisecond):
			}
		}
	}()
	return func() { close(quit) }
}

// THE `pr` EVENT REACHES THE BOARD with one room attached, on the merged stream and on the room's own, with the
// room's name and the room's bytes.
func TestAPrEventArrivesUntouchedFromASingleRoom(t *testing.T) {
	say := make(chan string, 4)
	front, _, done := pair(t, streamer(say))
	defer done()
	const row = `{"pr":{"id":"pr_01k8x2m4q7","state":"running","walker_task":"0f3a-card","cost_usd":0.71}}`
	for _, path := range []string{"/v1/events/hub", "/v1/events/room/testroom", "/v1/events"} {
		ch, shut := listen(t, front.URL+path)
		stop := pump(say, sse("pr", row))
		e := waitEvent(t, ch, "pr")
		stop()
		shut()
		if string(e.Data) != row {
			t.Fatalf("%s: the pr event came out %s, want it untouched", path, e.Data)
		}
	}
}

// IN THE ALL VIEW a `pr` event is tagged like a card event: its row's id as `room~pr_...`, its walker's card id as
// `room~card`, an empty walker left empty, and the room on it. The scoped stream of the same room stays bare.
func TestAPrEventIsTaggedInTheAllView(t *testing.T) {
	a, b := make(chan string, 4), make(chan string, 4)
	front, _, done := two(t, streamer(a), streamer(b))
	defer done()
	all, shutAll := listen(t, front.URL+"/v1/events/hub")
	defer shutAll()
	one, shutOne := listen(t, front.URL+"/v1/events/room/beta")
	defer shutOne()
	stopA := pump(a, sse("pr", `{"pr":{"id":"pr_a1","walker_task":"card-a"}}`))
	defer stopA()
	stopB := pump(b, sse("pr", `{"pr":{"id":"pr_b1","walker_task":""}}`))
	defer stopB()

	seen := map[string]map[string]any{}
	deadline := time.After(8 * time.Second)
	for len(seen) < 2 {
		select {
		case e := <-all:
			if e.Kind != "pr" {
				continue
			}
			obj := fields(t, e.Data)
			row, _ := obj["pr"].(map[string]any)
			if row == nil {
				t.Fatalf("the row went missing: %s", e.Data)
			}
			seen[row["room"].(string)] = row
		case <-deadline:
			t.Fatalf("only saw %v", seen)
		}
	}
	if seen["alpha"]["id"] != "alpha~pr_a1" || seen["alpha"]["walker_task"] != "alpha~card-a" {
		t.Errorf("alpha's row came through as %v", seen["alpha"])
	}
	if seen["beta"]["id"] != "beta~pr_b1" || seen["beta"]["walker_task"] != "" {
		t.Errorf("beta's row came through as %v", seen["beta"])
	}
	// The room's own stream is that room's, bare.
	e := waitEvent(t, one, "pr")
	if string(e.Data) != `{"pr":{"id":"pr_b1","walker_task":""}}` {
		t.Errorf("the scoped stream was rewritten: %s", e.Data)
	}
}

// THE FAN-IN DROPS NO KIND IT HAS NOT HEARD OF, and a row sent bare, with no `pr` wrapper, is tagged at the top.
func TestTheFanInCarriesAnEventKindItDoesNotKnow(t *testing.T) {
	a, b := make(chan string, 4), make(chan string, 4)
	front, _, done := two(t, streamer(a), streamer(b))
	defer done()
	ch, shut := listen(t, front.URL+"/v1/events/hub")
	defer shut()
	stop := pump(a, sse("zebra-crossing", `{"id":"z1","n":3}`), sse("pr", `{"id":"pr_bare","walker_task":"c9"}`))
	defer stop()

	z := waitEvent(t, ch, "zebra-crossing")
	if o := fields(t, z.Data); o["id"] != "z1" || o["room"] != "alpha" || o["n"] != float64(3) {
		t.Errorf("an unknown kind came out %s", z.Data)
	}
	p := waitEvent(t, ch, "pr")
	if o := fields(t, p.Data); o["id"] != "alpha~pr_bare" || o["walker_task"] != "alpha~c9" {
		t.Errorf("a bare row came out %s", p.Data)
	}
}

// ── the ALL view ─────────────────────────────────────────

// jsonOf decodes a body into an object.
func jsonOf(t *testing.T, raw string) map[string]any {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		t.Fatalf("not an object: %s", raw)
	}
	return obj
}

// listIDs is the ids of a merged list, in order.
func listIDs(t *testing.T, obj map[string]any) []string {
	t.Helper()
	var ids []string
	rows, _ := obj["prs"].([]any)
	for _, r := range rows {
		ids = append(ids, r.(map[string]any)["id"].(string))
	}
	return ids
}

func allPulls() (*pullsRoom, *pullsRoom) {
	a := &pullsRoom{rows: []map[string]any{
		pullsRow("pr_a1", "ready", "2026-10-01T14:00:00Z"),
		pullsRow("pr_a2", "running", "2026-10-01T16:00:00Z"),
	}}
	a.rows[1]["walker_task"] = "card-a2"
	b := &pullsRoom{rows: []map[string]any{
		pullsRow("pr_b1", "failed", "2026-10-01T14:00:00Z"),
		pullsRow("pr_b2", "ready", "2026-10-01T15:00:00Z"),
	}}
	return a, b
}

// THE ALL VIEW'S LIST: every room's rows with the room and a tagged id, a walker's card id tagged, the counts and the
// nav count summed, newest first with the tagged id breaking a tie, and the query sent to every room as it came.
func TestTheAllViewMergesEveryRoomsPulls(t *testing.T) {
	a, b := allPulls()
	front, _, done := two(t, a, b)
	defer done()

	code, body, _ := pullsDo(t, http.MethodGet, front.URL+"/v1/prs?state=ready&state=running&archived=1", "", nil)
	if code != 200 {
		t.Fatalf("list = %d %s", code, body)
	}
	obj := jsonOf(t, body)
	// pr_a2 16:00, pr_b2 15:00, then the 14:00 tie, where the larger tagged id comes first.
	want := []string{"alpha~pr_a2", "beta~pr_b2", "beta~pr_b1", "alpha~pr_a1"}
	if got := listIDs(t, obj); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", got, want)
	}
	rows := obj["prs"].([]any)
	first := rows[0].(map[string]any)
	if first["room"] != "alpha" || first["walker_task"] != "alpha~card-a2" {
		t.Errorf("the first row = %v", first)
	}
	if second := rows[1].(map[string]any); second["room"] != "beta" || second["walker_task"] != "" {
		t.Errorf("an empty walker_task was touched: %v", second)
	}
	counts := obj["counts"].(map[string]any)
	if counts["ready"] != float64(2) || counts["running"] != float64(1) || counts["failed"] != float64(1) ||
		counts["queued"] != float64(0) || counts["aborted"] != float64(0) || counts["fetching"] != float64(0) {
		t.Errorf("counts = %v", counts)
	}
	if obj["nav_count"] != float64(3) {
		t.Errorf("nav_count = %v, want ready + failed over both rooms", obj["nav_count"])
	}
	if _, has := obj["rooms_quiet"]; has {
		t.Errorf("a room was called quiet: %v", obj["rooms_quiet"])
	}
	const q = "/v1/prs?state=ready&state=running&archived=1"
	if a.count(http.MethodGet, q) != 1 || b.count(http.MethodGet, q) != 1 {
		t.Errorf("the query did not reach both rooms as sent: %+v %+v", a.requests(), b.requests())
	}
}

// A room that errors is quiet and the rest draws. A room on a build with no pulls answers 404, which is an answer: it
// adds nothing, is not quiet, and is named apart.
func TestAQuietPullsRoomDoesNotEmptyTheList(t *testing.T) {
	a, b := allPulls()
	b.sick = true
	front, _, done := two(t, a, b)
	code, body, _ := pullsDo(t, http.MethodGet, front.URL+"/v1/prs", "", nil)
	done()
	obj := jsonOf(t, body)
	if code != 200 || strings.Join(listIDs(t, obj), ",") != "alpha~pr_a2,alpha~pr_a1" {
		t.Fatalf("a sick room emptied the list: %d %s", code, body)
	}
	if q, _ := obj["rooms_quiet"].([]any); len(q) != 1 || q[0] != "beta" {
		t.Errorf("rooms_quiet = %v, want beta", obj["rooms_quiet"])
	}
	if obj["counts"].(map[string]any)["failed"] != float64(0) || obj["nav_count"] != float64(1) {
		t.Errorf("a quiet room was counted: %v", obj)
	}

	a, b = allPulls()
	b.old = true
	front, _, done = two(t, a, b)
	defer done()
	_, body, _ = pullsDo(t, http.MethodGet, front.URL+"/v1/prs", "", nil)
	obj = jsonOf(t, body)
	if strings.Join(listIDs(t, obj), ",") != "alpha~pr_a2,alpha~pr_a1" {
		t.Fatalf("a room with no pulls changed the list: %s", body)
	}
	if _, has := obj["rooms_quiet"]; has {
		t.Errorf("a 404 was called quiet: %v", obj["rooms_quiet"])
	}
	if w, _ := obj["rooms_without"].([]any); len(w) != 1 || w[0] != "beta" {
		t.Errorf("rooms_without = %v, want beta", obj["rooms_without"])
	}
}

// With no room answering 200 there is no pulls view, and the board opens the tab on any 200 with a `prs` array, so the
// answer is the contract's 404 and names the rooms asked. Two rooms without the store, and two that error, are both it.
func TestNoRoomServingPullsIsA404NotAnEmptyList(t *testing.T) {
	a, b := allPulls()
	a.old, b.old = true, true
	front, _, done := two(t, a, b)
	code, body, _ := pullsDo(t, http.MethodGet, front.URL+"/v1/prs", "", nil)
	done()
	obj := jsonOf(t, body)
	if code != http.StatusNotFound || obj["code"] != "not_found" || obj["error"] == "" || obj["prs"] != nil {
		t.Fatalf("two rooms without pulls = %d %s", code, body)
	}
	if w, _ := obj["rooms_without"].([]any); len(w) != 2 || w[0] != "alpha" || w[1] != "beta" {
		t.Errorf("rooms_without = %v, want alpha and beta", obj["rooms_without"])
	}
	if q, ok := obj["rooms_quiet"].([]any); !ok || len(q) != 0 {
		t.Errorf("rooms_quiet = %v, want an empty list", obj["rooms_quiet"])
	}

	a, b = allPulls()
	a.sick, b.sick = true, true
	front, _, done = two(t, a, b)
	defer done()
	code, body, _ = pullsDo(t, http.MethodGet, front.URL+"/v1/prs", "", nil)
	obj = jsonOf(t, body)
	if q, _ := obj["rooms_quiet"].([]any); code != http.StatusNotFound || len(q) != 2 {
		t.Fatalf("two sick rooms = %d %s", code, body)
	}
	if msg, _ := obj["error"].(string); !strings.Contains(msg, "answered") || strings.Contains(msg, "serves") {
		t.Errorf("sick rooms were reported as a missing feature: %q", msg)
	}
}

// With every room empty the answer is the contract's empty index, never null.
func TestAnEmptyAllViewPullsListIsNotNull(t *testing.T) {
	front, _, done := two(t, &pullsRoom{}, &pullsRoom{})
	defer done()
	_, body, _ := pullsDo(t, http.MethodGet, front.URL+"/v1/prs", "", nil)
	if !strings.Contains(body, `"prs":[]`) || !strings.Contains(body, `"nav_count":0`) {
		t.Fatalf("empty list = %s", body)
	}
}

// A new review with no room named is PLACED by the hub on the least busy room (here a tie, so the lower name), where it
// used to be the room question. Never fanned out: exactly one room gets it. See prclaim.go.
func TestANewReviewInTheAllViewIsPlacedOnOneRoom(t *testing.T) {
	a, b := allPulls()
	front, _, done := two(t, a, b)
	defer done()
	code, body, _ := pullsDo(t, http.MethodPost, front.URL+"/v1/prs", `{"url":"https://github.com/openziti/tlsuv/pull/378"}`, nil)
	if code != http.StatusCreated || a.count(http.MethodPost, "/v1/prs") != 1 || b.count(http.MethodPost, "/v1/prs") != 0 {
		t.Fatalf("post = %d %s, alpha %d beta %d", code, body, a.count(http.MethodPost, "/v1/prs"), b.count(http.MethodPost, "/v1/prs"))
	}
	// Named, it lands on that room alone.
	code, _, _ = pullsDo(t, http.MethodPost, front.URL+"/v1/prs", `{"url":"u"}`, map[string]string{RoomHeader: "beta"})
	if code != http.StatusCreated || b.count(http.MethodPost, "/v1/prs") != 1 || a.count(http.MethodPost, "/v1/prs") != 1 {
		t.Fatalf("a named room did not get it alone: %d", code)
	}
}

// A TAGGED ROW GOES TO ITS ROOM WITH THE BARE ID, and what comes back is tagged. Its findings, its hash and its
// error bodies are the room's bytes.
func TestATaggedPullGoesToItsRoomAndComesBackTagged(t *testing.T) {
	a, b := allPulls()
	front, _, done := two(t, a, b)
	defer done()

	code, body, _ := pullsDo(t, http.MethodGet, front.URL+"/v1/prs/beta~pr_b1", "", nil)
	if code != 200 {
		t.Fatalf("detail = %d %s", code, body)
	}
	row := jsonOf(t, body)["pr"].(map[string]any)
	if row["id"] != "beta~pr_b1" || row["room"] != "beta" {
		t.Errorf("the row came back %v", row)
	}
	if b.count(http.MethodGet, "/v1/prs/pr_b1") != 1 || len(a.requests()) != 0 {
		t.Fatalf("the click did not land on beta alone, with the bare id: a %+v b %+v", a.requests(), b.requests())
	}

	// The findings: pr tagged, everything else the room's bytes (the `&&` and `<` a re-marshal would escape).
	code, body, h := pullsDo(t, http.MethodGet, front.URL+"/v1/prs/beta~pr_b1/findings", "", nil)
	if code != 200 || h.Get("ETag") != findingsETag {
		t.Fatalf("findings = %d etag %q", code, h.Get("ETag"))
	}
	const list = `"findings":[{"key":"f-3a9c01d4e2","text":"a && b < c","hash":"7f1c","position":1}]`
	if !strings.Contains(body, list) {
		t.Fatalf("the findings were rewritten: %s", body)
	}
	if pr := jsonOf(t, body)["pr"].(map[string]any); pr["id"] != "beta~pr_b1" || pr["walker_task"] != "" || pr["room"] != "beta" {
		t.Errorf("findings pr = %v", pr)
	}
	// The repeat is a bare 304, ETag and all.
	code, body, h = pullsDo(t, http.MethodGet, front.URL+"/v1/prs/beta~pr_b1/findings", "",
		map[string]string{"If-None-Match": findingsETag})
	if code != http.StatusNotModified || body != "" || h.Get("ETag") != findingsETag {
		t.Fatalf("repeat = %d %q %q", code, h.Get("ETag"), body)
	}

	// A stale write: the room's 409 body unchanged, and the room's request body unchanged.
	put := `{"text":"a && b < c","hash":"stale","eol":"\r\n"}`
	code, body, _ = pullsDo(t, http.MethodPut, front.URL+"/v1/prs/beta~pr_b1/findings/f-3a9c01d4e2", put, nil)
	if code != http.StatusConflict || !strings.Contains(body, `"hash":"9e02"`) || !strings.Contains(body, `"text":"a && b < c\nnow"`) {
		t.Fatalf("stale put = %d %s", code, body)
	}
	if got := b.requests()[len(b.requests())-1]; got.Method != http.MethodPut ||
		got.URI != "/v1/prs/pr_b1/findings/f-3a9c01d4e2" || got.Body != put {
		t.Fatalf("beta got %+v", got)
	}

	// A walker answer tags the row and the card.
	code, body, _ = pullsDo(t, http.MethodPost, front.URL+"/v1/prs/beta~pr_b1/walker", `{"action":"launch"}`, nil)
	obj := jsonOf(t, body)
	if code != http.StatusCreated || obj["task"] != "beta~0f3a-card" ||
		obj["pr"].(map[string]any)["walker_task"] != "beta~0f3a-card" {
		t.Fatalf("walker = %d %s", code, body)
	}
}

// A PLAIN ID WITH TWO ROOMS IS LOOKED FOR, once: the holder is cached, so a repeat asks nobody. A header names the
// room outright and asks nobody. An id no room holds is a 404 naming the rooms asked.
func TestAPlainPullIdFindsItsRoom(t *testing.T) {
	a, b := allPulls()
	front, _, done := two(t, a, b)
	defer done()

	for i := 0; i < 3; i++ {
		code, body, _ := pullsDo(t, http.MethodGet, front.URL+"/v1/prs/pr_b2", "", nil)
		if code != 200 || jsonOf(t, body)["pr"].(map[string]any)["id"] != "pr_b2" {
			t.Fatalf("detail = %d %s", code, body)
		}
	}
	// One probe per room to find it, then the three requests themselves on beta. Alpha was asked once.
	if got := a.count(http.MethodGet, "/v1/prs/pr_b2"); got > 1 {
		t.Errorf("alpha was asked %d times, want the holder cached after one lookup", got)
	}
	if got := b.count(http.MethodGet, "/v1/prs/pr_b2"); got < 3 {
		t.Errorf("beta got %d requests for the row", got)
	}

	before := len(a.requests())
	code, _, _ := pullsDo(t, http.MethodGet, front.URL+"/v1/prs/pr_a1", "", map[string]string{RoomHeader: "alpha"})
	if code != 200 || len(a.requests()) != before+1 {
		t.Errorf("a named room was searched: %d, alpha saw %d", code, len(a.requests())-before)
	}

	code, body, _ := pullsDo(t, http.MethodGet, front.URL+"/v1/prs/pr_nowhere/retry", "", nil)
	obj := jsonOf(t, body)
	if code != http.StatusNotFound || obj["code"] != "not_found" ||
		!strings.Contains(obj["error"].(string), "pr_nowhere") || !strings.Contains(obj["error"].(string), "alpha") ||
		!strings.Contains(obj["error"].(string), "beta") {
		t.Fatalf("an unheld id = %d %s", code, body)
	}
}

// ── the acceptance test ──────────────────────────────────

// withEvents is a fake room that serves the pulls routes and a stream that says what it is told to.
func withEvents(say <-chan string, room *pullsRoom) http.Handler {
	stream := streamer(say)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/events" {
			stream.ServeHTTP(w, r)
			return
		}
		room.ServeHTTP(w, r)
	})
}

// A PR'S ROW OPENS ITS WALK THROUGH THE HUB. One fake room holds the review of openziti/tlsuv#378. Through the hub,
// scoped to that room and then in the ALL view with a second room attached, the list shows the row, the detail opens,
// the findings open with their ETag and answer 304 on a repeat, the `pr` event arrives with the right id, and a write
// quoting a stale hash gets the room's 409 body back unchanged.
func TestAPullsRowOpensItsWalkThroughTheHub(t *testing.T) {
	const prURL = "https://github.com/openziti/tlsuv/pull/378"
	row := pullsRow("pr_01k8x2m4q7", "ready", "2026-10-01T14:02:10Z")
	row["url"], row["title"] = prURL, "tls engine: session resumption on reconnect"
	alpha := &pullsRoom{rows: []map[string]any{row}}
	beta := &pullsRoom{}
	sayA, sayB := make(chan string, 4), make(chan string, 4)
	front, _, done := two(t, withEvents(sayA, alpha), withEvents(sayB, beta))
	defer done()

	const stale = `{"text":"a && b < c","hash":"stale","eol":"\n"}`
	const staleBody = `{"error":"that file changed while you were editing it. nothing was written.",` +
		`"code":"changed","text":"a && b < c\nnow","hash":"9e02"}`

	// walk is the whole path, against one spelling of the row's id and one way of naming the room. `id` is what
	// the list handed the board, and `hdr` is how the board names its room, if it does.
	walk := func(t *testing.T, hdr map[string]string, events, wantID string) {
		t.Helper()
		// The list shows the row.
		code, body, _ := pullsDo(t, http.MethodGet, front.URL+"/v1/prs", "", hdr)
		if code != 200 || !strings.Contains(body, prURL) {
			t.Fatalf("the list = %d %s", code, body)
		}
		listed := jsonOf(t, body)["prs"].([]any)[0].(map[string]any)
		id, _ := listed["id"].(string)
		if id != wantID {
			t.Fatalf("the list handed out the id %q, want %q", id, wantID)
		}
		// The detail opens from that id and says whose it is.
		code, body, _ = pullsDo(t, http.MethodGet, front.URL+"/v1/prs/"+id, "", hdr)
		detail := jsonOf(t, body)
		if code != 200 || detail["pr"].(map[string]any)["id"] != wantID || detail["run_log"] == nil {
			t.Fatalf("the detail = %d %s", code, body)
		}
		// The findings open with their ETag and answer 304 on a repeat.
		code, body, h := pullsDo(t, http.MethodGet, front.URL+"/v1/prs/"+id+"/findings", "", hdr)
		if code != 200 || h.Get("ETag") != findingsETag || !strings.Contains(body, `"hash":"7f1c"`) {
			t.Fatalf("the findings = %d etag %q %s", code, h.Get("ETag"), body)
		}
		hdr304 := map[string]string{"If-None-Match": h.Get("ETag")}
		for k, v := range hdr {
			hdr304[k] = v
		}
		if code, body, _ = pullsDo(t, http.MethodGet, front.URL+"/v1/prs/"+id+"/findings", "", hdr304); code != http.StatusNotModified || body != "" {
			t.Fatalf("the repeat = %d %q, want a 304", code, body)
		}
		// A write on a stale hash is the room's 409, byte for byte.
		code, body, _ = pullsDo(t, http.MethodPut, front.URL+"/v1/prs/"+id+"/findings/f-3a9c01d4e2", stale, hdr)
		if code != http.StatusConflict || body != staleBody {
			t.Fatalf("the stale write = %d %s", code, body)
		}
		// The event arrives with the id the list used, so the board replaces the row it holds.
		ch, shut := listen(t, front.URL+events)
		defer shut()
		stop := pump(sayA, sse("pr", `{"pr":{"id":"pr_01k8x2m4q7","state":"running","run_state":"panel"}}`))
		defer stop()
		ev := waitEvent(t, ch, "pr")
		if got := fields(t, ev.Data)["pr"].(map[string]any)["id"]; got != wantID {
			t.Fatalf("the event named %v, want %q", got, wantID)
		}
	}

	t.Run("scoped to the room", func(t *testing.T) {
		walk(t, map[string]string{RoomHeader: "alpha"}, "/v1/events/room/alpha", "pr_01k8x2m4q7")
	})
	t.Run("in the all view with a second room", func(t *testing.T) {
		walk(t, nil, "/v1/events/hub", "alpha~pr_01k8x2m4q7")
		// The other room holds nothing and was not asked to open this row.
		for _, q := range beta.requests() {
			if strings.Contains(q.URI, "pr_01k8x2m4q7/") || q.Method == http.MethodPut {
				t.Errorf("beta was asked about alpha's row: %+v", q)
			}
		}
	})
}
