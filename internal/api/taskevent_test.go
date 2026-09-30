package api

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func TestTaskEventFromAClosedStoreSendsNothing(t *testing.T) {
	srv, st, work := fileServer(t)
	card := cardIn(t, st, work)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/v1/events")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	st.Close()
	srv.PublishTask(card)
	if evs := readTaskEvents(resp, 3*taskCoalesce); len(evs) != 0 {
		t.Fatalf("a card the store cannot read was sent: %v", evs)
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

func readTaskEvents(resp *http.Response, d time.Duration) []string {
	var (
		mu  sync.Mutex
		got []string
	)
	done := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(resp.Body)
		kind := ""
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "event: ") {
				kind = strings.TrimPrefix(line, "event: ")
			}
			if strings.HasPrefix(line, "data: ") && kind == "task" {
				mu.Lock()
				got = append(got, strings.TrimPrefix(line, "data: "))
				mu.Unlock()
			}
			select {
			case <-done:
				return
			default:
			}
		}
	}()
	time.Sleep(d)
	close(done)
	resp.Body.Close()
	mu.Lock()
	defer mu.Unlock()
	return got
}

func TestPublishTaskCoalescesABurstToTheLatestState(t *testing.T) {
	srv, st, work := fileServer(t)
	card := cardIn(t, st, work)
	other := filepath.Join(work, "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	card2, _, err := st.Register(store.Observed{WireName: "second", Worktree: filepath.ToSlash(other), Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/v1/events")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	for i := 0; i < 10; i++ {
		if _, err := st.AddAsk(card.ID, "q?", ""); err != nil {
			t.Fatal(err)
		}
		got, _ := st.Get(card.ID)
		srv.PublishTask(got)
	}
	got2, _ := st.Get(card2.ID)
	srv.PublishTask(got2)

	evs := readTaskEvents(resp, 3*taskCoalesce)
	if len(evs) != 2 {
		t.Fatalf("want one event per card (2), got %d: %v", len(evs), evs)
	}
	for _, ev := range evs {
		if strings.Contains(ev, card.ID) && !strings.Contains(ev, `"asks_open":10`) {
			t.Fatalf("burst event is not the latest state: %s", ev)
		}
	}
}

func TestPublishTaskForADeletedCardSendsNoTaskEvent(t *testing.T) {
	srv, st, work := fileServer(t)
	card := cardIn(t, st, work)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/v1/events")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	srv.PublishTask(card)
	if err := st.Forget(card.ID); err != nil {
		t.Fatal(err)
	}
	if evs := readTaskEvents(resp, 3*taskCoalesce); len(evs) != 0 {
		t.Fatalf("a deleted card was sent again: %v", evs)
	}
}

func TestPublishTaskPendingStopsWithTheServer(t *testing.T) {
	srv, st, work := fileServer(t)
	card := cardIn(t, st, work)
	srv.PublishTask(card)
	srv.Close()
	srv.pend.mu.Lock()
	n := len(srv.pend.timers)
	srv.pend.mu.Unlock()
	if n != 0 {
		t.Fatalf("%d timers left after Close", n)
	}
	srv.PublishTask(card) // no panic, no timer
	srv.pend.mu.Lock()
	n = len(srv.pend.timers)
	srv.pend.mu.Unlock()
	if n != 0 {
		t.Fatal("a publish after Close armed a timer")
	}
}
