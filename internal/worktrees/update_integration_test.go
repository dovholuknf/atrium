//go:build integration && git

package worktrees

import (
	"context"
	"testing"
)

func TestUpdatesFastForwardsCleanAndSkipsTheRest(t *testing.T) {
	f := newRepo(t)
	up := f.root + "/upstream"
	f.git(f.root, "init", "-q", "--bare", "-b", "claude/main", up)
	f.git(f.main, "remote", "add", "origin", up)
	f.git(f.main, "push", "-q", "origin", "claude/main")

	clean := f.worktree("clean")
	dirty := f.worktree("dirty")
	gone := f.worktree("gone")
	local := f.worktree("local")
	for _, b := range []string{"clean", "dirty", "gone"} {
		f.git(f.main, "push", "-q", "origin", "claude/"+b)
		f.git(f.main, "branch", "-q", "--set-upstream-to=origin/claude/"+b, "claude/"+b)
	}

	other := f.root + "/other"
	f.git(f.root, "clone", "-q", up, other)
	f.git(other, "checkout", "-q", "claude/clean")
	f.commit(other, "clean moves", map[string]string{"c.txt": "1\n"})
	f.git(other, "push", "-q", "origin", "claude/clean")
	f.git(other, "checkout", "-q", "claude/dirty")
	f.commit(other, "dirty moves", map[string]string{"d.txt": "1\n"})
	f.git(other, "push", "-q", "origin", "claude/dirty")
	f.git(other, "push", "-q", "origin", "--delete", "claude/gone")
	f.write(dirty, "a.txt", "changed\n")

	res, err := Updates(context.Background(), f.main, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Update{}
	for _, u := range res {
		got[u.Path] = u
	}
	if u := got[clean]; u.Done == "" || u.Skipped != "" {
		t.Fatalf("clean: %+v", u)
	}
	if f.git(clean, "log", "-1", "--format=%s") != "clean moves" {
		t.Fatal("clean worktree was not fast-forwarded")
	}
	if u := got[dirty]; u.Skipped != ReasonDirty {
		t.Fatalf("dirty: %+v", u)
	}
	if f.git(dirty, "log", "-1", "--format=%s") == "dirty moves" {
		t.Fatal("dirty worktree was touched")
	}
	if u := got[gone]; u.Skipped == "" {
		t.Fatalf("gone: %+v", u)
	}
	if u := got[local]; u.Skipped != "no upstream" {
		t.Fatalf("local: %+v", u)
	}
}
