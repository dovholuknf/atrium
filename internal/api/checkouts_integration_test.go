//go:build integration

package api

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Kept out of the unit run: it walks real directories and goes over the 20ms unit budget on a loaded disk.
func TestBuildCheckoutsResolvesCardDirsToRepoAndWorktree(t *testing.T) {
	repo, wt := fakeRepo(t)
	sub := filepath.Join(repo, "cmd", "x")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	got := buildCheckouts([]dirUse{
		{wt, now},
		{sub, now.Add(-time.Hour)},
		{repo, now.Add(-2 * time.Hour)},
		{filepath.Join(t.TempDir(), "gone"), now},
		{t.TempDir(), now},
		{"", now},
	})
	if len(got) != 2 {
		t.Fatalf("want a worktree and a checkout, got %+v", got)
	}
	if !got[0].Linked || got[0].Branch != "fix/it" || got[0].Path != filepath.ToSlash(wt) {
		t.Errorf("worktree first, newest use: %+v", got[0])
	}
	if got[0].Root != filepath.ToSlash(repo) || got[0].Repo != "ziti" || got[0].Slug != "github/openziti/ziti" {
		t.Errorf("the worktree names its repo root and slug: %+v", got[0])
	}
	if got[1].Linked || got[1].Branch != "main" || got[1].Path != filepath.ToSlash(repo) {
		t.Errorf("the checkout, deduped with its subdirectory: %+v", got[1])
	}
}
