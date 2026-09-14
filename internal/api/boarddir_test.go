package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// The build id must reflect changes to board files served from disk.

// An unmodified disk copy must hash to the same id as the embedded board.
func TestADirectoryCopyOfTheBoardHasTheEmbeddedBuildID(t *testing.T) {
	s := &Server{BoardDir: "web"}
	if got := s.boardID(); got != BuildID {
		t.Errorf("the board on disk hashes to %q and the embedded one to %q. "+
			"the same files have to give the same build id", got, BuildID)
	}
}

func TestTheBuildIDFollowsAFileChangedOnDisk(t *testing.T) {
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
