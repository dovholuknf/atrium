//go:build integration

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func dropReq(id, query string, body []byte) *http.Request {
	r := httptest.NewRequest("PUT", "/v1/tasks/"+id+"/files/drop?"+query, bytes.NewReader(body))
	r.SetPathValue("id", id)
	return r
}

func dropDo(s *Server, id, query string, body []byte) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	s.dropIn(w, dropReq(id, query, body))
	return w
}

func dropAnswer(t *testing.T, w *httptest.ResponseRecorder) (path, rel string) {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("answered %d: %s", w.Code, w.Body.String())
	}
	var got struct{ Path, Rel string }
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got.Path, got.Rel
}

func TestDropInLandsInIncoming(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)

	path, rel := dropAnswer(t, dropDo(s, task.ID, "name="+url.QueryEscape("usage.jsonl"), []byte("rows")))
	if !strings.HasPrefix(rel, ".atrium/incoming/") || !strings.HasSuffix(rel, "-usage.jsonl") {
		t.Fatalf("rel %q", rel)
	}
	if strings.Contains(path, `\`) {
		t.Fatalf("path %q has backslashes", path)
	}
	b, err := os.ReadFile(filepath.FromSlash(path))
	if err != nil || string(b) != "rows" {
		t.Fatalf("read back %q, %v", b, err)
	}
	// A name that tries to be a path is only a name.
	path, _ = dropAnswer(t, dropDo(s, task.ID, "name="+url.QueryEscape("../../evil.txt"), []byte("x")))
	if !strings.Contains(path, ".atrium/incoming/") {
		t.Fatalf("a name climbed: %s", path)
	}
}

func TestDropInIgnoresScrapMode(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)
	if err := st.SetSetting(SettingPasteKeep, "scrap"); err != nil {
		t.Fatal(err)
	}
	old := ScrapDir
	ScrapDir = t.TempDir()
	defer func() { ScrapDir = old }()

	path, rel := dropAnswer(t, dropDo(s, task.ID, "name=a.txt", []byte("x")))
	if strings.HasPrefix(filepath.FromSlash(path), ScrapDir) || !strings.HasPrefix(rel, ".atrium/incoming/") {
		t.Fatalf("a drop followed paste_keep: %s", path)
	}
	if entries, _ := os.ReadDir(ScrapDir); len(entries) != 0 {
		t.Fatalf("%d things in the scrap dir", len(entries))
	}
}

func TestDropInDestInsideTheCard(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)

	path, rel := dropAnswer(t, dropDo(s, task.ID, "name=ignored&dest="+url.QueryEscape("out/deep/data.jsonl"), []byte("d")))
	if rel != "out/deep/data.jsonl" {
		t.Fatalf("rel %q", rel)
	}
	if b, err := os.ReadFile(filepath.FromSlash(path)); err != nil || string(b) != "d" {
		t.Fatalf("read back %q, %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(work, ".atrium")); err == nil {
		t.Fatal("a drop with a dest made .atrium/incoming")
	}
}

func TestDropInRefusesAbsoluteAndDotDot(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)
	outside := t.TempDir()

	for _, dest := range []string{
		filepath.ToSlash(filepath.Join(outside, "x")), "../x", "a/../../x", `a\..\..\x`, "/etc/x", `\x`, "..",
	} {
		w := dropDo(s, task.ID, "dest="+url.QueryEscape(dest), []byte("x"))
		if w.Code != http.StatusForbidden {
			t.Fatalf("dest %q answered %d: %s", dest, w.Code, w.Body.String())
		}
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatal("something was written outside the card")
	}
}

func TestDropInRefusesALinkOutOfTheCard(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(work, "out")); err != nil {
		t.Skipf("cannot create a symlink on this machine: %v", err)
	}

	for _, dest := range []string{"out/x", "out/sub/x"} {
		w := dropDo(s, task.ID, "dest="+url.QueryEscape(dest), []byte("x"))
		if w.Code != http.StatusForbidden {
			t.Fatalf("dest %q answered %d: %s", dest, w.Code, w.Body.String())
		}
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatalf("%d things were written outside the card", len(entries))
	}
}

func TestDropInRefusesOverwriteWithoutReplace(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)
	q := "dest=" + url.QueryEscape("out/a.txt")

	dropAnswer(t, dropDo(s, task.ID, q, []byte("one")))
	if w := dropDo(s, task.ID, q, []byte("two")); w.Code != http.StatusConflict {
		t.Fatalf("answered %d", w.Code)
	}
	if b, _ := os.ReadFile(filepath.Join(work, "out", "a.txt")); string(b) != "one" {
		t.Fatalf("the file was changed: %q", b)
	}
	dropAnswer(t, dropDo(s, task.ID, q+"&replace=1", []byte("three")))
	if b, _ := os.ReadFile(filepath.Join(work, "out", "a.txt")); string(b) != "three" {
		t.Fatalf("replace did not replace: %q", b)
	}
	entries, _ := os.ReadDir(filepath.Join(work, "out"))
	if len(entries) != 1 {
		t.Fatalf("a temp file was left behind: %v", entries)
	}
}

func TestDropInRefusesArchivedAndNoDir(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	nodir, _, err := st.Register(store.Observed{WireName: "nodir", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"name=a", "dest=a/b"} {
		if w := dropDo(s, nodir.ID, q, []byte("x")); w.Code != http.StatusBadRequest {
			t.Fatalf("no directory, %s: answered %d", q, w.Code)
		}
	}
	task := cardIn(t, st, work)
	if err := st.SetStatus(task.ID, store.StatusDead); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Archive(0, store.StatusDead); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"name=a", "dest=a/b"} {
		if w := dropDo(s, task.ID, q, []byte("x")); w.Code != http.StatusBadRequest {
			t.Fatalf("archived, %s: answered %d", q, w.Code)
		}
	}
}

func TestDropInOverTheCapIs413(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)

	w := dropDo(s, task.ID, "dest=big.bin", make([]byte, maxUploadFile+1))
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("answered %d", w.Code)
	}
	if _, err := os.Stat(filepath.Join(work, "big.bin")); err == nil {
		t.Fatal("a partial file was left")
	}
}

func TestDropInEmptyBodyIs400AndWritesNothing(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)

	if w := dropDo(s, task.ID, "dest=e.txt", nil); w.Code != http.StatusBadRequest {
		t.Fatalf("answered %d", w.Code)
	}
	if _, err := os.Stat(filepath.Join(work, "e.txt")); err == nil {
		t.Fatal("an empty file was left")
	}
}

func TestDropInDirectoryTargetIs400(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)
	if err := os.Mkdir(filepath.Join(work, "d"), 0o700); err != nil {
		t.Fatal(err)
	}
	if w := dropDo(s, task.ID, "dest=d&replace=1", []byte("x")); w.Code != http.StatusBadRequest {
		t.Fatalf("answered %d", w.Code)
	}
}

func TestDropInCaseFoldedEscapeIsRefused(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("case folding is a Windows rule")
	}
	t.Parallel()
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)
	outside := t.TempDir()
	// The same absolute path in the other case is still absolute, and still not inside the card.
	dest := filepath.ToSlash(strings.ToUpper(filepath.Join(outside, "x")))
	if w := dropDo(s, task.ID, "dest="+url.QueryEscape(dest), []byte("x")); w.Code != http.StatusForbidden {
		t.Fatalf("answered %d", w.Code)
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatal("something was written outside the card")
	}
}

// closeAtEOF closes the store when the body has been read, so the file is written and the event cannot be.
type closeAtEOF struct {
	*bytes.Reader
	st *store.Store
}

func (c closeAtEOF) Read(p []byte) (int, error) {
	n, err := c.Reader.Read(p)
	if err != nil {
		c.st.Close()
	}
	return n, err
}

func TestDropInAnswers200WhenTheEventCannotBeWritten(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)
	r := dropReq(task.ID, "dest=out.txt", nil)
	r.Body = http.NoBody
	r.Body = readCloser{closeAtEOF{bytes.NewReader([]byte("hello")), st}}
	w := httptest.NewRecorder()
	s.dropIn(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("answered %d: %s", w.Code, w.Body.String())
	}
	if b, err := os.ReadFile(filepath.Join(work, "out.txt")); err != nil || string(b) != "hello" {
		t.Fatalf("file = %q, %v", b, err)
	}
}

type readCloser struct{ r closeAtEOF }

func (c readCloser) Read(p []byte) (int, error) { return c.r.Read(p) }
func (c readCloser) Close() error               { return nil }

func TestDropInRefusesAColonInDest(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)
	for _, dest := range []string{"C:/x", "C:x", "a/b:stream", "sub/name.txt:ads"} {
		if w := dropDo(s, task.ID, "dest="+url.QueryEscape(dest), []byte("x")); w.Code != http.StatusForbidden {
			t.Errorf("dest %q answered %d", dest, w.Code)
		}
	}
}
