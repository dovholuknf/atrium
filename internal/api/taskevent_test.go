package api

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// nextTaskEvent reads the stream until a "task" event and returns its data.
func nextTaskEvent(t *testing.T, sc *bufio.Scanner) string {
	t.Helper()
	kind := ""
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "event: ") {
			kind = strings.TrimPrefix(line, "event: ")
		}
		if strings.HasPrefix(line, "data: ") && kind == "task" {
			return strings.TrimPrefix(line, "data: ")
		}
	}
	t.Fatal("stream ended before a task event")
	return ""
}

func listRow(t *testing.T, srv *Server, id string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/tasks", nil))
	var body struct {
		Tasks []json.RawMessage `json:"tasks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, raw := range body.Tasks {
		var h struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(raw, &h)
		if h.ID == id {
			return string(raw)
		}
	}
	t.Fatalf("card %s not in list", id)
	return ""
}

func eventStream(t *testing.T, srv *Server) *bufio.Scanner {
	t.Helper()
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	resp, err := http.Get(ts.URL + "/v1/events")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return bufio.NewScanner(resp.Body)
}

func sameJSON(t *testing.T, a, b string) {
	t.Helper()
	var x, y map[string]any
	if err := json.Unmarshal([]byte(a), &x); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(b), &y); err != nil {
		t.Fatal(err)
	}
	ja, _ := json.Marshal(x)
	jb, _ := json.Marshal(y)
	if string(ja) != string(jb) {
		t.Fatalf("event and list row differ:\nevent: %s\nlist:  %s", ja, jb)
	}
}

func TestTaskEventEqualsListRow(t *testing.T) {
	srv, st, work := fileServer(t)
	busy := cardIn(t, st, work)
	other := filepath.Join(work, "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	plain := cardIn(t, st, other)

	if _, err := st.AddAsk(busy.ID, "first?", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddAsk(busy.ID, "second?", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RecordSay(store.Say{ToTask: busy.ID, ReplyWant: true, State: store.SayDelivered}, "hi"); err != nil {
		t.Fatal(err)
	}
	if err := st.NoteTurnEnded(busy.ID, store.TurnQuestions{Known: true, Block: true, List: []string{"q"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.MarkSeen(busy.ID, store.SeenViewed, nil); err != nil {
		t.Fatal(err)
	}

	sc := eventStream(t, srv)
	time.Sleep(100 * time.Millisecond)
	for _, c := range []*store.Task{busy, plain} {
		got, err := st.Get(c.ID)
		if err != nil {
			t.Fatal(err)
		}
		srv.PublishTask(got)
		ev := nextTaskEvent(t, sc)
		sameJSON(t, ev, listRow(t, srv, c.ID))
		if c == busy {
			for _, want := range []string{`"asks_open":2`, `"replies_owed":1`, `"seen":`, `"row":1`} {
				if !strings.Contains(ev, want) {
					t.Fatalf("busy card event lacks %s: %s", want, ev)
				}
			}
		}
	}
}

func TestTaskEventSurvivesFailedDecoration(t *testing.T) {
	srv, st, work := fileServer(t)
	card := cardIn(t, st, work)
	sc := eventStream(t, srv)
	time.Sleep(100 * time.Millisecond)
	st.Close()
	srv.PublishTask(card)
	ev := nextTaskEvent(t, sc)
	if !strings.Contains(ev, card.ID) {
		t.Fatalf("event lost the card: %s", ev)
	}
}

func TestNoTaskBroadcastOutsideTheHelper(t *testing.T) {
	files, _ := filepath.Glob("../*/*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		n := strings.Count(string(b), `Broadcast("task"`)
		if n > 0 && !strings.HasSuffix(filepath.ToSlash(f), "api/api.go") || n > 1 {
			t.Errorf("%s has %d Broadcast(\"task\" call(s)", f, n)
		}
	}
}
