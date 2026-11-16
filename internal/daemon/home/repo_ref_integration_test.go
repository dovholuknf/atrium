//go:build integration

package home

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/store"
)

// EvalSymlinks fails with access denied where a parent folder cannot be queried, so the cleaned path stands in for it.
func real(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

func TestResolveRepoMapsTheBareGithubHostToTheClone(t *testing.T) {
	t.Parallel()
	st, err := store.Open(filepath.Join(t.TempDir(), "atrium.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	root := t.TempDir()
	if err := st.SetSetting(gitsync.SettingSCMRoot, filepath.ToSlash(root)); err != nil {
		t.Fatal(err)
	}
	clone := filepath.Join(root, "github", "o", "r")
	if err := os.MkdirAll(filepath.Join(clone, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	d := &Service{St: st, Calls: Calls{OptAgentAddr: func() string { return "" }}}
	for _, repo := range []string{"github/o/r", "github.com/o/r"} {
		got, err := d.ResolveRepo(context.Background(), repo)
		if err != nil {
			t.Fatalf("%q: %v", repo, err)
		}
		// the temp dir may be a symlink (macOS /private) or an 8.3 name (Windows), so compare the real paths
		if got == "" || real(got) != real(clone) {
			t.Errorf("%q resolved to %q, want %q", repo, got, clone)
		}
	}
}
