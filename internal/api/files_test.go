package api

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// Moving files to and from a card's directory.
//
// The two halves are tested differently on purpose, because they are different
// risks. Upload takes NO caller path, so its tests are about bounds and where
// bytes land. Download takes one, so its tests are about what it refuses.

func fileServer(t *testing.T) (*Server, *store.Store, string) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "atrium.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	work := t.TempDir()
	real, err := filepath.EvalSymlinks(work)
	if err != nil {
		t.Fatal(err)
	}
	return New(st), st, real
}

func cardIn(t *testing.T, st *store.Store, dir string) *store.Task {
	t.Helper()
	task, _, err := st.Register(store.Observed{
		WireName: "files", Worktree: filepath.ToSlash(dir), Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func uploadReq(t *testing.T, id string, names []string, body []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, n := range names {
		part, err := mw.CreateFormFile("file", n)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/v1/tasks/"+id+"/files", &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.SetPathValue("id", id)
	return r
}

func TestAnUploadLandsUnderTheCardsDirectory(t *testing.T) {
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)

	w := httptest.NewRecorder()
	s.uploadFiles(w, uploadReq(t, task.ID, []string{"screenshot.png"}, []byte("bytes")))
	if w.Code != http.StatusOK {
		t.Fatalf("upload answered %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), ".atrium/incoming") {
		t.Fatalf("the answer does not name where it went: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "screenshot.png") {
		t.Fatalf("the file lost its name: %s", w.Body.String())
	}

	// Under the card's directory, in the incoming folder, and nowhere else.
	// Not the directory root: a repository does not want a screenshot in its
	// root turning up in `git status`.
	entries, err := os.ReadDir(filepath.Join(work, ".atrium", "incoming"))
	if err != nil {
		t.Fatalf("nothing landed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("%d files landed", len(entries))
	}
	if !strings.HasSuffix(entries[0].Name(), "screenshot.png") {
		t.Fatalf("it landed as %q", entries[0].Name())
	}
}

// The destination is computed, so a caller trying to name one has nothing to
// name it with. This is the whole security argument for the first version, and
// it is worth a test that says so.
func TestAnUploadCannotChooseWhereItLands(t *testing.T) {
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)

	w := httptest.NewRecorder()
	s.uploadFiles(w, uploadReq(t, task.ID,
		[]string{`../../../../evil.txt`, `C:\windows\system32\also-evil.dll`}, []byte("x")))
	if w.Code != http.StatusOK {
		t.Fatalf("upload answered %d: %s", w.Code, w.Body.String())
	}

	dir := filepath.Join(work, ".atrium", "incoming")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("%d files landed", len(entries))
	}
	for _, e := range entries {
		if strings.ContainsAny(e.Name(), `/\`) {
			t.Fatalf("a separator survived into the name: %q", e.Name())
		}
		if strings.Contains(e.Name(), "..") {
			t.Fatalf("a traversal survived into the name: %q", e.Name())
		}
	}
	// And nothing escaped upward.
	if _, err := os.Stat(filepath.Join(work, "evil.txt")); err == nil {
		t.Fatal("a file escaped the incoming directory")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(work), "evil.txt")); err == nil {
		t.Fatal("a file escaped the card's directory entirely")
	}
}

// Several files in one paste do not overwrite each other, even landing in the
// same second under the same name.
func TestTwoFilesInOnePasteBothSurvive(t *testing.T) {
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)

	w := httptest.NewRecorder()
	s.uploadFiles(w, uploadReq(t, task.ID, []string{"a.png", "a.png"}, []byte("x")))
	if w.Code != http.StatusOK {
		t.Fatalf("upload answered %d: %s", w.Code, w.Body.String())
	}
	entries, err := os.ReadDir(filepath.Join(work, ".atrium", "incoming"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("two files with one name produced %d", len(entries))
	}
}

// A card with no directory has nowhere to put anything, and saying so is
// better than inventing somewhere.
func TestUploadingToACardWithNoDirectoryIsRefused(t *testing.T) {
	s, st, _ := fileServer(t)
	task, _, err := st.Register(store.Observed{WireName: "nodir", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	s.uploadFiles(w, uploadReq(t, task.ID, []string{"a.png"}, []byte("x")))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("answered %d", w.Code)
	}
}

// Uploading into the working directory of a session that ended is almost
// always a mistake about which card is which.
func TestUploadingToAnArchivedCardIsRefused(t *testing.T) {
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)
	if err := st.SetStatus(task.ID, store.StatusDead); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Archive(0, store.StatusDead); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	s.uploadFiles(w, uploadReq(t, task.ID, []string{"a.png"}, []byte("x")))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("answered %d: %s", w.Code, w.Body.String())
	}
}

// Download is the first caller-supplied path in atrium that is bounded.

func downloadReq(id, path string) *http.Request {
	r := httptest.NewRequest("GET", "/v1/tasks/"+id+"/files?path="+path, nil)
	r.SetPathValue("id", id)
	return r
}

func TestDownloadingAFileInsideWorks(t *testing.T) {
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)
	if err := os.WriteFile(filepath.Join(work, "notes.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	s.downloadFile(w, downloadReq(task.ID, filepath.ToSlash(filepath.Join(work, "notes.txt"))))
	if w.Code != http.StatusOK {
		t.Fatalf("answered %d: %s", w.Code, w.Body.String())
	}
	if w.Body.String() != "hello" {
		t.Fatalf("served %q", w.Body.String())
	}
}

// Everything is an attachment and nothing is sniffed. Serving an HTML file
// from a working directory inline would be serving attacker-authored script on
// the board's own origin, and the board holds the settings and every card.
func TestEverythingIsServedAsAnAttachment(t *testing.T) {
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)
	if err := os.WriteFile(filepath.Join(work, "page.html"),
		[]byte("<script>alert(1)</script>"), 0o600); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	s.downloadFile(w, downloadReq(task.ID, filepath.ToSlash(filepath.Join(work, "page.html"))))
	if w.Code != http.StatusOK {
		t.Fatalf("answered %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Fatalf("served html as %q", ct)
	}
	if !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment") {
		t.Fatal("html was served inline")
	}
	if w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("the browser is allowed to sniff the type")
	}
}

func TestDownloadingOutsideTheCardIsRefused(t *testing.T) {
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)

	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, p := range []string{
		filepath.ToSlash(secret),
		"../../etc/passwd",
		"..%2F..%2Fsecret.txt",
	} {
		w := httptest.NewRecorder()
		s.downloadFile(w, downloadReq(task.ID, p))
		if w.Code == http.StatusOK {
			t.Fatalf("%q was served: %s", p, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "nope") {
			t.Fatalf("%q leaked its contents", p)
		}
	}
}

// One status for "outside" and for "does not exist", so this cannot be used to
// find out what is on the machine outside the card.
func TestOutsideAndMissingAnswerTheSame(t *testing.T) {
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)

	outside := t.TempDir()
	real := filepath.Join(outside, "exists.txt")
	if err := os.WriteFile(real, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	existing := httptest.NewRecorder()
	s.downloadFile(existing, downloadReq(task.ID, filepath.ToSlash(real)))
	missing := httptest.NewRecorder()
	s.downloadFile(missing, downloadReq(task.ID,
		filepath.ToSlash(filepath.Join(outside, "does-not-exist.txt"))))

	if existing.Code != missing.Code {
		t.Fatalf("an existence oracle: %d for a real file, %d for a missing one",
			existing.Code, missing.Code)
	}
}

// A directory is refused rather than zipped. A zip stream is a second
// mechanism with its own bounds, and the case that comes up is one file.
func TestDownloadingADirectoryIsRefused(t *testing.T) {
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)

	w := httptest.NewRecorder()
	s.downloadFile(w, downloadReq(task.ID, filepath.ToSlash(work)))
	if w.Code == http.StatusOK {
		t.Fatal("a directory was served")
	}
}

func TestDownloadingNeedsAPath(t *testing.T) {
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)

	w := httptest.NewRecorder()
	s.downloadFile(w, downloadReq(task.ID, ""))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("answered %d", w.Code)
	}
}
