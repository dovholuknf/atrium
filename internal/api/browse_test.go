package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// What the directory picker refuses.
//
// The tests are named for the ways it gets broken rather than for the
// behaviour, because every one of these is a real technique and the point of
// the file is that a later change has to keep answering them.
//
// The picker was deliberately unbounded for a long time: on loopback it is a
// directory picker and the daemon runs as you. What changed is that the board
// can be published, and a share terminates on this machine so every request
// over one presents as loopback. See browseroots.go.

func browseServer(t *testing.T) (*Server, string) {
	t.Helper()
	srv, st, work := fileServer(t)
	// One card, so the default root set is this directory and nothing else.
	// The home directory is deliberately NOT in play here: a test that passes
	// because the temp dir happens to be under it proves nothing.
	cardIn(t, st, work)
	if err := st.SetSetting(SettingBrowseRoots, filepath.ToSlash(work)); err != nil {
		t.Fatal(err)
	}
	return srv, work
}

func browseAt(t *testing.T, srv *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	url := "/v1/browse"
	if path != "" {
		url += "?path=" + path
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	return rec
}

func TestBrowseListsInsideARoot(t *testing.T) {
	srv, work := browseServer(t)
	if err := os.MkdirAll(filepath.Join(work, "project"), 0o700); err != nil {
		t.Fatal(err)
	}

	rec := browseAt(t, srv, filepath.ToSlash(work))
	if rec.Code != http.StatusOK {
		t.Fatalf("browsing a root answered %d: %s", rec.Code, rec.Body)
	}
	var body struct {
		Entries []browseEntry `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Entries) != 1 || body.Entries[0].Name != "project" {
		t.Fatalf("want the one directory in the root, got %+v", body.Entries)
	}
}

// The obvious one, and the one a lexical check does catch.
func TestBrowseRefusesTraversal(t *testing.T) {
	srv, work := browseServer(t)
	rec := browseAt(t, srv, filepath.ToSlash(filepath.Join(work, "..")))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("traversal out of the root answered %d, want 403: %s", rec.Code, rec.Body)
	}
}

// A path with nothing to do with any root.
func TestBrowseRefusesAnUnrelatedPath(t *testing.T) {
	srv, _ := browseServer(t)
	elsewhere := t.TempDir()
	rec := browseAt(t, srv, filepath.ToSlash(elsewhere))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("an unrelated directory answered %d, want 403: %s", rec.Code, rec.Body)
	}
}

// THE ONE A STRING COMPARISON MISSES. `work-evil` is not inside `work`, and
// `strings.HasPrefix` says it is.
func TestBrowseRefusesASiblingWithTheRootAsAPrefix(t *testing.T) {
	srv, work := browseServer(t)
	evil := work + "-evil"
	if err := os.MkdirAll(evil, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(evil) })

	rec := browseAt(t, srv, filepath.ToSlash(evil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a sibling sharing the root's prefix answered %d, want 403: %s",
			rec.Code, rec.Body)
	}
}

// THE ONE THAT MATTERS. A symlink inside a root pointing out of it passes every
// lexical test there is, which is the whole reason safepath touches the
// filesystem.
func TestBrowseRefusesASymlinkOutOfTheRoot(t *testing.T) {
	srv, work := browseServer(t)
	outside := t.TempDir()
	link := filepath.Join(work, "escape")
	if err := os.Symlink(outside, link); err != nil {
		// Windows needs developer mode or elevation for this. Skipping is
		// honest: the rule is enforced by safepath, which has its own tests.
		t.Skipf("cannot create a symlink here: %v", err)
	}

	rec := browseAt(t, srv, filepath.ToSlash(link))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a symlink out of the root answered %d, want 403: %s", rec.Code, rec.Body)
	}
}

// Outside and missing answer the same, or the refusal is an oracle for what
// exists on the machine.
func TestBrowseRefusesMissingAndOutsideIdentically(t *testing.T) {
	srv, _ := browseServer(t)
	missing := browseAt(t, srv, "/no/such/place/anywhere")
	outside := browseAt(t, srv, filepath.ToSlash(t.TempDir()))
	if missing.Code != outside.Code {
		t.Fatalf("missing answered %d and outside answered %d. they must not be "+
			"tellable apart", missing.Code, outside.Code)
	}
}

// With no path, the picker offers the roots rather than the machine's drives.
func TestBrowseWithNoPathOffersTheRoots(t *testing.T) {
	srv, work := browseServer(t)
	rec := browseAt(t, srv, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("the root list answered %d: %s", rec.Code, rec.Body)
	}
	var body struct {
		Roots   bool          `json:"roots"`
		Entries []browseEntry `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Roots {
		t.Fatal("the root listing should say it is one")
	}
	if len(body.Entries) != 1 {
		t.Fatalf("want exactly the configured root, got %+v", body.Entries)
	}
	if got, want := body.Entries[0].Path, filepath.ToSlash(work); got != want {
		t.Fatalf("root is %q, want %q", got, want)
	}
}

// Stepping up from a root stops at the root, rather than handing back its
// parent, which is the directory the picker is not allowed to see.
func TestBrowseUpFromARootHasNoParent(t *testing.T) {
	srv, work := browseServer(t)
	rec := browseAt(t, srv, filepath.ToSlash(work))
	var body struct {
		Parent string `json:"parent"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Parent != "" {
		t.Fatalf("parent of a root is %q, want empty so `up` returns to the root list",
			body.Parent)
	}
}

// A card's directory is browsable without configuring anything, which is what
// keeps the default useful.
func TestBrowseDefaultsToCardDirectories(t *testing.T) {
	srv, st, work := fileServer(t)
	cardIn(t, st, work)
	if err := os.MkdirAll(filepath.Join(work, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}

	rec := browseAt(t, srv, filepath.ToSlash(work))
	if rec.Code != http.StatusOK {
		t.Fatalf("a card's own directory answered %d, want it browsable: %s",
			rec.Code, rec.Body)
	}
}
