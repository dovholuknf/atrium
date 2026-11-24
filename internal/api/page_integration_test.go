//go:build integration

package api

import (
	"github.com/dovholuknf/atrium/internal/base/seenhost"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pageGet(s *Server, id, path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "/v1/tasks/"+id+"/page/"+path, nil)
	r.SetPathValue("id", id)
	r.SetPathValue("path", path)
	w := httptest.NewRecorder()
	s.servePage(w, r)
	return w
}

func TestAPageIsServedInlineInASandboxWithItsRelativeAssets(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)
	if err := os.MkdirAll(filepath.Join(work, "mock"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(work, "mock", "a.html"), []byte("<script src=b.js></script>"), 0o644)
	os.WriteFile(filepath.Join(work, "mock", "b.js"), []byte("1"), 0o644)

	w := pageGet(s, task.ID, "mock/a.html")
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") ||
		strings.Contains(w.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("page = %d %v", w.Code, w.Header())
	}
	if csp := w.Header().Get("Content-Security-Policy"); !strings.HasPrefix(csp, "sandbox ") || strings.Contains(csp, "allow-same-origin") {
		t.Fatalf("csp = %q", csp)
	}
	if w := pageGet(s, task.ID, "mock/b.js"); w.Code != http.StatusOK {
		t.Fatalf("asset = %d", w.Code)
	}
}

func TestAPageCannotLeaveTheCard(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret.html"), []byte("x"), 0o644)
	if err := os.Symlink(outside, filepath.Join(work, "link")); err != nil {
		t.Skip("no symlinks")
	}
	for _, p := range []string{"../" + filepath.Base(outside) + "/secret.html", "link/secret.html", "/etc/hosts", outside + "/secret.html"} {
		if w := pageGet(s, task.ID, p); w.Code != http.StatusForbidden {
			t.Fatalf("%q answered %d", p, w.Code)
		}
	}
	if w := pageGet(s, task.ID, "nope.html"); w.Code != http.StatusNotFound {
		t.Fatalf("missing answered %d", w.Code)
	}
}

func TestAFolderOpensItsLoneHTMLWithRootLinksPutBackUnderThePrefixAndTheBoardCSS(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)
	dir := filepath.Join(work, "mock")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "mock.html"),
		[]byte(`<link rel="stylesheet" href="/css/tokens.css"><link rel="stylesheet" href="/mock.css"><script src="mock.js"></script>`), 0o644)
	os.WriteFile(filepath.Join(dir, "mock.css"), []byte("a{}"), 0o644)
	get := func(p, prefix string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/v1/tasks/"+task.ID+"/page/"+p+"?v=1", nil)
		r.SetPathValue("id", task.ID)
		r.SetPathValue("path", p)
		r.Header.Set(PagePrefixHeader, prefix)
		w := httptest.NewRecorder()
		s.servePage(w, r)
		return w
	}
	pre := "/room/r/" + task.ID + "/files/"
	if w := get("mock", pre); w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != pre+"mock/?v=1" {
		t.Fatalf("folder without slash = %d %q", w.Code, w.Header().Get("Location"))
	}
	w := get("mock/", pre)
	body := w.Body.String()
	if w.Code != 200 {
		t.Fatalf("folder = %d %q", w.Code, body)
	}
	if !strings.Contains(body, `href="`+pre+`_board/css/tokens.css"`) || !strings.Contains(body, `href="`+pre+`mock/mock.css"`) {
		t.Fatalf("links not put under the prefix: %q", body)
	}
	if w := get("_board/css/tokens.css", pre); w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Security-Policy"), "sandbox ") {
		t.Fatalf("board css = %d", w.Code)
	}
	if w := get("_board/../../etc/hosts", pre); w.Code != http.StatusNotFound {
		t.Fatalf("board escape = %d", w.Code)
	}
}

func TestAMarkdownFileIsDrawnAsAPageAndRawIsKept(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)
	os.WriteFile(filepath.Join(work, "n.md"), []byte("# Hi</script>"), 0o644)
	get := func(q string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/v1/tasks/"+task.ID+"/page/n.md"+q, nil)
		r.SetPathValue("id", task.ID)
		r.SetPathValue("path", "n.md")
		r.Header.Set(PagePrefixHeader, "/room/r/"+task.ID+"/files/")
		w := httptest.NewRecorder()
		s.servePage(w, r)
		return w
	}
	w := get("")
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") || !strings.Contains(w.Body.String(), "vendor/marked.js") ||
		strings.Contains(w.Body.String(), "# Hi</script>") {
		t.Fatalf("not drawn: %q", w.Body.String())
	}
	if w := get("?raw=1"); w.Body.String() != "# Hi</script>" {
		t.Fatalf("raw = %q", w.Body.String())
	}
}

func TestHumanBaseAnswersTheSeenHostAndNothingBeforeOne(t *testing.T) {
	s, _, _ := fileServer(t)
	seenhost.Reset()
	t.Cleanup(seenhost.Reset)
	ask := func() string {
		w := httptest.NewRecorder()
		s.humanBase(w, httptest.NewRequest("GET", "/v1/human-base", nil))
		return strings.TrimSpace(w.Body.String())
	}
	if got := ask(); got != `{"base":""}` {
		t.Fatalf("before a host was seen: %s", got)
	}
	r := httptest.NewRequest("POST", "http://atrium.ziti/v1/x", nil)
	r.Header.Set("Sec-Fetch-Mode", "cors")
	seenhost.Note(r)
	if got := ask(); got != `{"base":"http://atrium.ziti"}` {
		t.Fatalf("after a host was seen: %s", got)
	}
}

func TestTheRoomServesACardLinkOnItsOwnPortAndAcceptsAWorkPrefix(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)
	if err := os.MkdirAll(filepath.Join(work, "mock"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(work, "mock", "a.html"), []byte("<p>hi</p>"), 0o644)
	for _, p := range []string{"mock/a.html", filepath.Base(work) + "/mock/a.html"} {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/room/r1/"+task.ID+"/files/"+p, nil))
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "<p>hi</p>") {
			t.Errorf("%s answered %d %q", p, w.Code, w.Body.String())
		}
	}
}
