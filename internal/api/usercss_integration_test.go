//go:build integration

package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func getUserCSS(s *Server) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	s.userCSS(w, httptest.NewRequest(http.MethodGet, "/user.css", nil))
	return w
}

func TestUserCSSIsServedFromTheStateFolder(t *testing.T) {
	t.Parallel()
	state := t.TempDir()
	s := &Server{StateDir: state}

	w := getUserCSS(s)
	if w.Code != http.StatusOK || w.Body.Len() != 0 {
		t.Fatalf("no user.css answered %d with %q, want an empty 200", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
		t.Errorf("a missing user.css came back as %q", ct)
	}

	if err := os.MkdirAll(filepath.Join(state, "board"), 0o700); err != nil {
		t.Fatal(err)
	}
	css := "body { --uiscale: 1.2; }\n"
	if err := os.WriteFile(filepath.Join(state, "board", "user.css"), []byte(css), 0o600); err != nil {
		t.Fatal(err)
	}
	w = getUserCSS(s)
	if w.Code != http.StatusOK || w.Body.String() != css {
		t.Fatalf("user.css answered %d with %q", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
		t.Errorf("user.css came back as %q", ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("user.css came back with Cache-Control %q, so an edit waits on the browser's cache", cc)
	}
}

func TestUserCSSRefusesALinkOutOfTheStateFolder(t *testing.T) {
	t.Parallel()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "user.css"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, link := range map[string]func(state string) error{
		"the folder": func(state string) error {
			link := filepath.Join(state, "board")
			if err := os.Symlink(outside, link); err == nil || runtime.GOOS != "windows" {
				return err
			}
			return exec.Command("cmd", "/c", "mklink", "/J", link, outside).Run()
		},
		"the file": func(state string) error {
			if err := os.MkdirAll(filepath.Join(state, "board"), 0o700); err != nil {
				return err
			}
			return os.Symlink(filepath.Join(outside, "user.css"), filepath.Join(state, "board", "user.css"))
		},
	} {
		state := t.TempDir()
		if err := link(state); err != nil {
			t.Logf("cannot link out through %s on this machine: %v", name, err)
			continue
		}
		w := getUserCSS(&Server{StateDir: state})
		if w.Code != http.StatusForbidden || strings.Contains(w.Body.String(), "secret") {
			t.Errorf("a link out through %s answered %d with %q", name, w.Code, w.Body.String())
		}
	}
}

func TestSafeModeLeavesUserCSSOut(t *testing.T) {
	t.Parallel()
	h := webHandler("")
	for _, page := range []string{"/", "/m/"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, page, nil))
		if !strings.Contains(w.Body.String(), userCSSLink) {
			t.Errorf("%s does not link user.css", page)
		}
		w = httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, page+"?safe", nil))
		if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "/user.css") || !strings.Contains(w.Body.String(), "/css/tokens.css") {
			t.Errorf("%s?safe answered %d and still links user.css, or lost the board's own styles", page, w.Code)
		}
	}
}
