//go:build integration

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

func walkDiffGet(t *testing.T, s *Server, id string) (int, walkDiffOut, string) {
	t.Helper()
	r := httptest.NewRequest("GET", "/v1/tasks/"+id+"/diff", nil)
	r.SetPathValue("id", id)
	w := httptest.NewRecorder()
	s.walkDiff(w, r)
	var out walkDiffOut
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("unreadable answer: %v", err)
		}
	}
	return w.Code, out, w.Body.String()
}

// A TWO-COMMIT BRANCH IS WALKED AGAINST ITS BASE: the files of both commits are in the patch and nothing of main's.
func TestWalkDiffOfATwoCommitBranchListsItsFiles(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	git(t, work, "init", "-q", "-b", "main")
	write := func(rel, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(work, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("base.txt", "base\n")
	git(t, work, "add", ".")
	git(t, work, "commit", "-q", "-m", "base")
	git(t, work, "switch", "-q", "-c", "feature")
	write("one.txt", "one\n")
	git(t, work, "add", ".")
	git(t, work, "commit", "-q", "-m", "one")
	write("base.txt", "base\nchanged\n")
	write("two.txt", "two\n")
	git(t, work, "add", ".")
	git(t, work, "commit", "-q", "-m", "two")
	task := cardIn(t, st, work)

	code, out, body := walkDiffGet(t, s, task.ID)
	if code != http.StatusOK {
		t.Fatalf("%d %s", code, body)
	}
	if out.Branch != "feature" || out.Base != "main" {
		t.Errorf("branch %q against %q", out.Branch, out.Base)
	}
	for _, f := range []string{"b/one.txt", "b/two.txt", "b/base.txt", "+changed"} {
		if !strings.Contains(out.Diff, f) {
			t.Errorf("the patch has no %s:\n%s", f, out.Diff)
		}
	}
}

// A WORKTREE WITH NOTHING TO COMPARE WITH SAYS SO in words, rather than answering an empty patch.
func TestWalkDiffWithNoBaseSaysWhat(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	git(t, work, "init", "-q", "-b", "topic")
	if err := os.WriteFile(filepath.Join(work, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, work, "add", ".")
	git(t, work, "commit", "-q", "-m", "a")
	task := cardIn(t, st, work)

	code, _, body := walkDiffGet(t, s, task.ID)
	if code != http.StatusConflict || !strings.Contains(body, "no base to compare topic with") {
		t.Errorf("%d %s", code, body)
	}
}
