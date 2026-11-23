//go:build integration && git

package gitsync

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// hygieneRepo is stashRepo with claude/main holding a file, a branch with the same change as a squash (a different
// commit), a branch with a change main lacks, and a stash copy of each.
func hygieneRepo(t *testing.T) (*Hub, string) {
	t.Helper()
	h, dir := stashRepo(t, nil)
	git := func(args ...string) string {
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(), "GIT_AUTHOR_DATE=2020-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2020-01-01T00:00:00Z",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	commit := func(parent, file, text string) string {
		tmp := filepath.Join(t.TempDir(), "f")
		if err := os.WriteFile(tmp, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		b := git("hash-object", "-w", tmp)
		c := exec.Command("git", "mktree")
		c.Dir = dir
		c.Stdin = strings.NewReader("100644 blob " + b + "\t" + file + "\n")
		out, err := c.Output()
		if err != nil {
			t.Fatal(err)
		}
		args := []string{"commit-tree", "-m", file, strings.TrimSpace(string(out))}
		if parent != "" {
			args = append(args, "-p", parent)
		}
		return git(args...)
	}
	base := commit("", "a", "base\n")
	landed := commit(base, "b", "landed\n")
	git("update-ref", "refs/heads/claude/main", landed)
	squash := commit(base, "b", "landed\n")
	squash = git("commit-tree", "-m", "squashed", squash+"^{tree}", "-p", base)
	unique := commit(landed, "c", "only here\n")
	git("update-ref", "refs/heads/claude/sq", squash)
	git("update-ref", "refs/heads/claude/uniq", unique)
	git("update-ref", "refs/heads/stash/abc12345/claude/sq", squash)
	git("update-ref", "refs/heads/stash/abc12345/claude/uniq", unique)
	return h, dir
}

func TestDeleteLandedBundlesTheBranchAndDropsItsStashCopy(t *testing.T) {
	h, dir := hygieneRepo(t)
	st := h.Store()
	sq := strings.TrimSpace(gitOut(t, dir, "rev-parse", "refs/heads/claude/sq"))
	if ok, err := st.DeleteLanded(context.Background(), "github/o/r", "claude/sq", sq); err != nil || !ok {
		t.Fatalf("deleted = %v, %v", ok, err)
	}
	left := strings.Join(refsOf(t, dir), " ")
	if strings.Contains(left, "claude/sq") {
		t.Errorf("the branch or its stash copy is still there: %s", left)
	}
	if !strings.Contains(left, "claude/uniq") || !strings.Contains(left, "stash/abc12345/claude/uniq") {
		t.Errorf("an unlanded branch was touched: %s", left)
	}
	files, _ := filepath.Glob(filepath.Join(h.BundleDir("github/o/r"), "*.bundle"))
	if len(files) < 1 {
		t.Fatalf("no bundle in %s", h.BundleDir("github/o/r"))
	}
	if out := gitOut(t, dir, "bundle", "verify", files[0]); !strings.Contains(out, "okay") {
		t.Errorf("bundle verify: %s", out)
	}
}

func TestSweepLandedDeletesByContentAndListsTheRest(t *testing.T) {
	h, dir := hygieneRepo(t)
	res, err := h.Store().SweepLanded(context.Background(), "github/o/r")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Deleted) != 2 || len(res.Unlanded) != 2 {
		t.Fatalf("result = %+v", res)
	}
	left := strings.Join(refsOf(t, dir), " ")
	for _, want := range []string{"refs/heads/claude/main", "claude/uniq", "stash/abc12345/claude/uniq"} {
		if !strings.Contains(left, want) {
			t.Errorf("%s was deleted: %s", want, left)
		}
	}
	if strings.Contains(left, "claude/sq") {
		t.Errorf("a landed branch is still there: %s", left)
	}
}

func TestSweepSkipsABranchPushedJustNow(t *testing.T) {
	h, dir := hygieneRepo(t)
	h.PushLog = seedPushes(t, map[string]time.Duration{"refs/heads/claude/sq": time.Hour})
	res, err := h.Store().SweepLanded(context.Background(), "github/o/r")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(refsOf(t, dir), " "), "refs/heads/claude/sq") || len(res.Deleted) != 1 {
		t.Errorf("result = %+v, refs = %v", res, refsOf(t, dir))
	}
}

func TestABundleOlderThanTheRetentionIsRemoved(t *testing.T) {
	h, _ := hygieneRepo(t)
	h.BundleRetentionDays = func() int { return 30 }
	bd := h.BundleDir("github/o/r")
	if err := os.MkdirAll(bd, 0o755); err != nil {
		t.Fatal(err)
	}
	old, young := filepath.Join(bd, "old.bundle"), filepath.Join(bd, "young.bundle")
	for _, p := range []string{old, young} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(old, time.Now(), time.Now().Add(-40*day)); err != nil {
		t.Fatal(err)
	}
	if err := h.ExpireBundles(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); err == nil {
		t.Error("the old bundle is still there")
	}
	if _, err := os.Stat(young); err != nil {
		t.Error("the young bundle was removed")
	}
	h.BundleRetentionDays = func() int { return 0 }
	if err := os.Chtimes(young, time.Now(), time.Now().Add(-400*day)); err != nil {
		t.Fatal(err)
	}
	_ = h.ExpireBundles()
	if _, err := os.Stat(young); err != nil {
		t.Error("retention 0 removed a bundle")
	}
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}
