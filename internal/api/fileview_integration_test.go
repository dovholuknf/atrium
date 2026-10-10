//go:build integration

package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func viewReq(id, p string) *http.Request {
	r := httptest.NewRequest("GET", "/v1/tasks/"+id+"/files/view?path="+url.QueryEscape(p), nil)
	r.SetPathValue("id", id)
	return r
}

// Whatever is in the file, a card's file is shown inline as text or as a raster
// image and never as a document. Every one of these would run script on the
// board's origin if served as itself.
func TestAViewIsNeverADocument(t *testing.T) {
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)

	cases := []struct{ name, body, want string }{
		{"page.html", "<script>alert(1)</script>", "text/plain; charset=utf-8"},
		{"pic.svg", `<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>`, "text/plain; charset=utf-8"},
		{"notes.md", "# hi <script>x</script>", "text/plain; charset=utf-8"},
		{"data.xml", "<a/>", "text/plain; charset=utf-8"},
		{"noext", "plain", "text/plain; charset=utf-8"},
		{"shot.png", "\x89PNG", "image/png"},
		{"SHOT.JPG", "jpg", "image/jpeg"},
		{"anim.gif", "GIF8", "image/gif"},
	}
	for _, c := range cases {
		if err := os.WriteFile(filepath.Join(work, c.name), []byte(c.body), 0o600); err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		s.viewFile(w, viewReq(task.ID, c.name))
		if w.Code != http.StatusOK {
			t.Fatalf("%s answered %d: %s", c.name, w.Code, w.Body.String())
		}
		if got := w.Header().Get("Content-Type"); got != c.want {
			t.Errorf("%s served as %q, want %q", c.name, got, c.want)
		}
		if w.Body.String() != c.body {
			t.Errorf("%s: served %q", c.name, w.Body.String())
		}
		if w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s may be sniffed", c.name)
		}
		if w.Header().Get("Content-Security-Policy") != "sandbox" {
			t.Errorf("%s has no sandbox", c.name)
		}
		if cd := w.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "inline") {
			t.Errorf("%s is not inline: %q", c.name, cd)
		}
	}
}

// A link named for an image that points at a page is served as the page's type,
// which is text, because the extension that counts is the resolved one.
func TestAViewTypesWhatTheLinkResolvesTo(t *testing.T) {
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)
	if err := os.WriteFile(filepath.Join(work, "page.html"), []byte("<script>1</script>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(work, "page.html"), filepath.Join(work, "pic.png")); err != nil {
		t.Skip("no symlinks here")
	}
	w := httptest.NewRecorder()
	s.viewFile(w, viewReq(task.ID, "pic.png"))
	if got := w.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Fatalf("a link to html was served as %q", got)
	}
}

func TestAViewOutsideTheCardIsRefused(t *testing.T) {
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(work, "link.txt")); err != nil {
		t.Log("no symlinks here, skipping that one")
	}

	for _, p := range []string{
		filepath.ToSlash(secret),
		"../../etc/passwd",
		"../" + filepath.Base(outside) + "/secret.txt",
		"link.txt",
	} {
		if p == "link.txt" {
			if _, err := os.Lstat(filepath.Join(work, p)); err != nil {
				continue
			}
		}
		w := httptest.NewRecorder()
		s.viewFile(w, viewReq(task.ID, p))
		if w.Code != http.StatusForbidden {
			t.Errorf("%q answered %d", p, w.Code)
		}
		if strings.Contains(w.Body.String(), "nope") {
			t.Errorf("%q leaked the file", p)
		}
	}
}

func TestAViewOfNothingOrADirectory(t *testing.T) {
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)
	if err := os.Mkdir(filepath.Join(work, "d"), 0o700); err != nil {
		t.Fatal(err)
	}
	for p, want := range map[string]int{"": 400, "d": 400, "missing.md": 404} {
		w := httptest.NewRecorder()
		s.viewFile(w, viewReq(task.ID, p))
		if w.Code != want {
			t.Errorf("%q answered %d, want %d", p, w.Code, want)
		}
	}
}

// The card is the boundary: another card's file is not this card's to read.
func TestAViewCannotReachAnotherCardsFile(t *testing.T) {
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "theirs.md"), []byte("theirs"), 0o600); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.viewFile(w, viewReq(task.ID, filepath.ToSlash(filepath.Join(other, "theirs.md"))))
	if w.Code != http.StatusForbidden {
		t.Fatalf("answered %d", w.Code)
	}
}
