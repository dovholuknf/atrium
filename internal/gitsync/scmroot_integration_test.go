//go:build integration

package gitsync

import (
	"os"
	"path/filepath"
	"testing"
)

// AN UNSET git.scm_root IS ~/git WHEN THAT FOLDER EXISTS, derived on each read and never written.
func TestEffectiveSCMRootDefaultsToTheHomeGitFolder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	if got := EffectiveSCMRoot(""); got != "" {
		t.Fatalf("no ~/git and no setting = %q, want none", got)
	}
	if err := os.Mkdir(filepath.Join(home, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, want := EffectiveSCMRoot("  "), filepath.Join(home, "git"); got != want {
		t.Fatalf("unset = %q, want %q", got, want)
	}
	if got := EffectiveSCMRoot(" D:/elsewhere "); got != "D:/elsewhere" {
		t.Fatalf("a setting wins, got %q", got)
	}
}

// A ROOM WITH NEITHER ANSWERS ErrNoSCMRoot, AND ONE WITH THE DEFAULT CLONES UNDER IT.
func TestSCMClonePathUsesTheDefaultRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	c := &SCM{Root: func() string { return EffectiveSCMRoot("") }}
	ref := Ref{Host: "github", Owner: "o", Repo: "r"}
	if _, _, err := c.ClonePath(ref); err != ErrNoSCMRoot {
		t.Fatalf("no ~/git = %v, want ErrNoSCMRoot", err)
	}
	if err := os.Mkdir(filepath.Join(home, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	root, dest, err := c.ClonePath(ref)
	real, _ := filepath.EvalSymlinks(home)
	if err != nil || root != filepath.Join(home, "git") || dest != filepath.Join(real, "git", "github", "o", "r") {
		t.Fatalf("with ~/git = %q %q %v", root, dest, err)
	}
}
