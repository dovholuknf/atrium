package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

type changesFix struct {
	t    *testing.T
	d    *Daemon
	ts   *httptest.Server
	repo string
	task *store.Task
	tr   string // the transcript
}

func (f *changesFix) get(query string) (int, map[string]any) {
	f.t.Helper()
	res, err := http.Get(f.ts.URL + "/v1/tasks/" + f.task.ID + "/changes" + query)
	if err != nil {
		f.t.Fatal(err)
	}
	defer res.Body.Close()
	var m map[string]any
	_ = json.NewDecoder(res.Body).Decode(&m)
	return res.StatusCode, m
}

func (f *changesFix) ok(query string) *ChangesView {
	f.t.Helper()
	res, err := http.Get(f.ts.URL + "/v1/tasks/" + f.task.ID + "/changes" + query)
	if err != nil {
		f.t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		f.t.Fatalf("changes%s answered %d", query, res.StatusCode)
	}
	var v ChangesView
	if err := json.NewDecoder(res.Body).Decode(&v); err != nil {
		f.t.Fatal(err)
	}
	return &v
}

func (f *changesFix) prompt(at time.Time, text string) {
	f.t.Helper()
	b, _ := json.Marshal(map[string]any{"type": "user", "timestamp": at.Format(time.RFC3339Nano),
		"message": map[string]any{"content": text}})
	fh, err := os.OpenFile(f.tr, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		f.t.Fatal(err)
	}
	defer fh.Close()
	fmt.Fprintln(fh, string(b))
}

// turn writes one turn: a prompt, an assistant message calling the given edits, and a text reply.
// It returns the reply's time.
func (f *changesFix) turn(start time.Time, id string, edits ...map[string]any) time.Time {
	f.prompt(start, "do "+id)
	if len(edits) > 0 {
		transcriptReply(f.t, f.tr, id+"-tools", start.Add(time.Second), false, edits...)
	}
	at := start.Add(2 * time.Second)
	transcriptReply(f.t, f.tr, id+"-text", at, false, textBlock("done "+id))
	return at
}
