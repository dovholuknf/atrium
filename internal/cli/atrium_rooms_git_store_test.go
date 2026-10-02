package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func runHub(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return runRooms(t, args...)
}

func TestHubGitInitAsksTheHubAndPrintsTheAnswer(t *testing.T) {
	var got map[string]any
	var path, method string
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, method = r.URL.Path, r.Method
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(map[string]any{"repo": "github/o/r", "created": true, "seeded": true,
			"main": "0123456789abcdef0123456789abcdef01234567", "note": "created, and main seeded"})
	}))
	defer hub.Close()
	out, err := runHub(t, "git", "init", "https://github.com/o/r", "--atrium-board-addr", strings.TrimPrefix(hub.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	if method != "POST" || path != "/_hub/git/init" || got["url"] != "https://github.com/o/r" {
		t.Fatalf("hub was sent %s %s %v", method, path, got)
	}
	if !strings.Contains(out, "github/o/r") || !strings.Contains(out, "created, seeded") ||
		!strings.Contains(out, "0123456789ab") || !strings.Contains(out, "main seeded") {
		t.Fatalf("out = %s", out)
	}
}

func TestHubGitInitSaysWhatTheHubRefused(t *testing.T) {
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "that URL has a username or a token in it"})
	}))
	defer hub.Close()
	_, err := runHub(t, "git", "init", "https://tok@github.com/o/r", "--atrium-board-addr", strings.TrimPrefix(hub.URL, "http://"))
	if err == nil || !strings.Contains(err.Error(), "token") {
		t.Fatalf("err = %v", err)
	}
	if _, err := runHub(t, "git", "init"); err == nil {
		t.Fatal("init without a url worked")
	}
}

func TestHubGitLsPrintsTheStore(t *testing.T) {
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_hub/git/repos" || r.Method != "GET" {
			t.Errorf("asked %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"repos":[{"host":"github","owner":"o","repo":"r","url":"git@hub.atrium:o/r.git",` +
			`"path":"/git/hub/github/o/r.git","main":{"sha":"","at":null},"branches":[]}]}`))
	}))
	defer hub.Close()
	out, err := runHub(t, "git", "store", "--atrium-board-addr", strings.TrimPrefix(hub.URL, "http://"))
	if err != nil || !strings.Contains(out, "github/o/r") || !strings.Contains(out, "(empty)") ||
		!strings.Contains(out, "git@hub.atrium:o/r.git") {
		t.Fatalf("out = %q err = %v", out, err)
	}
}

func TestHubGitSettingsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "hub.db")
	out, err := runHub(t, "git", "settings", "--atrium-db", db, "--atrium-dir", dir)
	if err != nil || !strings.Contains(out, filepath.Join(dir, "git")) || !strings.Contains(out, "git.create_on_push  off") {
		t.Fatalf("defaults: %v %s", err, out)
	}
	alt := filepath.ToSlash(filepath.Join(t.TempDir(), "store"))
	out, err = runHub(t, "git", "settings", "--store", alt, "--create-on-push", "on", "--atrium-db", db, "--atrium-dir", dir)
	if err != nil || !strings.Contains(out, alt) || !strings.Contains(out, "git.create_on_push  on") {
		t.Fatalf("set: %v %s", err, out)
	}
	// It stuck: a plain show reads it back.
	out, _ = runHub(t, "git", "settings", "--atrium-db", db, "--atrium-dir", dir)
	if !strings.Contains(out, alt) || !strings.Contains(out, "create_on_push  on") {
		t.Fatalf("reread: %s", out)
	}
	if _, err := runHub(t, "git", "settings", "--store", "relative", "--atrium-db", db, "--atrium-dir", dir); err == nil {
		t.Fatal("a relative store was taken")
	}
	if _, err := runHub(t, "git", "settings", "--create-on-push", "maybe", "--atrium-db", db, "--atrium-dir", dir); err == nil {
		t.Fatal("maybe was taken")
	}
	out, err = runHub(t, "git", "settings", "--store", "", "--create-on-push", "off", "--atrium-db", db, "--atrium-dir", dir)
	if err != nil || !strings.Contains(out, filepath.Join(dir, "git")) || !strings.Contains(out, "git.create_on_push  off") {
		t.Fatalf("reset: %v %s", err, out)
	}
}

// The verb takes a URL as its one argument, and has no flag for a path, a branch or a refspec.
func TestHubGitVerbsHaveNoPathBranchOrRefspecFlag(t *testing.T) {
	for _, sub := range []*cobra.Command{gitInitCmd("atrium-"), gitStoreCmd("atrium-"), gitSettingsCmd("atrium-")} {
		for _, banned := range []string{"branch", "url", "ref", "path", "remote", "checkout"} {
			if f := sub.Flags().Lookup(banned); f != nil {
				t.Errorf("rooms git %s has --%s", sub.Name(), banned)
			}
		}
	}
}
