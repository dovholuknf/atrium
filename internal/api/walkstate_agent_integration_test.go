//go:build integration

package api

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func walkSync(s *Server, id string) (int, string) {
	r := httptest.NewRequest("POST", "/v1/tasks/"+id+"/walk/agent", nil)
	r.SetPathValue("id", id)
	w := httptest.NewRecorder()
	s.walkAgentSync(w, r)
	return w.Code, strings.TrimSpace(w.Body.String())
}

func TestWalkAgentFileBecomesCommentsAndRepliesOnce(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)
	if err := os.MkdirAll(filepath.Join(work, ".atrium"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(work, filepath.FromSlash(walkAgentFile))
	line := `{"file":"a.go","line":3,"text":"why not a map?","scope":"a.go","summary":"s"}` + "\n" + `not json` + "\n" + `{"file":"","line":3,"text":"x"}` + "\n"
	if err := os.WriteFile(file, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	code, body := walkSync(s, task.ID)
	if code != 200 || strings.Count(body, `"who":"agent"`) != 1 || !strings.Contains(body, `"summary":"s"`) {
		t.Fatalf("one comment expected: %d %s", code, body)
	}
	id := walkHash("a", "a.go", "3", "why not a map?")
	f, _ := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0o644)
	_, _ = f.WriteString(`{"reply":"` + id + `","text":"because order"}` + "\n" + `{"reply":"nope","text":"lost"}` + "\n")
	f.Close()
	for i := 0; i < 2; i++ {
		if code, body = walkSync(s, task.ID); code != 200 || strings.Count(body, `"who":"agent"`) != 2 || strings.Count(body, "because order") != 1 {
			t.Fatalf("one comment with one reply expected, run %d: %d %s", i, code, body)
		}
	}
	if code, _ := walkStateCall(s, "PUT", task.ID, `{"comments":[],"gone":["`+id+`"]}`); code != 200 {
		t.Fatalf("put: %d", code)
	}
	if _, body = walkSync(s, task.ID); strings.Contains(body, "why not a map?") {
		t.Fatalf("a deleted comment came back: %s", body)
	}
}

func TestWalkStateNamesThePullRequestItIsFor(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)
	if _, body := walkStateCall(s, "GET", task.ID, ""); strings.Contains(body, "pr_url") {
		t.Fatalf("a card with no pull request named one: %s", body)
	}
}
