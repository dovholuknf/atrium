package cli

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestLandCheck(t *testing.T) {
	root := t.TempDir()
	sh := func(dir string, args ...string) {
		t.Helper()
		c := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
		c.Dir = dir
		if b, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, b)
		}
	}
	write := func(dir, name, body string) {
		if err := os.WriteFile(dir+"/"+name, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	hub, work := root+"/hub", root+"/work"
	sh(root, "init", "--bare", "-b", "claude/main", hub)
	sh(root, "clone", hub, work)
	sh(work, "checkout", "-b", "claude/main")
	write(work, "a.txt", "one\n")
	sh(work, "add", ".")
	sh(work, "commit", "-m", "base")
	sh(work, "remote", "rename", "origin", "hub")
	sh(work, "push", "hub", "claude/main")
	sh(work, "checkout", "-b", "clean")
	write(work, "b.txt", "b\n")
	sh(work, "add", ".")
	sh(work, "commit", "-m", "add b")
	sh(work, "push", "hub", "clean")
	sh(work, "checkout", "claude/main")
	sh(work, "checkout", "-b", "bad")
	write(work, "a.txt", "two\n")
	sh(work, "commit", "-am", "bad side")
	sh(work, "push", "hub", "bad")
	sh(work, "checkout", "claude/main")
	write(work, "a.txt", "three\n")
	sh(work, "commit", "-am", "main side")
	sh(work, "push", "hub", "claude/main")

	t.Chdir(work)
	cases := []struct {
		branch string
		code   int
		want   string
	}{{"clean", 0, "conflict: no"}, {"bad", landConflict, "  a.txt"}, {"nope", landNotFound, "not found"}}
	for _, tc := range cases {
		var out bytes.Buffer
		err := landCheck(&out, "hub", "claude/main", tc.branch)
		code := 0
		if ec, ok := err.(exitCodeError); ok {
			code = ec.Code
		} else if err != nil {
			t.Fatalf("%s: %v", tc.branch, err)
		}
		if code != tc.code || !strings.Contains(out.String(), tc.want) {
			t.Errorf("%s: code %d, output %q", tc.branch, code, out.String())
		}
	}
}
