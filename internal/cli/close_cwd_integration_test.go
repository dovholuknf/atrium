//go:build integration

package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCloseCommandTakesACardOrCwdNotBoth(t *testing.T) {
	// no board is reachable here: both refusals must come before any call
	c := newClose()
	c.SetArgs([]string{"card1", "--cwd", "/x", "--yes", "--url", "127.0.0.1:1"})
	if err := c.Execute(); err == nil || !strings.Contains(err.Error(), "not both") {
		t.Fatalf("both were accepted: %v", err)
	}
	c = newClose()
	c.SetArgs([]string{"--url", "127.0.0.1:1"})
	if err := c.Execute(); err == nil || !strings.Contains(err.Error(), "--cwd") {
		t.Fatalf("neither was refused: %v", err)
	}
}

// `atrium close --cwd <path> --yes` runs through the real command and posts to the archive route.
func TestCloseCommandWithCwdPostsTheArchive(t *testing.T) {
	var path string
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.Method + " " + r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"cards":[]}`))
	}))
	defer srv.Close()
	c := newClose()
	c.SetArgs([]string{"--cwd", `C:\wt\a`, "--yes", "--url", srv.URL})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	if path != "POST /v1/cards/archive" || got["cwd"] != `C:\wt\a` {
		t.Fatalf("%s %v", path, got)
	}
}
