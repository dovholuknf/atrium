//go:build integration

package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// An unmodified disk copy must hash to the same id as the embedded board.
func TestADirectoryCopyOfTheBoardHasTheEmbeddedBuildID(t *testing.T) {
	t.Parallel()
	s := &Server{BoardDir: "web"}
	if got := s.boardID(); got != BuildID {
		t.Errorf("the board on disk hashes to %q and the embedded one to %q. "+
			"the same files have to give the same build id", got, BuildID)
	}
}

func TestTheBuildIDFollowsAFileChangedOnDisk(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	page := filepath.Join(dir, "index.html")
	if err := os.WriteFile(page, []byte("<html>one</html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &Server{BoardDir: dir}
	first := s.boardID()
	if first == "" {
		t.Fatal("a board on disk produced no build id")
	}

	if err := os.WriteFile(page, []byte("<html>two</html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	// No restart, no rebuild, and nothing told the server. Asking again is
	// what `/v1/health` does on a timer, and it has to answer differently.
	if second := s.boardID(); second == first {
		t.Error("the page changed under a running daemon and the build id did not move, " +
			"so nothing open would ever learn it is out of date")
	}
}

// Serving from a directory is serving the files in it, not the embedded copy.
func TestTheHandlerServesTheDirectoryWhenGivenOne(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("from disk"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	webHandler(dir).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "from disk" {
		t.Errorf("a board directory answered %d with %q", rec.Code, rec.Body.String())
	}
}

// A page names its build itself and never revalidates, or a reload is handed the cached page's header and the old
// build, and reloads again.
func TestAPageLoadNamesItsBuildAndIsNeverA304(t *testing.T) {
	t.Parallel()
	h := stampBuild(func() string { return "b2" }, webHandler(""))
	first := httptest.NewRecorder()
	h.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/index.js", nil))
	if got := first.Header().Get("Server-Timing"); got != "" {
		t.Errorf("a script was stamped %q, only a page is", got)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	page := httptest.NewRecorder()
	h.ServeHTTP(page, req)
	req.Header.Set("If-None-Match", page.Header().Get("ETag"))
	again := httptest.NewRecorder()
	h.ServeHTTP(again, req)
	if again.Code != http.StatusOK || again.Header().Get("Server-Timing") != `build;desc="b2"` {
		t.Errorf("a page revalidated answered %d with Server-Timing %q", again.Code, again.Header().Get("Server-Timing"))
	}
}
