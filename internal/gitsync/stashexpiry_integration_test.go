//go:build integration && git

package gitsync

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stashRepo is a store repository in a temp directory holding the given refs, each a commit dated `age` ago.
func stashRepo(t *testing.T, refs map[string]time.Duration) (*Hub, string) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "github", "o", "r.git")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	git := func(date string, args ...string) string {
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date,
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("", "init", "-q", "--bare", ".")
	if err := os.WriteFile(filepath.Join(dir, Marker), []byte("made by test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	empty := git("", "mktree")
	for ref, age := range refs {
		date := time.Now().Add(-age).Format(time.RFC3339)
		sha := git(date, "commit-tree", "-m", ref, empty)
		git("", "update-ref", ref, sha)
	}
	return &Hub{Dir: root, StoreRoot: func() string { return root }, Runner: NewRunner()}, dir
}

func refsOf(t *testing.T, dir string) []string {
	t.Helper()
	c := exec.Command("git", "for-each-ref", "--format=%(refname)")
	c.Dir = dir
	out, err := c.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(string(out))
}

const day = 24 * time.Hour

// seedPushes is a push log with a settled push of each ref, `age` ago.
func seedPushes(t *testing.T, refs map[string]time.Duration) *MemPushLog {
	t.Helper()
	log := &MemPushLog{}
	for ref, age := range refs {
		if err := log.Append(context.Background(), PushRow{Repo: "github/o/r", Ref: ref, Card: "abc12345", Room: "r",
			At: time.Now().Add(-age)}); err != nil {
			t.Fatal(err)
		}
	}
	return log
}

// brokenBranches is a push log that cannot answer which branches it has rows for.
type brokenBranches struct{ *MemPushLog }

func (brokenBranches) Branches(context.Context, string) ([]BranchRecord, error) {
	return nil, errors.New("push log is down")
}

func TestAStashOlderThanTheRetentionIsGoneAndAYoungerOneStays(t *testing.T) {
	h, dir := stashRepo(t, map[string]time.Duration{
		"refs/heads/stash/abc12345/old":   40 * day,
		"refs/heads/stash/abc12345/young": 5 * day,
		"refs/heads/claude/main":          400 * day,
		"refs/heads/main":                 400 * day,
		"refs/tags/stash/abc12345/tagged": 400 * day,
	})
	h.PushLog = seedPushes(t, map[string]time.Duration{
		"refs/heads/stash/abc12345/old":   40 * day,
		"refs/heads/stash/abc12345/young": 5 * day,
	})
	var audited []string
	h.Audit = func(room, kind, detail string) { audited = append(audited, kind+" "+detail) }
	h.StashRetentionDays = func() int { return 30 }
	got, err := h.ExpireStashes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Ref != "refs/heads/stash/abc12345/old" || got[0].Card != "abc12345" || len(got[0].SHA) != 40 {
		t.Fatalf("expired = %+v", got)
	}
	left := strings.Join(refsOf(t, dir), " ")
	for _, want := range []string{"stash/abc12345/young", "claude/main", "refs/heads/main", "refs/tags/stash/abc12345/tagged"} {
		if !strings.Contains(left, want) {
			t.Errorf("%s was touched: %s", want, left)
		}
	}
	if strings.Contains(left, "/old") {
		t.Errorf("the old stash is still there: %s", left)
	}
	if len(audited) != 1 || !strings.Contains(audited[0], "git-stash-expired") || !strings.Contains(audited[0], got[0].SHA) ||
		!strings.Contains(audited[0], "card=abc12345") {
		t.Errorf("audit = %v", audited)
	}
}

func TestRetentionZeroKeepsEveryStash(t *testing.T) {
	h, dir := stashRepo(t, map[string]time.Duration{"refs/heads/stash/abc12345/ancient": 900 * day})
	h.StashRetentionDays = func() int { return 0 }
	got, err := h.ExpireStashes(context.Background())
	if err != nil || len(got) != 0 {
		t.Fatalf("%v %v", got, err)
	}
	if len(refsOf(t, dir)) != 1 {
		t.Errorf("refs = %v", refsOf(t, dir))
	}
}

func TestAStashPushedRecentlyIsNotExpiredByItsOldCommit(t *testing.T) {
	h, dir := stashRepo(t, map[string]time.Duration{"refs/heads/stash/abc12345/clean": 200 * day})
	log := &MemPushLog{}
	if err := log.Append(context.Background(), PushRow{Repo: "github/o/r", Ref: "refs/heads/stash/abc12345/clean",
		Card: "abc12345", Room: "r", At: time.Now().Add(-1 * day)}); err != nil {
		t.Fatal(err)
	}
	h.PushLog = log
	h.StashRetentionDays = func() int { return 30 }
	if got, err := h.ExpireStashes(context.Background()); err != nil || len(got) != 0 {
		t.Fatalf("%v %v", got, err)
	}
	if len(refsOf(t, dir)) != 1 {
		t.Errorf("refs = %v", refsOf(t, dir))
	}
}

func TestAStashWithNoPushRecordSurvivesAnOldCommitDate(t *testing.T) {
	h, dir := stashRepo(t, map[string]time.Duration{"refs/heads/stash/abc12345/unrecorded": 60 * day})
	h.PushLog = &MemPushLog{}
	h.StashRetentionDays = func() int { return 30 }
	if got, err := h.ExpireStashes(context.Background()); err != nil || len(got) != 0 {
		t.Fatalf("%v %v", got, err)
	}
	if len(refsOf(t, dir)) != 1 {
		t.Errorf("the stash with no push record was expired: %v", refsOf(t, dir))
	}
}

func TestAPushLogThatCannotBeReadExpiresNothing(t *testing.T) {
	h, dir := stashRepo(t, map[string]time.Duration{"refs/heads/stash/abc12345/old": 60 * day})
	h.PushLog = brokenBranches{seedPushes(t, map[string]time.Duration{"refs/heads/stash/abc12345/old": 60 * day})}
	h.StashRetentionDays = func() int { return 30 }
	got, err := h.ExpireStashes(context.Background())
	if err == nil || len(got) != 0 {
		t.Fatalf("want the push log's error and nothing expired, got %v %v", got, err)
	}
	if len(refsOf(t, dir)) != 1 {
		t.Errorf("a stash was expired while the push log was down: %v", refsOf(t, dir))
	}
}

func TestTheRetentionDefaultsToThirty(t *testing.T) {
	h := &Hub{}
	if h.StashRetention() != 30 {
		t.Errorf("default = %d", h.StashRetention())
	}
}
