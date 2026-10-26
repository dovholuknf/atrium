//go:build integration

package link

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

// taskRig's control server answers /v1/ as the room that takes task launches, and /_hub/backlog/ as the hub.
type taskRig struct {
	*itemRig
	c        *controlMCP
	mu       sync.Mutex
	launches []string
	answer   func(w http.ResponseWriter) bool
}

func newTaskRig(t *testing.T) *taskRig {
	t.Helper()
	x := &taskRig{itemRig: newItemRig(t)}
	if _, err := x.st.ItemFile(hubstore.ItemNew{ID: "f-x", Dept: "test", Title: "the data item",
		Body: "THE BACKLOG BODY"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := x.st.PutItem(hubstore.Item{ID: "it-b", Room: "sg4", Card: "c1", Generation: 1, State: "running",
		Revision: 1, OwnerKind: hubstore.OwnerHuman, WorkID: "f-x"}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/launch/task":
			raw, _ := io.ReadAll(r.Body)
			x.mu.Lock()
			x.launches = append(x.launches, string(raw))
			answer := x.answer
			x.mu.Unlock()
			if answer != nil && answer(w) {
				return
			}
			_, _ = w.Write([]byte(`{"id":"c-task","wire_name":"worker"}`))
		case r.URL.Path == "/v1/tasks":
			_, _ = w.Write([]byte(`{"tasks":[]}`))
		case strings.HasPrefix(r.URL.Path, "/_hub/backlog/"):
			_, _ = w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	x.c = &controlMCP{board: srv.URL, client: srv.Client()}
	x.p.mu.Lock()
	x.p.ctl = x.c
	x.p.mu.Unlock()
	return x
}

func (x *taskRig) add(t *testing.T, title string) hubstore.Task {
	t.Helper()
	got, err := x.st.TaskAdd(hubstore.TaskInput{BacklogID: "f-x", Title: title, Brief: "do " + title,
		Outputs: []string{"out.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func (x *taskRig) sent() []string {
	x.mu.Lock()
	defer x.mu.Unlock()
	return append([]string(nil), x.launches...)
}

func (x *taskRig) state(t *testing.T, id string) hubstore.Task {
	t.Helper()
	got, err := x.st.TaskGet("f-x", id)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func (x *taskRig) capture(t *testing.T, id string, recap, learned string) {
	t.Helper()
	got := x.state(t, id)
	e := hubstore.TaskEvent{BacklogID: "f-x", TaskID: id, Attempt: got.Attempt, Card: got.Card, Recap: recap, Learned: learned}
	if _, applied, err := x.st.TaskCapturedAs(e); err != nil || !applied {
		t.Fatalf("capture %s: %v %v", id, applied, err)
	}
}

func TestTheSchedulerStartsOneTaskOfAnItemAtATimeInTheOrderAdded(t *testing.T) {
	x := newTaskRig(t)
	x.add(t, "first")
	x.add(t, "second")
	x.p.taskSchedule(context.Background())
	x.p.taskSchedule(context.Background())
	if got := x.sent(); len(got) != 1 || !strings.Contains(got[0], `"task_id":"t1"`) {
		t.Fatalf("launches: %v", got)
	}
	if got := x.state(t, "t1"); got.State != hubstore.TaskRunning || got.Card != "sg4~c-task" || got.Attempt != 1 {
		t.Fatalf("t1: %+v", got)
	}
	if got := x.state(t, "t2"); got.State != hubstore.TaskPending {
		t.Fatalf("t2 started beside t1: %+v", got)
	}
	x.capture(t, "t1", "T1 RECAP", "the csv has a header")
	x.p.taskSchedule(context.Background())
	if got := x.sent(); len(got) != 2 || !strings.Contains(got[1], `"task_id":"t2"`) {
		t.Fatalf("launches after t1 was captured: %v", got)
	}
}

func TestATaskLaunchCarriesItsBriefTheContextAndLearningsAndNothingElseOfTheItem(t *testing.T) {
	x := newTaskRig(t)
	if err := x.st.SetTaskContext("f-x", "the data lives in a.csv"); err != nil {
		t.Fatal(err)
	}
	x.add(t, "first")
	x.add(t, "second")
	x.p.taskSchedule(context.Background())
	x.capture(t, "t1", "T1 RECAP", "the csv has a header")
	x.p.taskSchedule(context.Background())
	got := x.sent()
	if len(got) != 2 {
		t.Fatalf("launches: %v", got)
	}
	var body struct {
		Task taskBlock `json:"task"`
		Tags []string  `json:"tags"`
		Item bool      `json:"item"`
	}
	if err := json.Unmarshal([]byte(got[1]), &body); err != nil {
		t.Fatal(err)
	}
	b := body.Task
	if b.Brief != "do second" || b.Context != "the data lives in a.csv" || b.ItemTitle != "the data item" ||
		len(b.Outputs) != 1 || len(b.Learnings) != 1 || b.Learnings[0] != "the csv has a header" || b.Attempt != 1 {
		t.Fatalf("task block: %+v", b)
	}
	if !body.Item || !hasTag(body.Tags, "atrium:task:t2") || !hasTag(body.Tags, ItemTag) {
		t.Fatalf("not an isolated task launch: %s", got[1])
	}
	for _, never := range []string{"THE BACKLOG BODY", "T1 RECAP"} {
		if strings.Contains(got[1], never) {
			t.Fatalf("the launch carries %q: %s", never, got[1])
		}
	}
}

func TestAHeldTaskNeverStarts(t *testing.T) {
	x := newTaskRig(t)
	x.add(t, "first")
	if moved, _ := x.st.TaskSetHeld("f-x", "t1", true); !moved {
		t.Fatal("hold")
	}
	x.p.taskSchedule(context.Background())
	if got := x.sent(); len(got) != 0 {
		t.Fatalf("a held task was launched: %v", got)
	}
}

func TestTheRoomCapLeavesATaskPendingWithItsReason(t *testing.T) {
	x := newTaskRig(t)
	x.c.capFor = func(string) int { return 0 }
	x.add(t, "first")
	x.p.taskSchedule(context.Background())
	if got := x.sent(); len(got) != 0 {
		t.Fatalf("launched over the cap: %v", got)
	}
	got := x.state(t, "t1")
	if got.State != hubstore.TaskPending || got.Attempt != 0 || !strings.Contains(got.Reason, "launch cap") {
		t.Fatalf("t1: %+v", got)
	}
}

func TestAnUnconfirmedLaunchIsNeverSentAgain(t *testing.T) {
	x := newTaskRig(t)
	x.answer = func(w http.ResponseWriter) bool {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"the answer was lost"}`))
		return true
	}
	x.add(t, "first")
	x.add(t, "second")
	x.p.taskSchedule(context.Background())
	if got := x.state(t, "t1"); got.State != hubstore.TaskUnconfirmed {
		t.Fatalf("t1: %+v", got)
	}
	x.p.taskSchedule(context.Background())
	x.p.taskSchedule(context.Background())
	if got := x.sent(); len(got) != 1 {
		t.Fatalf("launches: %d, want the one", len(got))
	}
}

func TestARoomThatRefusesATaskLaunchFailsTheTaskWithItsSentence(t *testing.T) {
	x := newTaskRig(t)
	x.answer = func(w http.ResponseWriter) bool {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"this room has no place for card homes"}`))
		return true
	}
	x.add(t, "first")
	x.p.taskSchedule(context.Background())
	if got := x.state(t, "t1"); got.State != hubstore.TaskFailed || !strings.Contains(got.Reason, "no place for card homes") {
		t.Fatalf("t1: %+v", got)
	}
}

func (x *taskRig) do(t *testing.T, body map[string]any) (int, string) {
	t.Helper()
	return x.send("POST", "/_hub/items/f-x/tasks", js(body), loopAddr)
}

func TestTheTaskRouteAddsForTheBoardAndRefusesWhatT1DoesNotRun(t *testing.T) {
	x := newItemRig(t)
	x.st.PutItem(hubstore.Item{ID: "it-b", Room: "sg4", Card: "c1", Generation: 1, State: "running", Revision: 1,
		OwnerKind: hubstore.OwnerSession, Owner: "orch@sg4", WorkID: "f-x"})
	tr := &taskRig{itemRig: x}
	for name, c := range map[string]struct {
		body map[string]any
		code int
		says string
	}{
		"a stranger": {map[string]any{"do": "add", "by": "someone@sg4", "title": "a", "brief": "b"}, http.StatusForbidden, ""},
		"code":       {map[string]any{"do": "add", "by": "board", "title": "a", "brief": "b", "kind": "code"}, http.StatusBadRequest, "not built"},
		"after":      {map[string]any{"do": "add", "by": "board", "title": "a", "brief": "b", "after": []string{"t1"}}, http.StatusBadRequest, "not built"},
		"no brief":   {map[string]any{"do": "add", "by": "board", "title": "a"}, http.StatusBadRequest, "brief"},
		"no task":    {map[string]any{"do": "hold", "by": "board"}, http.StatusBadRequest, "needs task"},
	} {
		code, body := tr.do(t, c.body)
		if code != c.code || !strings.Contains(body, c.says) {
			t.Errorf("%s: %d %s", name, code, body)
		}
	}
	if list, _ := x.st.TaskList("f-x"); len(list) != 0 {
		t.Fatalf("a refused add left a task: %+v", list)
	}
	for _, by := range []string{"board", "orch@sg4"} {
		if code, body := tr.do(t, map[string]any{"do": "add", "by": by, "title": "a", "brief": "b"}); code != http.StatusOK {
			t.Fatalf("add by %s: %d %s", by, code, body)
		}
	}
	if code, _ := tr.do(t, map[string]any{"do": "retry", "by": "board", "task": "t1"}); code != http.StatusConflict {
		t.Fatalf("retry of a pending task: %d", code)
	}
	if code, body := tr.do(t, map[string]any{"do": "hold", "by": "board", "task": "t1"}); code != http.StatusOK ||
		str(obj(t, body), "task", "state") != hubstore.TaskHeld {
		t.Fatalf("hold: %d %s", code, body)
	}
	code, body := x.send("GET", "/_hub/items/f-x/tasks", "", loopAddr)
	if code != http.StatusOK || !strings.Contains(body, `"t2"`) || !strings.Contains(body, `"held"`) {
		t.Fatalf("list: %d %s", code, body)
	}
}

func TestARoomReportsATaskFileOnceAndOnlyForATaskOnThatRoom(t *testing.T) {
	x := newTaskRig(t)
	x.add(t, "first")
	x.p.taskSchedule(context.Background())
	file := map[string]any{"event": "file", "room": "sg4", "backlog_id": "f-x", "task_id": "t1", "attempt": 1,
		"card": "c-task", "path": "out.txt", "data": []byte("abc")}
	var names []string
	for i := 0; i < 2; i++ {
		code, body := x.send("POST", "/_hub/items/tasks/sync", js(file), loopAddr)
		if code != http.StatusOK {
			t.Fatalf("file: %d %s", code, body)
		}
		names = append(names, str(obj(t, body), "name"))
	}
	if names[0] != "f-x-t1-out.txt" || names[1] != names[0] {
		t.Fatalf("names: %v", names)
	}
	file["room"] = "sg5"
	if code, _ := x.send("POST", "/_hub/items/tasks/sync", js(file), loopAddr); code != http.StatusForbidden {
		t.Fatalf("a task on sg4 reported from sg5: %d", code)
	}
	file["room"], file["attempt"] = "sg4", 2
	if code, _ := x.send("POST", "/_hub/items/tasks/sync", js(file), loopAddr); code != http.StatusConflict {
		t.Fatalf("a file for an attempt the task is not on: %d", code)
	}
}
