//go:build integration

package gitsync

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestGitCappedStopsGitAtTheCap(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("init")
	if err := os.WriteFile(dir+"/f.txt", []byte(strings.Repeat("0123456789\n", 50000)), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "x")
	r := NewRunner()
	out, err := r.GitCapped(context.Background(), dir, nil, 1000, "show", "--format=", "HEAD")
	if !errors.Is(err, ErrOutputCap) || len(out) != 1000 {
		t.Fatalf("got %d bytes, err %v", len(out), err)
	}
	// Under the cap, or exactly at it, nothing is cut.
	full, err := r.GitCapped(context.Background(), dir, nil, 0, "show", "--format=", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := r.GitCapped(context.Background(), dir, nil, len(full), "show", "--format=", "HEAD"); err != nil || got != full {
		t.Fatalf("exactly at the cap: %d of %d bytes, err %v", len(got), len(full), err)
	}
}
