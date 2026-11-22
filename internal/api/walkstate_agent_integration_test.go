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

func TestWalkMergeAnswersTheWholeThreadOnceInOrder(t *testing.T) {
	state := map[string]any{"thread": []any{map[string]any{"q": "one"}, map[string]any{"q": "two"}}}
	lines := [][]byte{[]byte(`{"reply":"whole","text":"first"}`)}
	if n := walkMerge(state, lines, ""); n != 1 {
		t.Fatalf("added %d, want 1", n)
	}
	lines = append(lines, []byte(`{"reply":"whole","text":"second"}`), []byte(`{"reply":"whole","text":"third"}`))
	if n := walkMerge(state, lines, ""); n != 1 {
		t.Fatalf("added %d on the second read, want 1", n)
	}
	th := state["thread"].([]any)
	if a := th[0].(map[string]any)["a"]; a != "first" {
		t.Fatalf("first answer %v", a)
	}
	if a := th[1].(map[string]any)["a"]; a != "second" {
		t.Fatalf("second answer %v", a)
	}
}

func TestWalkMergeKeepsAReviewCardAnswerAsTheReviewCards(t *testing.T) {
	t.Parallel()
	id := walkHash("a", "a.go", "3", "q")
	state := map[string]any{"comments": []any{map[string]any{"id": id, "file": "a.go", "line": 3.0, "who": "agent", "text": "q"}}}
	lines := [][]byte{[]byte(`{"reply":"` + id + `","who":"reviewer","text":"keep it"}`), []byte(`{"reply":"` + id + `","who":"root","text":"plain"}`)}
	if n := walkMerge(state, lines, ""); n != 2 {
		t.Fatalf("two turns expected, got %d", n)
	}
	turns := state["comments"].([]any)[0].(map[string]any)["turns"].([]any)
	if turns[0].(map[string]any)["who"] != "reviewer" || turns[1].(map[string]any)["who"] != "agent" {
		t.Fatalf("%v", turns)
	}
}

const walkMapDiff = "diff --git a/f b/f\n--- a/f\n+++ b/f\n" +
	"@@ -1,3 +1,5 @@\n a\n+new1\n+new2\n b\n c\n" +
	"@@ -10,3 +12,2 @@\n x\n-gone\n y\n" +
	"@@ -20,1 +21,1 @@\n-old\n+edited\n"

func TestWalkLineMap(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		line int
		want int
		ok   bool
	}{
		{"above every hunk", 1, 1, true},
		{"after an insert above", 3, 5, true},
		{"between hunks", 7, 9, true},
		{"deleted", 11, 0, false},
		{"after a delete", 12, 13, true},
		{"edited", 20, 0, false},
		{"past the last hunk", 30, 31, true},
	} {
		got, ok := walkLineMap(walkMapDiff, c.line)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: line %d mapped to %d,%v want %d,%v", c.name, c.line, got, ok, c.want, c.ok)
		}
	}
	if got, ok := walkLineMap("", 4); got != 4 || !ok {
		t.Errorf("an empty diff moved the line: %d %v", got, ok)
	}
}

func TestWalkMergeKeepsTwoCommentsOnOneLineApart(t *testing.T) {
	t.Parallel()
	state := map[string]any{}
	one := `{"file":"a.go","line":3,"text":"first"}`
	two := `{"file":"a.go","line":3,"text":"second","sha":"abc1234"}`
	idOne, idTwo := walkHash("a", "a.go", "3", "first"), walkHash("a", "a.go", "3", "second")
	lines := [][]byte{[]byte(one), []byte(two),
		[]byte(`{"reply":"` + idTwo + `","text":"re second"}`), []byte(`{"reply":"` + idOne + `","text":"re first"}`),
		[]byte(`{"reply":"a.go:3","text":"by line"}`)}
	if n := walkMerge(state, lines, "def5678"); n != 4 {
		t.Fatalf("two comments and two replies expected, got %d", n)
	}
	for _, c := range state["comments"].([]any) {
		m := c.(map[string]any)
		turns := m["turns"].([]any)
		if len(turns) != 1 {
			t.Fatalf("%v: one reply expected, got %v", m["text"], turns)
		}
		want := map[string]string{"first": "re first", "second": "re second"}[m["text"].(string)]
		if turns[0].(map[string]any)["text"] != want {
			t.Fatalf("%v got the reply %v", m["text"], turns[0])
		}
		sha := map[string]string{"first": "def5678", "second": "abc1234"}[m["text"].(string)]
		if m["sha"] != sha {
			t.Fatalf("%v has sha %v want %s", m["text"], m["sha"], sha)
		}
	}
}
