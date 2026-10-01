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
// answer what docs/rnd/pulls-api.md says a room answers.

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
		"/v1/prs/pr_01k8x2m4q7/start", "/v1/prs/pr_01k8x2m4q7/walker"} {
		if code, body, _ := pullsDo(t, http.MethodPost, front.URL+path, "", nil); code >= 400 {
			t.Fatalf("%s on a marked room = %d %s, want the room's answer", path, code, body)
		}
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
