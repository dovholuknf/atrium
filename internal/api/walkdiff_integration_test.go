//go:build integration

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func walkDiffGet(t *testing.T, s *Server, id string) (int, walkDiffOut, string) {
	t.Helper()
	return walkDiffQuery(t, s, id, "")
}

func walkDiffQuery(t *testing.T, s *Server, id, query string) (int, walkDiffOut, string) {
	t.Helper()
	r := httptest.NewRequest("GET", "/v1/tasks/"+id+"/diff"+query, nil)
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

// A WORKTREE WITH NOTHING TO COMPARE WITH SAYS SO in words, and still answers, so its card keeps the walk button.
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

	code, out, body := walkDiffGet(t, s, task.ID)
	if code != http.StatusOK || out.Mode != "branch" || out.Diff != "" || !strings.Contains(out.Why, "no base to compare topic with") {
		t.Errorf("%d %s", code, body)
	}
}

// A WORKING CARD'S UNCOMMITTED EDITS ARE WALKED beside its commits, on a branch atrium did not name.
func TestWalkDiffCarriesUncommittedChanges(t *testing.T) {
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
	git(t, work, "switch", "-q", "-c", "secfix-v0.5.2")
	write("one.txt", "one\n")
	git(t, work, "add", ".")
	git(t, work, "commit", "-q", "-m", "one")
	write("base.txt", "base\nedited\n")
	task := cardIn(t, st, work)

	code, out, body := walkDiffQuery(t, s, task.ID, "?mode=branch")
	if code != http.StatusOK {
		t.Fatalf("%d %s", code, body)
	}
	for _, f := range []string{"b/one.txt", "b/base.txt", "+edited"} {
		if !strings.Contains(out.Diff, f) {
			t.Errorf("the patch has no %s:\n%s", f, out.Diff)
		}
	}
}

