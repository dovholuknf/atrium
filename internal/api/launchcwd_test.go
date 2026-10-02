package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func cwdAnswer(t *testing.T, srv *Server, path string) (exists, dir bool) {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/launch/cwd?path="+url.QueryEscape(path), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("the directory check answered %d: %s", rec.Code, rec.Body)
	}
	var body struct {
		Exists bool `json:"exists"`
		Dir    bool `json:"dir"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Exists, body.Dir
}

func TestLaunchCwdSaysAnExistingDirectoryIsOne(t *testing.T) {
	srv, _, work := fileServer(t)
	if exists, dir := cwdAnswer(t, srv, work); !exists || !dir {
		t.Fatalf("a directory that is there answered exists=%v dir=%v", exists, dir)
	}
}

func TestLaunchCwdSaysAMissingDirectoryIsNot(t *testing.T) {
	srv, _, work := fileServer(t)
	if exists, dir := cwdAnswer(t, srv, filepath.Join(work, "nowhere")); exists || dir {
		t.Fatalf("a directory that is not there answered exists=%v dir=%v", exists, dir)
	}
}

// A file is there and is not a place to run.
func TestLaunchCwdSaysAFileIsNotADirectory(t *testing.T) {
	srv, _, work := fileServer(t)
	file := filepath.Join(work, "a-file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if exists, dir := cwdAnswer(t, srv, file); !exists || dir {
		t.Fatalf("a file answered exists=%v dir=%v, want it there and not a directory", exists, dir)
	}
}

// Relative to the daemon's own directory means nothing to the caller.
func TestLaunchCwdIgnoresARelativePath(t *testing.T) {
	srv, _, _ := fileServer(t)
	for _, p := range []string{"", ".", "..", "internal"} {
		if exists, dir := cwdAnswer(t, srv, p); exists || dir {
			t.Fatalf("%q answered exists=%v dir=%v, want neither", p, exists, dir)
		}
	}
}
