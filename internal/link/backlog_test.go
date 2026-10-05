package link

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

// The hub's backlog and director reports. See backlog.go.

type backlogRig struct {
	p    *Proxy
	feed *sub
}

func newBacklogRig(t *testing.T) *backlogRig {
	t.Helper()
	p := NewProxy(NewHub(Timings{Beat: 200 * time.Millisecond, Silence: time.Second, Warm: 1}), nil, "", nil)
	st, err := hubstore.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	p.SetBacklog(st)
	x := &backlogRig{p: p, feed: &sub{ch: make(chan Event, 16)}}
	p.feeds.mu.Lock()
	p.feeds.subs[x.feed] = struct{}{}
	p.feeds.mu.Unlock()
	return x
}

// sawEvent waits for an event of a kind, past the audit lines that come with a write.
func (x *backlogRig) sawEvent(t *testing.T, kind string) {
	t.Helper()
	for {
		select {
		case ev := <-x.feed.ch:
			if ev.Kind == kind {
				return
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("no %s event", kind)
		}
	}
}

func (x *backlogRig) send(method, target, body, remote string) (int, string) {
	req := httptest.NewRequest(method, "http://127.0.0.1:7778"+target, strings.NewReader(body))
	req.RemoteAddr = remote
	rec := httptest.NewRecorder()
	x.p.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestBacklogFiledOnOneRoomIsListedAndReadFromAnother(t *testing.T) {
	x := newBacklogRig(t)
	code, body := x.send("POST", "/_hub/backlog", js(map[string]any{"id": "f-new-x", "dept": "fabric",
		"title": "do x", "body": "the whole item", "by": "orch@sg4", "room": "sg4"}), loopAddr)
	if code != http.StatusCreated || str(obj(t, body), "status") != "open" {
		t.Fatalf("file: %d %s", code, body)
	}
	x.sawEvent(t, "backlog")
	// A read needs no machine: the board and the m1mini director's room both read.
	code, body = x.send("GET", "/_hub/backlog?dept=fabric&open=1", "", "203.0.113.9:4000")
	if code != 200 || !strings.Contains(body, `"f-new-x"`) || strings.Contains(body, "the whole item") {
		t.Fatalf("list: %d %s", code, body)
	}
	code, body = x.send("GET", "/_hub/backlog/f-new-x", "", loopAddr)
	if code != 200 || str(obj(t, body), "body") != "the whole item" || str(obj(t, body), "filed_room") != "sg4" {
		t.Fatalf("get: %d %s", code, body)
	}
}

func TestBacklogRefusals(t *testing.T) {
	x := newBacklogRig(t)
	file := js(map[string]any{"id": "a", "dept": "fabric", "title": "t"})
	if code, _ := x.send("POST", "/_hub/backlog", file, "203.0.113.9:4000"); code != http.StatusForbidden {
		t.Fatalf("a write from another machine: %d", code)
	}
	if code, _ := x.send("POST", "/_hub/backlog", file, loopAddr); code != http.StatusCreated {
		t.Fatalf("file: %d", code)
	}
	if code, body := x.send("POST", "/_hub/backlog", file, loopAddr); code != http.StatusConflict ||
		str(obj(t, body), "id") != "a" {
		t.Fatalf("a taken id: %d %s", code, body)
	}
	for _, bad := range []string{
		`{"id":"A","dept":"fabric","title":"t"}`,
		`{"id":"b","dept":"fabric","title":""}`,
		`{"id":"b","dept":"fabric","title":"t","extra":1}`,
	} {
		if code, _ := x.send("POST", "/_hub/backlog", bad, loopAddr); code != http.StatusBadRequest {
			t.Errorf("%s: %d", bad, code)
		}
	}
	if code, _ := x.send("GET", "/_hub/backlog/nope", "", loopAddr); code != http.StatusNotFound {
		t.Fatalf("missing: %d", code)
	}
	if code, _ := x.send("POST", "/_hub/backlog/a", `{"status":"bogus"}`, loopAddr); code != http.StatusBadRequest {
		t.Fatalf("bogus status: %d", code)
	}
	if code, body := x.send("POST", "/_hub/backlog/a", `{"status":"done","by":"fabric@m1mini"}`, loopAddr); code != 200 ||
		str(obj(t, body), "status") != "done" {
		t.Fatalf("status: %d %s", code, body)
	}
	if code, _ := x.send("PUT", "/_hub/backlog", file, loopAddr); code != http.StatusMethodNotAllowed {
		t.Fatalf("put: %d", code)
	}
}

func TestBacklogIsAbsentUntilWired(t *testing.T) {
	p := NewProxy(NewHub(Timings{Beat: 200 * time.Millisecond, Silence: time.Second, Warm: 1}), nil, "", nil)
	for _, path := range []string{"/_hub/backlog", "/_hub/reports"} {
		req := httptest.NewRequest("GET", "http://127.0.0.1:7778"+path, nil)
		req.RemoteAddr = loopAddr
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: %d", path, rec.Code)
		}
	}
}

func TestReportsFlowFromAnotherRoomToTheOrchestrator(t *testing.T) {
	x := newBacklogRig(t)
	code, body := x.send("POST", "/_hub/reports", js(map[string]any{"subject": "landed", "body": "sha abc",
		"by": "fabric@m1mini", "room": "m1mini"}), loopAddr)
	if code != http.StatusCreated || str(obj(t, body), "id") != "rp_1" {
		t.Fatalf("add: %d %s", code, body)
	}
	x.sawEvent(t, "report")
	code, body = x.send("GET", "/_hub/reports?unread=1", "", loopAddr)
	if code != 200 || !strings.Contains(body, "sha abc") {
		t.Fatalf("list: %d %s", code, body)
	}
	if code, body = x.send("POST", "/_hub/reports/rp_1", `{"do":"read","by":"orch@sg4"}`, loopAddr); code != 200 ||
		str(obj(t, body), "read_by") != "orch@sg4" {
		t.Fatalf("read: %d %s", code, body)
	}
	if _, body = x.send("GET", "/_hub/reports?unread=1", "", loopAddr); strings.Contains(body, "rp_1") {
		t.Fatalf("still unread: %s", body)
	}
	if code, _ := x.send("POST", "/_hub/reports", `{"subject":""}`, loopAddr); code != http.StatusBadRequest {
		t.Fatalf("empty subject: %d", code)
	}
	if code, _ := x.send("POST", "/_hub/reports/rp_9", `{"do":"read"}`, loopAddr); code != http.StatusNotFound {
		t.Fatalf("missing: %d", code)
	}
}

// The tools go through the hub's board, so a director's call on one room lands in rows another room reads.
func TestBacklogAndReportsToolsReachTheHub(t *testing.T) {
	x := newBacklogRig(t)
	srv := httptest.NewServer(x.p)
	t.Cleanup(srv.Close)
	c := &controlMCP{board: srv.URL, client: srv.Client()}
	ctx := context.Background()

	_, out, err := c.backlogHandler(ctx, nil, backlogInput{Action: "file", ID: "m-1", Dept: "fabric", Title: "from m1mini"})
	if err != nil || out.Item == nil || out.Item.Status != "open" {
		t.Fatalf("file: %+v %v", out, err)
	}
	_, out, err = c.backlogHandler(ctx, nil, backlogInput{Action: "file", ID: "m-1", Dept: "fabric", Title: "again"})
	if err == nil || !strings.Contains(err.Error(), "already exists") && !strings.Contains(err.Error(), "409") {
		t.Fatalf("a taken id was filed: %+v %v", out, err)
	}
	_, out, err = c.backlogHandler(ctx, nil, backlogInput{Action: "list", Open: true})
	if err != nil || len(out.Items) != 1 || out.Items[0].ID != "m-1" {
		t.Fatalf("list: %+v %v", out, err)
	}
	if _, _, err := c.backlogHandler(ctx, nil, backlogInput{Action: "get"}); err == nil {
		t.Fatal("get without an id")
	}
	_, rout, err := c.reportsHandler(ctx, nil, reportsInput{Action: "add", Subject: "hi", Body: "there"})
	if err != nil || rout.Report == nil || rout.Report.ID != "rp_1" {
		t.Fatalf("add: %+v %v", rout, err)
	}
	_, rout, err = c.reportsHandler(ctx, nil, reportsInput{Action: "list", Unread: true})
	if err != nil || len(rout.Reports) != 1 {
		t.Fatalf("list: %+v %v", rout, err)
	}
	_, rout, err = c.reportsHandler(ctx, nil, reportsInput{Action: "read", ID: "rp_1"})
	if err != nil || rout.Report == nil || rout.Report.ReadAt == nil {
		t.Fatalf("read: %+v %v", rout, err)
	}
}

func TestBacklogToolsAreForTheFullClassOnly(t *testing.T) {
	for _, name := range []string{"atrium_backlog", "atrium_reports"} {
		if inClass(name, classWorker) || !inClass(name, classFull) {
			t.Errorf("%s is in the wrong classes", name)
		}
	}
}
