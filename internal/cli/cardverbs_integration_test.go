//go:build integration

package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// The card verbs, against a board that answers the way the hub does: the path
// it was asked, and the two headers naming what it reached.
func cardBoard(t *testing.T) (*httptest.Server, func() []string, func() map[string]any) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	var last map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.EscapedPath())
		raw, _ := io.ReadAll(r.Body)
		last = nil
		_ = json.Unmarshal(raw, &last)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "nobody") {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"no card called \"nobody\"","would_work":["rnd-director@claude-sg4 (@rnd)"]}`))
			return
		}
		if strings.Contains(r.URL.Path, "twice") {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":"\"twice\" is on more than one room","candidates":["twice@a (a~1)","twice@b (b~2)"]}`))
			return
		}
		w.Header().Set("X-Atrium-Card", "claude-sg4~01a0")
		w.Header().Set("X-Atrium-Handle", "rnd-director@claude-sg4")
		_, _ = w.Write([]byte(`{"id":"01a0","display_title":"rnd director","wire_name":"rnd-director","alias":"rnd","status":"needs-input","wait_seconds":90}`))
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), seen...) },
		func() map[string]any { mu.Lock(); defer mu.Unlock(); return last }
}

func runCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := newRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func TestTheCardVerbs(t *testing.T) {
	srv, seen, _ := cardBoard(t)
	out, err := runCmd(t, "task", "rnd@claude-sg4", "--url", srv.URL)
	if err != nil || !strings.Contains(out, "handle  rnd-director@claude-sg4") || !strings.Contains(out, "status  needs-input") {
		t.Fatalf("task printed %q (%v)", out, err)
	}
	if out, err = runCmd(t, "exit", "@rnd", "--url", srv.URL); err != nil ||
		!strings.Contains(out, "asked rnd-director@claude-sg4 (claude-sg4~01a0) to exit") {
		t.Fatalf("exit printed %q (%v)", out, err)
	}
	if out, err = runCmd(t, "new-context", "sparta/rnd", "--url", srv.URL); err != nil ||
		!strings.Contains(out, "for a new context") {
		t.Fatalf("new-context printed %q (%v)", out, err)
	}
	want := []string{"GET /v1/tasks/rnd@claude-sg4", "POST /v1/tasks/@rnd/exit", "POST /v1/tasks/sparta%2Frnd/new-context"}
	if got := seen(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("asked %v, want %v", got, want)
	}
	// A miss says what would have worked, and two holders are both named.
	if _, err := runCmd(t, "task", "nobody", "--url", srv.URL); err == nil ||
		!strings.Contains(err.Error(), "no card called") {
		t.Fatalf("a miss answered %v", err)
	}
	if _, err := runCmd(t, "exit", "twice", "--url", srv.URL); err == nil ||
		!strings.Contains(err.Error(), "twice@b (b~2)") {
		t.Fatalf("an ambiguous name answered %v", err)
	}
}

// launch --onto names the card in the body and leaves the directory to it.
func TestLaunchOntoACardByName(t *testing.T) {
	srv, seen, last := cardBoard(t)
	if err := launchAgent(launchOpts{boardURL: srv.URL, harness: "claude", onto: "rnd", quiet: true}); err != nil {
		t.Fatal(err)
	}
	if got := seen(); len(got) != 1 || got[0] != "POST /v1/launch" {
		t.Fatalf("asked %v", got)
	}
	body := last()
	if body["task_id"] != "rnd" || body["cwd"] != "" {
		t.Fatalf("the launch body was %v, want task_id rnd and no cwd", body)
	}
}
