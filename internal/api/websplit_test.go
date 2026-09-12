package api

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// shortSum is what `buildID` used to do to one file.
func shortSum(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:8])
}

// The board is more than one file now, and every one of them has to be both
// EMBEDDED and SERVED.
//
// `go:embed web` takes the subdirectory, and `webHandler` serves the tree, so
// neither of these needs a change to work. That is exactly why they are checked:
// nothing in the build says a file is missing. A script that is not embedded is
// a 404 at load, the rest of the page keeps running, and what a human sees is
// one feature quietly absent.

func TestEveryScriptThePageLoadsIsServed(t *testing.T) {
	h := webHandler("")

	get := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	// `/` rather than `/index.html`: the file server redirects the explicit
	// name to the directory, which is what a browser asks for anyway.
	for _, path := range []string{"/", "/board.css"} {
		if rec := get(path); rec.Code != http.StatusOK || rec.Body.Len() == 0 {
			t.Errorf("%s answered %d with %d bytes. it is part of the board and has to "+
				"be embedded and served", path, rec.Code, rec.Body.Len())
		}
	}

	for _, f := range scriptFiles(t) {
		rec := get("/js/" + f.name)
		if rec.Code != http.StatusOK {
			t.Errorf("the page loads /js/%s and the handler answers %d", f.name, rec.Code)
			continue
		}
		if rec.Body.String() != f.body {
			t.Errorf("/js/%s is served as something other than the file on disk", f.name)
		}
	}
}

// The board must not be cached, and that has to hold for the new files too.
//
// A cached script after a rebuild looks exactly like a bug that was not fixed,
// and with the board split across two dozen files there are now two dozen ways
// to get half an old board and half a new one.
func TestTheSplitBoardIsNotCached(t *testing.T) {
	h := webHandler("")
	for _, path := range []string{"/", "/board.css", "/js/core.js"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if !strings.Contains(rec.Header().Get("Cache-Control"), "no-store") {
			t.Errorf("%s is served as cacheable: %q", path, rec.Header().Get("Cache-Control"))
		}
	}
}

// The build id has to move when any part of the board moves.
//
// It used to hash `index.html`, which was the whole board. It is now a loader,
// so a hash of it alone would be identical across a change to any script, and
// the reload that happens when a new daemon is installed would stop firing for
// the changes it exists for. There is no way to ask the running binary about a
// tree it does not have, so this asserts the property that makes that true:
// every file under `web/` is part of the hash.
func TestTheBuildIDCoversMoreThanThePage(t *testing.T) {
	if BuildID == "" {
		t.Fatal("there is no build id, so a board and a daemon can never be told apart")
	}

	raw, err := os.ReadFile(filepath.Join("web", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	// Hashing the page alone is what this replaced. If the two ever agree
	// again, somebody has put it back.
	if BuildID == shortSum(raw) {
		t.Error("the build id is a hash of index.html alone. it has to cover every " +
			"file under web/, or it stops changing when a script does")
	}
}