// A CARD WHOSE BRANCH IS ALREADY IN ITS BASE AND WHOSE WORK IS ONLY NEW FILES still has a walk: docprev on claude-sg4.
func TestWalkDiffCarriesUntrackedFiles(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	git(t, work, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(work, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, work, "add", ".")
	git(t, work, "commit", "-q", "-m", "base")
	git(t, work, "switch", "-q", "-c", "dashboard-fixes")
	if err := os.MkdirAll(filepath.Join(work, "ziti-qs"), 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, body := range map[string]string{"dashboard-head.html": "<p>head</p>\n", "ziti-qs/run.sh": "echo qs\n",
		".gitignore": "ignored.log\n", "ignored.log": "noise\n"} {
		if err := os.WriteFile(filepath.Join(work, filepath.FromSlash(rel)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git(t, work, "switch", "-q", "main")
	git(t, work, "commit", "-q", "--allow-empty", "-m", "main moved on")
	git(t, work, "switch", "-q", "dashboard-fixes")
	task := cardIn(t, st, work)

	code, out, body := walkDiffGet(t, s, task.ID)
	if code != http.StatusOK {
		t.Fatalf("%d %s", code, body)
	}
	for _, f := range []string{"b/dashboard-head.html", "+<p>head</p>", "b/ziti-qs/run.sh", "+echo qs"} {
		if !strings.Contains(out.Diff, f) {
			t.Errorf("the patch has no %s:\n%s", f, out.Diff)
		}
	}
	if strings.Contains(out.Diff, "b/ignored.log") {
		t.Errorf("an ignored file was walked:\n%s", out.Diff)
	}
}

func walkRepo(t *testing.T, work, branch string) func(rel, body string) {
	t.Helper()
	write := func(rel, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(work, filepath.FromSlash(rel)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git(t, work, "init", "-q", "-b", "main")
	write("base.txt", "base\n")
	git(t, work, "add", ".")
	git(t, work, "commit", "-q", "-m", "base")
	git(t, work, "switch", "-q", "-c", branch)
	write("one.txt", "one\n")
	git(t, work, "add", ".")
	git(t, work, "commit", "-q", "-m", "one")
	return write
}

// THE WALK OPENS ON UNSTAGED, ELSE STAGED, ELSE BRANCH, and each mode compares only its own side, with every count.
func TestWalkDiffModesOpenOnTheNearestChange(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	write := walkRepo(t, work, "feature")
	task := cardIn(t, st, work)

	if code, out, body := walkDiffGet(t, s, task.ID); code != http.StatusOK || out.Mode != "branch" ||
		!strings.Contains(out.Diff, "b/one.txt") || out.Modes["unstaged"] != 0 || out.Modes["staged"] != 0 || out.Modes["branch"] != 1 {
		t.Errorf("a clean tree: %d %s", code, body)
	}
	write("staged.txt", "staged\n")
	git(t, work, "add", "staged.txt")
	if code, out, body := walkDiffGet(t, s, task.ID); code != http.StatusOK || out.Mode != "staged" ||
		!strings.Contains(out.Diff, "b/staged.txt") || strings.Contains(out.Diff, "b/one.txt") || out.Modes["staged"] != 1 {
		t.Errorf("only staged: %d %s", code, body)
	}
	write("base.txt", "base\nedited\n")
	write("new.txt", "new\n")
	code, out, body := walkDiffGet(t, s, task.ID)
	if code != http.StatusOK || out.Mode != "unstaged" || out.Modes["unstaged"] != 2 || out.Modes["staged"] != 1 || out.Modes["branch"] != 3 {
		t.Fatalf("unstaged and staged: %d %s", code, body)
	}
	for _, f := range []string{"b/base.txt", "+edited", "b/new.txt"} {
		if !strings.Contains(out.Diff, f) {
			t.Errorf("unstaged has no %s:\n%s", f, out.Diff)
		}
	}
	if strings.Contains(out.Diff, "b/staged.txt") || strings.Contains(out.Diff, "b/one.txt") {
		t.Errorf("unstaged carries staged or committed work:\n%s", out.Diff)
	}
	if code, out, body := walkDiffQuery(t, s, task.ID, "?mode=staged"); code != http.StatusOK || out.Mode != "staged" ||
		!strings.Contains(out.Diff, "b/staged.txt") || strings.Contains(out.Diff, "b/new.txt") {
		t.Errorf("asked for staged: %d %s", code, body)
	}
	if code, _, body := walkDiffQuery(t, s, task.ID, "?mode=all"); code != http.StatusBadRequest {
		t.Errorf("an unknown mode: %d %s", code, body)
	}
}

// A MODE WITH NOTHING IN IT ANSWERS 200 WITH WHY, so a git card with no change still has its walk.
func TestWalkDiffWithNothingChangedSaysWhy(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	walkRepo(t, work, "feature")
	task := cardIn(t, st, work)

	code, out, body := walkDiffQuery(t, s, task.ID, "?mode=unstaged")
	if code != http.StatusOK || out.Diff != "" || out.Why != "nothing unstaged in feature" {
		t.Errorf("%d %s", code, body)
	}
}

// A FOLDER THAT IS NOT IN GIT HAS NO WALK.
func TestWalkDiffOfAFolderNotInGitRefuses(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)

	code, _, body := walkDiffGet(t, s, task.ID)
	if code != http.StatusConflict || !strings.Contains(body, "is not a git checkout") {
		t.Errorf("%d %s", code, body)
	}
}

// A CARD WHOSE FOLDER IS NOT GIT BUT HOLDS ONE REPOSITORY walks that repository: docprev showed no walk.
func TestWalkDiffOfAFolderHoldingOneRepositoryWalksIt(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	inner := filepath.Join(work, "docprev")
	if err := os.Mkdir(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	walkRepo(t, inner, "feature")("new.txt", "new\n")
	task := cardIn(t, st, work)

	code, out, body := walkDiffGet(t, s, task.ID)
	if code != http.StatusOK || out.Branch != "feature" || !strings.Contains(out.Diff, "b/new.txt") {
		t.Errorf("%d %s", code, body)
	}
}

// AN AGENT'S LINK WALKS THE WORKTREE IT HANDED OFF, another card's, and never a folder outside atrium's paths.
func TestWalkDiffOfAHandedOffWorktree(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	walkRepo(t, work, "feature")
	task := cardIn(t, st, work)
	_, _, other := fileServer(t)
	write := walkRepo(t, other, "pr-tests")
	write("test.sh", "echo test\n")
	if _, _, err := st.Register(store.Observed{WireName: "handed", Worktree: filepath.ToSlash(other), Runner: "claude"}); err != nil {
		t.Fatal(err)
	}

	code, out, body := walkDiffQuery(t, s, task.ID, "?dir="+url.QueryEscape(filepath.ToSlash(other)))
	if code != http.StatusOK || out.Mode != "unstaged" || out.Branch != "pr-tests" || !strings.Contains(out.Diff, "b/test.sh") {
		t.Errorf("the handed-off worktree: %d %s", code, body)
	}
	outside := t.TempDir()
	walkRepo(t, outside, "elsewhere")
	if code, _, body := walkDiffQuery(t, s, task.ID, "?dir="+url.QueryEscape(filepath.ToSlash(outside))); code != http.StatusForbidden {
		t.Errorf("a folder outside atrium's paths: %d %s", code, body)
	}
}

// A FRESH BRANCH OFF CLAUDE/MAIN IS WALKED AGAINST CLAUDE/MAIN, not against a main far behind it: walk-interview's
// "walk branch: 1814 files" of nothing.
func TestWalkDiffOfAFreshBranchUsesTheNearestBase(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	write := walkRepo(t, work, "claude/main")
	write("two.txt", "two\n")
	git(t, work, "add", ".")
	git(t, work, "commit", "-q", "-m", "two")
	git(t, work, "switch", "-q", "-c", "claude/walk-interview")
	task := cardIn(t, st, work)

	code, out, body := walkDiffGet(t, s, task.ID)
	if code != http.StatusOK || out.Base != "claude/main" || out.Modes["branch"] != 0 || out.Diff != "" {
		t.Errorf("%d %s", code, body)
	}
}

// A BRANCH OFF CLAUDE/MAIN IS WALKED FROM ITS MERGE BASE WITH CLAUDE/MAIN even when main is fewer commits behind it.
func TestWalkDiffPrefersClaudeMainOverANearerMain(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	write := walkRepo(t, work, "claude/main")
	git(t, work, "switch", "-q", "-c", "claude/walk-fix")
	write("fix.txt", "fix\n")
	git(t, work, "add", ".")
	git(t, work, "commit", "-q", "-m", "fix")
	git(t, work, "branch", "-f", "main", "HEAD")
	write("more.txt", "more\n")
	git(t, work, "add", ".")
	git(t, work, "commit", "-q", "-m", "more")
	task := cardIn(t, st, work)

	code, out, body := walkDiffQuery(t, s, task.ID, "?mode=branch")
	if code != http.StatusOK || out.Base != "claude/main" || out.Modes["branch"] != 2 ||
		!strings.Contains(out.Diff, "b/fix.txt") || !strings.Contains(out.Diff, "b/more.txt") {
		t.Errorf("%d %s", code, body)
	}
}

// A CARD ON CLAUDE/MAIN ITSELF HAS NO BRANCH FILES, so its button counts the unstaged and staged: the orch card on
// sg4-control showed "walk branch: 396 files" for one dirty package.json.
func TestWalkDiffOnClaudeMainItselfHasNoBranchFiles(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	write := walkRepo(t, work, "claude/main")
	write("base.txt", "base\ndirty\n")
	write("new.txt", "new\n")
	write("staged.txt", "staged\n")
	git(t, work, "add", "staged.txt")
	task := cardIn(t, st, work)

	code, out, body := walkDiffGet(t, s, task.ID)
	if code != http.StatusOK || out.Base != "claude/main" || out.Mode != "unstaged" || out.Modes["branch"] != 0 ||
		out.Modes["unstaged"] != 2 || out.Modes["staged"] != 1 {
		t.Errorf("%d %s", code, body)
	}
	if code, out, body := walkDiffQuery(t, s, task.ID, "?mode=branch"); code != http.StatusOK || out.Diff != "" ||
		!strings.Contains(out.Why, "nothing to walk") {
		t.Errorf("asked for branch: %d %s", code, body)
	}
}

// A MOCK COPY OF THE BOARD IS NOT THE BRANCH: copy-clean-fixes counted "walk branch: 1844 files" for a small fix.
func TestWalkDiffBranchLeavesOutAnUntrackedMock(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	write := walkRepo(t, work, "claude/main")
	git(t, work, "switch", "-q", "-c", "claude/copy-clean-fixes")
	write("fix.txt", "fix\n")
	git(t, work, "add", ".")
	git(t, work, "commit", "-q", "-m", "fix")
	if err := os.MkdirAll(filepath.Join(work, "mock", "js"), 0o755); err != nil {
		t.Fatal(err)
	}
	write("mock/index.html", "<p>mock</p>\n")
	write("mock/js/walk.js", "mock\n")
	task := cardIn(t, st, work)

	code, out, body := walkDiffQuery(t, s, task.ID, "?mode=branch")
	if code != http.StatusOK || out.Modes["branch"] != 1 || out.Modes["unstaged"] != 2 ||
		!strings.Contains(out.Diff, "b/fix.txt") || strings.Contains(out.Diff, "mock/") {
		t.Errorf("%d %s", code, body)
	}
}
