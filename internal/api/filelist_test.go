package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Finding a file so it can be taken back out.
//
// The risk here is the same as download's and so are the tests: every path
// comes from the caller, so the interesting cases are the ones it refuses.

func listReq(id, path string) *http.Request {
	url := "/v1/tasks/" + id + "/files/list"
	if path != "" {
		url += "?path=" + path
	}
	r := httptest.NewRequest("GET", url, nil)
	r.SetPathValue("id", id)
	return r
}

type listing struct {
	Path      string `json:"path"`
	Root      string `json:"root"`
	Parent    string `json:"parent"`
	Truncated bool   `json:"truncated"`
	Entries   []struct {
		Name string `json:"name"`
		Path string `json:"path"`
		Dir  bool   `json:"dir"`
		Size int64  `json:"size"`
	} `json:"entries"`
}

func doList(t *testing.T, s *Server, id, path string) (listing, int) {
	t.Helper()
	w := httptest.NewRecorder()
	s.listFiles(w, listReq(id, path))
	var out listing
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("unreadable listing: %v", err)
		}
	}
	return out, w.Code
}

func TestListingShowsFilesAndDirectories(t *testing.T) {
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)
	if err := os.MkdirAll(filepath.Join(work, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "notes.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, code := doList(t, s, task.ID, filepath.ToSlash(work))
	if code != http.StatusOK {
		t.Fatalf("answered %d", code)
	}
	var sawFile, sawDir bool
	for _, e := range got.Entries {
		if e.Name == "notes.txt" && !e.Dir && e.Size == 5 {
			sawFile = true
		}
		if e.Name == "sub" && e.Dir {
			sawDir = true
		}
	}
	if !sawFile {
		t.Fatal("the file is missing, which is the whole point of the listing")
	}
	if !sawDir {
		// Unlike browse.go, which drops files because a launch wants a
		// directory. Here both matter.
		t.Fatal("the directory is missing")
	}
}

// The place something just wrote to is the place you usually want, so an
// unqualified listing starts there when it exists.
func TestAnUnqualifiedListingStartsAtIncoming(t *testing.T) {
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)

	// Nothing there yet: the card's own directory.
	got, code := doList(t, s, task.ID, "")
	if code != http.StatusOK {
		t.Fatalf("answered %d", code)
	}
	if !strings.EqualFold(filepath.Clean(got.Path), filepath.Clean(work)) {
		t.Fatalf("started at %q, wanted the card's directory", got.Path)
	}

	// Once an upload has landed, that is where to look.
	incoming := filepath.Join(work, ".atrium", "incoming")
	if err := os.MkdirAll(incoming, 0o700); err != nil {
		t.Fatal(err)
	}
	got, _ = doList(t, s, task.ID, "")
	if !strings.Contains(filepath.ToSlash(got.Path), "incoming") {
		t.Fatalf("started at %q, wanted the incoming directory", got.Path)
	}
}

// Every path the board uses comes from the server, including where up goes,
// so the board never does path arithmetic.
func TestParentIsServerComputedAndStopsAtTheCard(t *testing.T) {
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)
	sub := filepath.Join(work, "a", "b")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}

	deep, _ := doList(t, s, task.ID, filepath.ToSlash(sub))
	if deep.Parent == "" {
		t.Fatal("no way back up from a subdirectory")
	}

	// Walking up repeatedly must stop at the card, not climb out of it.
	at := deep.Path
	for i := 0; i < 5; i++ {
		got, code := doList(t, s, task.ID, at)
		if code != http.StatusOK {
			t.Fatalf("walking up answered %d at %q", code, at)
		}
		if got.Parent == "" {
			if !strings.EqualFold(filepath.Clean(got.Path), filepath.Clean(work)) {
				t.Fatalf("ran out of parents at %q, which is not the card's directory", got.Path)
			}
			return
		}
		at = got.Parent
	}
	t.Fatal("walking up never reached the card's own directory")
}

func TestListingOutsideTheCardIsRefused(t *testing.T) {
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("no"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, p := range []string{
		filepath.ToSlash(outside),
		"../..",
		filepath.ToSlash(filepath.Join(work, "..")),
	} {
		_, code := doList(t, s, task.ID, p)
		if code == http.StatusOK {
			t.Fatalf("%q was listed", p)
		}
	}
}

// Outside and missing answer the same, so this is not an oracle for what is on
// the machine. Same rule download follows.
func TestListingOutsideAndMissingAnswerTheSame(t *testing.T) {
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)
	outside := t.TempDir()

	_, exists := doList(t, s, task.ID, filepath.ToSlash(outside))
	_, missing := doList(t, s, task.ID, filepath.ToSlash(filepath.Join(outside, "nope")))
	if exists != missing {
		t.Fatalf("an existence oracle: %d for a real directory, %d for a missing one",
			exists, missing)
	}
}

// A working directory with a node_modules in it has more entries than anybody
// is going to read. The answer is a shorter list that says so, not a slow
// board.
func TestAHugeDirectoryIsBoundedAndSaysSo(t *testing.T) {
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)
	big := filepath.Join(work, "many")
	if err := os.MkdirAll(big, 0o700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxListEntries+50; i++ {
		if err := os.WriteFile(filepath.Join(big, "f"+strings.Repeat("0", 4)+
			itoa(i)+".txt"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got, code := doList(t, s, task.ID, filepath.ToSlash(big))
	if code != http.StatusOK {
		t.Fatalf("answered %d", code)
	}
	if len(got.Entries) > maxListEntries {
		t.Fatalf("returned %d entries, over the %d cap", len(got.Entries), maxListEntries)
	}
	if !got.Truncated {
		t.Fatal("a truncated listing does not say it was truncated")
	}
}

// Asking for a file answers with its directory, because clicking a file in a
// picker meaning "show me where that is" is a reasonable thing to have meant.
func TestAskingForAFileListsItsDirectory(t *testing.T) {
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)
	f := filepath.Join(work, "notes.txt")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, code := doList(t, s, task.ID, filepath.ToSlash(f))
	if code != http.StatusOK {
		t.Fatalf("answered %d", code)
	}
	if !strings.EqualFold(filepath.Clean(got.Path), filepath.Clean(work)) {
		t.Fatalf("listed %q", got.Path)
	}
}

// A card with no directory has nothing to list, and saying so beats an empty
// panel that looks like an empty directory.
func TestListingACardWithNoDirectoryIsRefused(t *testing.T) {
	s, st, _ := fileServer(t)
	task := cardIn(t, st, "")
	_, code := doList(t, s, task.ID, "")
	if code != http.StatusBadRequest {
		t.Fatalf("answered %d", code)
	}
}

// itoa without importing strconv into a test that needs nothing else from it.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
