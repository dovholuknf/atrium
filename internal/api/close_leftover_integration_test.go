//go:build integration

package api

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// deepTree makes root/node_modules/... with a file at a path past 300 characters, and answers the file's size.
func deepTree(t *testing.T, root string) int64 {
	t.Helper()
	dir := filepath.Join(root, "node_modules")
	for len(dir) < 330 {
		dir = filepath.Join(dir, strings.Repeat("d", 40))
	}
	if err := os.MkdirAll(longPath(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte(strings.Repeat("x", 100_000))
	if err := os.WriteFile(longPath(filepath.Join(dir, "big.js")), body, 0o644); err != nil {
		t.Fatal(err)
	}
	return int64(len(body))
}

func TestCloseRemovesAnIgnoredDeepTreeAndCountsItsBytes(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: a real server with git worktrees and a database")
	}
	t.Parallel()
	oh, card, wt, _ := openForClose(t)
	// node_modules is ignored, so it does not make the worktree ask.
	exclude := git(t, wt, "rev-parse", "--path-format=absolute", "--git-path", "info/exclude")
	if err := os.MkdirAll(filepath.Dir(exclude), 0o755); err != nil {
		t.Fatal(err)
	}
	// Added to what is there: the open already excluded .atrium/, and a rewrite would make its PR.md a dirty file.
	have, _ := os.ReadFile(exclude)
	if err := os.WriteFile(exclude, append(have, []byte("\nnode_modules/\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	size := deepTree(t, filepath.FromSlash(wt))

	code, out := closeCall(t, oh, card, map[string]any{"confirm": true})
	if code != 200 || !out.Closed || out.Left != 0 {
		t.Fatalf("%d %+v", code, out)
	}
	if exists(wt) {
		t.Fatal("the folder is still on disk, and the close called it freed")
	}
	for _, it := range out.Items {
		if it.Kind == store.ResWorktree && it.Bytes < size {
			t.Errorf("the worktree row says %d bytes, the ignored tree alone is %d", it.Bytes, size)
		}
	}
}

func TestRemoveLeftoverRemovesWhatGitLeftAndNamesWhatItCannot(t *testing.T) {
	t.Parallel()
	_, _, wt, _ := openForClose(t)
	repo := git(t, filepath.FromSlash(wt), "rev-parse", "--path-format=absolute", "--git-common-dir")
	repo = filepath.Dir(repo)

	// A folder git does not list, holding a tree too deep for the old limit: gone, with nothing said.
	gone := filepath.Join(t.TempDir(), "a", "b", "wt")
	deepTree(t, gone)
	if err := removeLeftover(repo, gone); err != nil || exists(gone) {
		t.Fatalf("not swept: %v", err)
	}

	// A path git still lists is not deleted, and the row is told why.
	err := removeLeftover(repo, wt)
	if err == nil || !strings.Contains(err.Error(), "still a worktree") || !exists(wt) {
		t.Fatalf("a listed worktree was swept or called freed: %v", err)
	}

	// A path that is not there is already freed.
	if err := removeLeftover(repo, filepath.Join(t.TempDir(), "a", "b", "nope")); err != nil {
		t.Errorf("a path that is not there: %v", err)
	}
}

func TestCloseMarksTheCardClosed(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: a real server with git worktrees and a database")
	}
	t.Parallel()
	oh, card, _, _ := openForClose(t)
	if code, out := closeCall(t, oh, card, map[string]any{"confirm": true}); code != 200 || !out.Closed {
		t.Fatalf("%d %+v", code, out)
	}
	task, _ := oh.srv.st.Get(card)
	if !slices.Contains(task.Tags, ClosedTag) {
		t.Errorf("the card does not carry %s: %v", ClosedTag, task.Tags)
	}
}
