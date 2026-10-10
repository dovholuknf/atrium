//go:build integration

package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// `atrium merged` runs inside a git hook: it exits 0 whatever happens.
func TestMergedExitsZeroWhenNothingIsListening(t *testing.T) {
	c := newMerged()
	var errb bytes.Buffer
	c.SetErr(&errb)
	c.SetArgs([]string{"--into", "claude/main", "--url", "http://127.0.0.1:1"})
	if err := c.Execute(); err != nil {
		t.Fatalf("merged failed a merge: %v", err)
	}
	if !strings.Contains(errb.String(), "atrium merged:") {
		t.Errorf("stderr = %q, want one line saying what went wrong", errb.String())
	}
}

func TestMergedExitsZeroOnAnErrorAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := newMerged()
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"--into", "claude/main", "--url", srv.URL})
	if err := c.Execute(); err != nil {
		t.Fatalf("merged failed a merge: %v", err)
	}
}

func TestMergedPostsTheBranchAndSaysWhatWasMarked(t *testing.T) {
	var path, body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		var b bytes.Buffer
		_, _ = b.ReadFrom(r.Body)
		body = b.String()
		_, _ = w.Write([]byte(`{"into":"claude/main","marked":[{"card":"c1"}]}`))
	}))
	defer srv.Close()
	c := newMerged()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetArgs([]string{"--into", "claude/main", "--url", srv.URL})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	if path != "/v1/merged" || !strings.Contains(body, `"claude/main"`) {
		t.Errorf("posted %s %s", path, body)
	}
	if !strings.Contains(out.String(), "c1") {
		t.Errorf("stdout = %q, want the marked card", out.String())
	}
}
