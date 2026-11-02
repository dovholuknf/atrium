package gitsync

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestTheStorePathNeverLeavesTheRoot(t *testing.T) {
	root := t.TempDir()
	s := &Store{h: &Hub{StoreRoot: func() string { return root }}}
	for _, ref := range []Ref{{"..", "x", "y"}, {"github", "..", "y"}, {"github", "x", "../../y"}, {"", "", ""}} {
		d := s.dir(ref)
		if d != filepath.Join(root, "refused.git") {
			t.Errorf("%+v gave %q", ref, d)
		}
	}
	good := s.dir(Ref{"github", "o", "r"})
	if !strings.HasPrefix(good, root) || !strings.HasSuffix(filepath.ToSlash(good), "github/o/r.git") {
		t.Errorf("a plain ref gave %q", good)
	}
}

func TestABranchOrStorePathNeverReadsAsAnOption(t *testing.T) {
	s := &Store{h: &Hub{}}
	for _, bad := range []string{"--output=x", "-x", "a..b", "", "a b"} {
		if _, _, err := s.tipOf(context.Background(), "", bad); err == nil {
			t.Errorf("tipOf took %q", bad)
		}
	}
	if err := s.create(context.Background(), "-x/p.git"); err == nil {
		t.Error("create took an option as its path")
	}
}

func TestGitRefusesAnArgumentHoldingANul(t *testing.T) {
	r := &Runner{root: context.Background()}
	if _, err := r.Git(context.Background(), "", "rev-parse", "a\x00b"); err == nil || r.Spawned.Load() != 0 {
		t.Errorf("a NUL argument was run: %v", err)
	}
}

func TestARevisionNeverReadsAsAnOption(t *testing.T) {
	s := &Store{h: &Hub{}}
	for _, bad := range []string{"--output=x", "-x", "a..b", "a\nb", "", "a b"} {
		if _, err := s.commitHeld(context.Background(), "", bad); err == nil {
			t.Errorf("commitHeld took %q", bad)
		}
		if _, err := s.isAncestor(context.Background(), "", bad, "claude/main"); err == nil {
			t.Errorf("isAncestor took %q", bad)
		}
	}
	if !safeRevArg("refs/heads/claude/main") || !safeRevArg(strings.Repeat("a", 40)) {
		t.Error("a plain ref or sha was refused")
	}
}
