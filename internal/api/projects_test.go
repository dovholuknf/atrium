package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mkRepo makes a directory that looks like a checkout to the scan.
func mkRepo(t *testing.T, parts ...string) string {
	t.Helper()
	dir := filepath.Join(parts...)
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// The scan finds a checkout at the convention's depth, and does not walk into
// one once it has. A repository's own subdirectories are its source, and a
// scan that descends turns one repository into a page of them.
func TestScanProjectsStopsAtACheckout(t *testing.T) {
	root := t.TempDir()
	mkRepo(t, root, "orgA", "one")
	mkRepo(t, root, "orgA", "two")
	// A repository with a repository inside it, which is a vendored checkout
	// or a worktree kept in-tree. The outer one is the answer.
	inner := mkRepo(t, root, "orgB", "outer")
	if err := os.MkdirAll(filepath.Join(inner, "vendor", "dep", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Not a checkout, and deeper than the depth allows.
	if err := os.MkdirAll(filepath.Join(root, "orgC", "notes", "deep", "deeper"), 0o755); err != nil {
		t.Fatal(err)
	}

	found, truncated := scanProjects([]string{root}, 2)
	if truncated {
		t.Fatalf("a tree this small should not have been cut short")
	}
	var names []string
	for _, p := range found {
		names = append(names, p.Group+"/"+p.Name)
	}
	want := []string{"orgA/one", "orgA/two", "orgB/outer"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("found %v, wanted %v", names, want)
	}
}

// A root that IS the checkout is found at depth zero. The picker's roots
// include every directory a card names, and those are worktrees.
func TestScanProjectsFindsARootThatIsARepo(t *testing.T) {
	root := t.TempDir()
	mkRepo(t, root)
	found, _ := scanProjects([]string{root}, 2)
	if len(found) != 1 || found[0].Path != filepath.ToSlash(root) {
		t.Fatalf("a root holding a .git should be the one answer, got %v", found)
	}
}

// The same checkout reached through two roots is one row. The default root set
// overlaps by design: home, plus every card's directory, which is usually
// under home.
func TestScanProjectsDeduplicates(t *testing.T) {
	root := t.TempDir()
	repo := mkRepo(t, root, "org", "repo")
	found, _ := scanProjects([]string{root, repo}, 2)
	if len(found) != 1 {
		t.Fatalf("wanted one row, got %d", len(found))
	}
}

// The porcelain form, and the two things read out of it: the repository itself
// is not one of its own worktrees, and a detached one still gets a label.
func TestParseWorktreeList(t *testing.T) {
	repo := filepath.FromSlash("D:/git/github/org/repo")
	out := strings.Join([]string{
		"worktree D:/git/github/org/repo",
		"HEAD 1111111111111111111111111111111111111111",
		"branch refs/heads/main",
		"",
		"worktree D:/worktrees/github/org/repo/feature-x",
		"HEAD 2222222222222222222222222222222222222222",
		"branch refs/heads/feature-x",
		"",
		"worktree D:/worktrees/github/org/repo/loose",
		"HEAD 3333333333333333333333333333333333333333",
		"detached",
		"",
	}, "\n")

	got := parseWorktreeList(out, filepath.ToSlash(repo))
	if len(got) != 2 {
		t.Fatalf("wanted two worktrees beside the checkout, got %v", got)
	}
	if got[0].Branch != "feature-x" {
		t.Errorf("branch came out as %q", got[0].Branch)
	}
	if got[1].Branch != "loose" {
		t.Errorf("a detached worktree should be labelled by its directory, got %q", got[1].Branch)
	}
}

// The fence on the one value that reaches a shell. Everything a shell could do
// something with is outside the set, and so is a leading dash, which is an
// option rather than a branch.
func TestLegalBranch(t *testing.T) {
	for _, ok := range []string{"main", "b3-25-git-projects", "feature/x.y", "v1_2"} {
		if !legalBranch.MatchString(ok) {
			t.Errorf("%q should be a legal branch name", ok)
		}
	}
	for _, bad := range []string{
		"", "-y", "a b", "a;shutdown", "a&b", "a|b", "a$(id)", "a`id`", "a'b", `a"b`,
		"a\nb", "a>b", "a\\b", "a%b%",
	} {
		if legalBranch.MatchString(bad) {
			t.Errorf("%q must not be a legal branch name", bad)
		}
	}
}

// Empty is the default and `off` is nothing, which is the difference a store
// that cannot tell "never set" from "set to nothing" forces into the values.
func TestWorktreeTemplate(t *testing.T) {
	if got := worktreeTemplate(""); got != DefaultWorktreeCommand {
		t.Errorf("empty should mean the default, got %q", got)
	}
	if got := worktreeTemplate("  "); got != DefaultWorktreeCommand {
		t.Errorf("blank should mean the default, got %q", got)
	}
	if got := worktreeTemplate("off"); got != "" {
		t.Errorf("`off` should mean no command, got %q", got)
	}
	if got := worktreeTemplate(" gwt twig {branch} -y "); got != "gwt twig {branch} -y" {
		t.Errorf("anything else is stored as typed, got %q", got)
	}
}

// The shell hosts the line, and the flag differs per shell. Getting this wrong
// means the template arrives as the shell's own first argument and nothing
// says so.
func TestShellCommandFlags(t *testing.T) {
	for shell, want := range map[string]string{
		`C:\Program Files\PowerShell\7\pwsh.exe`: "-Command",
		`powershell.exe`:                         "-Command",
		`C:\Windows\System32\cmd.exe`:            "/c",

		// Both separators, because the answer comes from the path and not
		// from the machine reading it. Splitting with the host's separator
		// means a Windows path read on Linux matches nothing and cmd is
		// handed `-c`, which is how this test failed on the Linux runner
		// while passing on Windows.
		"/usr/local/bin/pwsh": "-Command",
	} {
		_, args := shellArgsFor(shell, "gwt new x -y")
		if len(args) == 0 || args[len(args)-2] != want {
			t.Errorf("%s should host a line with %s, got %v", shell, want, args)
		}
		if args[len(args)-1] != "gwt new x -y" {
			t.Errorf("%s did not get the line last, got %v", shell, args)
		}
	}
}
