package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/gitsync"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// a checkout and one linked worktree of it, made by hand: no git is run.
func fakeRepo(t *testing.T) (repo, wt string) {
	t.Helper()
	base := t.TempDir()
	repo = filepath.Join(base, "ziti")
	write(t, filepath.Join(repo, ".git", "HEAD"), "ref: refs/heads/main\n")
	write(t, filepath.Join(repo, ".git", "config"),
		"[core]\n\tbare = false\n[remote \"origin\"]\n\turl = git@github.com:openziti/ziti.git\n")
	wt = filepath.Join(base, "wt", "fix-it")
	gitdir := filepath.Join(repo, ".git", "worktrees", "fix-it")
	write(t, filepath.Join(gitdir, "HEAD"), "ref: refs/heads/fix/it\n")
	write(t, filepath.Join(gitdir, "commondir"), "../..\n")
	write(t, filepath.Join(wt, ".git"), "gitdir: "+filepath.ToSlash(gitdir)+"\n")
	return repo, wt
}

func TestOriginSlugRefusesWhatIsNotAForge(t *testing.T) {
	for body, want := range map[string]string{
		"[remote \"origin\"]\n\turl = https://github.com/o/r.git\n":     "github/o/r",
		"[remote \"origin\"]\n\turl = https://tok@github.com/o/r.git\n": "",
		"[remote \"upstream\"]\n\turl = https://github.com/o/r.git\n":   "",
		"[remote \"origin\"]\n\turl = https://git.example.com/o/r\n":    "git.example.com/o/r",
		"[remote \"origin\"]\n\turl = /some/local/path\n":               "",
	} {
		readFile := func(path string) ([]byte, error) {
			if path != "config" {
				t.Fatalf("read path %q, want config", path)
			}
			return []byte(body), nil
		}
		if got := originSlugWithReadFile("config", readFile); got != want {
			t.Errorf("%q: got %q want %q", strings.TrimSpace(body), got, want)
		}
	}
}

func TestCloneCheckoutGoesThroughTheHubClone(t *testing.T) {
	var asked string
	s := &Server{CloneFromHub: func(_ context.Context, url string) (gitsync.SCMResult, error) {
		asked = url
		return gitsync.SCMResult{Path: "/home/me/git/github/o/r", State: "cloned"}, nil
	}}
	s.checkouts.have, s.checkouts.at = true, time.Now()
	rec := httptest.NewRecorder()
	s.cloneCheckout(rec, httptest.NewRequest("POST", "/v1/checkouts/clone", strings.NewReader(`{"repo":"github/o/r"}`)))
	if rec.Code != http.StatusOK || asked != "https://github.com/o/r" {
		t.Fatalf("code %d asked %q body %s", rec.Code, asked, rec.Body)
	}
	var out gitsync.SCMResult
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Path != "/home/me/git/github/o/r" {
		t.Errorf("the path comes back for the dialog to fill: %+v", out)
	}
}

func TestCloneCheckoutRefusesBadNamesAndRoomsWithNoHub(t *testing.T) {
	post := func(s *Server, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.cloneCheckout(rec, httptest.NewRequest("POST", "/v1/checkouts/clone", strings.NewReader(body)))
		return rec
	}
	s := &Server{CloneFromHub: func(context.Context, string) (gitsync.SCMResult, error) {
		return gitsync.SCMResult{}, errors.New("this room has no hub to clone from")
	}}
	s.checkouts.have, s.checkouts.at = true, time.Now()
	if rec := post(s, `{"repo":"../../etc"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("a traversal is a 400: %d", rec.Code)
	}
	if rec := post(s, `{"repo":"github/o/r"}`); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "no hub") {
		t.Errorf("no hub says so: %d %s", rec.Code, rec.Body)
	}
	if rec := post(&Server{}, `{"repo":"github/o/r"}`); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("a room that cannot clone: %d", rec.Code)
	}
}
