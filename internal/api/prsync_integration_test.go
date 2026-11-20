//go:build integration

package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/base/forge"
	"github.com/dovholuknf/atrium/internal/base/rules"
	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/store"
)

// prSyncCard is a PR worktree of o/r#7 on feat/x, a card in it opened from the PR's link, and the card's id.
func prSyncCard(t *testing.T) (*prHarness, string, string) {
	t.Helper()
	if testing.Short() {
		t.Skip("slow: a real server with git worktrees and a database")
	}
	hn := newPRHarness(t, &fakeForge{pr: forge.PR{HeadRef: "feat/x"}})
	hn.checkout(t, "o", "r")
	code, out := hn.ask(t)
	if code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	wt := out["path"].(string)
	hn.srv.Recognise = func(url string) (*rules.Resolved, error) {
		return &rules.Resolved{URL: url, Host: "github.com", Tags: []string{rules.PRTag},
			Vars: map[string]string{"host": "github.com", "org": "o", "repo": "r", "num": "7"}}, nil
	}
	task, _, err := hn.srv.st.Register(store.Observed{WireName: "pr7", Worktree: wt, Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := hn.srv.st.SetOrigin(task.ID, "github", "", "https://github.com/o/r/pull/7"); err != nil {
		t.Fatal(err)
	}
	return hn, wt, task.ID
}

// movePR points refs/pull/7/head upstream at a new commit on top of parent, and answers it.
func movePR(t *testing.T, hn *prHarness, parent, msg string) string {
	t.Helper()
	sha := git(t, hn.up, "commit-tree", parent+"^{tree}", "-p", parent, "-m", msg)
	git(t, hn.up, "update-ref", "refs/pull/7/head", sha)
	return sha
}

func prSyncAsk(t *testing.T, hn *prHarness, card string) (int, gitsync.PRSyncResult, string) {
	t.Helper()
	rec := post(t, hn.h, "/v1/tasks/"+card+"/pr-sync", map[string]any{})
	var out gitsync.PRSyncResult
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out, rec.Body.String()
}

func TestPRSyncFastForwardsAndNamesTheNewCommits(t *testing.T) {
	t.Parallel()
	hn, wt, card := prSyncCard(t)
	code, out, _ := prSyncAsk(t, hn, card)
	if code != 200 || out.State != "current" {
		t.Fatalf("nothing new: %d %+v", code, out)
	}
	next := movePR(t, hn, "refs/pull/7/head", "second change")
	code, out, body := prSyncAsk(t, hn, card)
	if code != 200 || out.State != "fast-forward" || out.Branch != "feat/x" || out.New != next ||
		len(out.Commits) != 1 || !strings.HasSuffix(out.Commits[0], "second change") {
		t.Fatalf("%d %s", code, body)
	}
	if got := git(t, wt, "rev-parse", "HEAD"); got != next {
		t.Fatalf("the worktree is at %s, not %s", got, next)
	}
}

func TestPRSyncResetsAForcePushAndSaysSo(t *testing.T) {
	t.Parallel()
	hn, wt, card := prSyncCard(t)
	redo := movePR(t, hn, "main", "redone")
	code, out, body := prSyncAsk(t, hn, card)
	if code != 200 || out.State != "reset" || out.Dropped != 1 || len(out.Commits) != 1 ||
		!strings.Contains(out.Text, "force-pushed") {
		t.Fatalf("%d %s", code, body)
	}
	if got := git(t, wt, "rev-parse", "HEAD"); got != redo {
		t.Fatalf("the worktree is at %s, not %s", got, redo)
	}
}

func TestPRSyncRefusesLocalChangesAndLocalCommits(t *testing.T) {
	t.Parallel()
	hn, wt, card := prSyncCard(t)
	before := git(t, wt, "rev-parse", "HEAD")
	movePR(t, hn, "main", "redone")
	if err := os.WriteFile(filepath.Join(wt, "staged.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, wt, "add", "staged.txt")
	if code, _, body := prSyncAsk(t, hn, card); code != 409 || !strings.Contains(body, "local changes") {
		t.Fatalf("changes: %d %s", code, body)
	}
	if got := git(t, wt, "rev-parse", "HEAD"); got != before {
		t.Fatal("a refused sync moved the branch")
	}
	git(t, wt, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "mine")
	mine := git(t, wt, "rev-parse", "HEAD")
	if code, _, body := prSyncAsk(t, hn, card); code != 409 || !strings.Contains(body, "not in pull request 7") {
		t.Fatalf("commits: %d %s", code, body)
	}
	if got := git(t, wt, "rev-parse", "HEAD"); got != mine {
		t.Fatal("a refused sync moved the branch")
	}
}

func TestPRSyncRefusesACardNoPullRequestOpened(t *testing.T) {
	t.Parallel()
	hn, _, _ := prSyncCard(t)
	task, _, err := hn.srv.st.Register(store.Observed{WireName: "plain", Worktree: filepath.ToSlash(t.TempDir()), Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if code, _, body := prSyncAsk(t, hn, task.ID); code != 409 || !strings.Contains(body, "not opened from a pull request") {
		t.Fatalf("%d %s", code, body)
	}
}
