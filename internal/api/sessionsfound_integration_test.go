//go:build integration

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTranscript(t *testing.T, home, folder, id, body string, at time.Time) {
	t.Helper()
	dir := filepath.Join(home, ".claude", "projects", encodeProjectDir(folder))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, at, at); err != nil {
		t.Fatal(err)
	}
}

func foundOf(t *testing.T, q string) (int, []foundSession, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	(&Server{}).sessionsFound(rec, httptest.NewRequest(http.MethodGet, "/v1/sessions/found"+q, nil))
	var got struct {
		Sessions []foundSession `json:"sessions"`
		Error    string         `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	return rec.Code, got.Sessions, got.Error
}

func TestSessionsFoundListsTheNewestPerFolderAndSkipsFoldersThatAreGone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	a, b, gone := filepath.Join(home, "src", "a"), filepath.Join(home, "src", "b"), filepath.Join(home, "src", "gone")
	for _, d := range []string{a, b} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	line := func(cwd, title string) string {
		c, _ := json.Marshal(cwd)
		return `{"type":"ai-title","aiTitle":"` + title + `","cwd":` + string(c) + "}\n"
	}
	now := time.Now()
	writeTranscript(t, home, a, "old", line(a, "older work"), now.Add(-48*time.Hour))
	writeTranscript(t, home, a, "new", line(a, "newer work"), now.Add(-2*time.Hour))
	writeTranscript(t, home, b, "only", line(b, "other repo"), now.Add(-24*time.Hour))
	writeTranscript(t, home, gone, "ghost", line(gone, "folder deleted"), now)

	code, got, _ := foundOf(t, "")
	if code != http.StatusOK || len(got) != 2 {
		t.Fatalf("got %d %+v, want 2 sessions", code, got)
	}
	if got[0].ID != "new" || got[0].Title != "newer work" || got[0].Cwd != filepath.ToSlash(a) {
		t.Errorf("first = %+v, want the newest session of a", got[0])
	}
	if got[1].ID != "only" {
		t.Errorf("second = %+v, want b's only session", got[1])
	}
}

func TestSessionsFoundDirLooksInOneFolderAndRefusesAMissingOne(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	a := filepath.Join(home, "src", "a")
	if err := os.MkdirAll(a, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTranscript(t, home, a, "s1", `{"type":"ai-title","aiTitle":"one"}`+"\n", time.Now().Add(-time.Hour))
	writeTranscript(t, home, a, "s2", `{"type":"ai-title","aiTitle":"two"}`+"\n", time.Now())

	code, got, _ := foundOf(t, "?dir="+url.QueryEscape(a))
	if code != http.StatusOK || len(got) != 2 || got[0].ID != "s2" || got[0].Cwd != filepath.ToSlash(a) {
		t.Fatalf("got %d %+v, want both sessions of the folder, newest first", code, got)
	}
	code, _, msg := foundOf(t, "?dir="+url.QueryEscape(filepath.Join(home, "nope")))
	if code != http.StatusBadRequest || msg == "" {
		t.Fatalf("got %d %q, want 400 saying the folder is not there", code, msg)
	}
}

func TestSessionsFoundIsEmptyWhenClaudeHasNeverRun(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	code, got, _ := foundOf(t, "")
	if code != http.StatusOK || len(got) != 0 {
		t.Fatalf("got %d %+v, want an empty list", code, got)
	}
}
