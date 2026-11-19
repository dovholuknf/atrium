package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanReposFindsCheckoutsAndWorktreesButNotInsideThem(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mk := func(p string, file bool) {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if file {
			if err := os.WriteFile(full, []byte("gitdir: x"), 0o644); err != nil {
				t.Fatal(err)
			}
			return
		}
		if err := os.MkdirAll(full, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mk("a/.git", false)
	mk("a/inner/.git", false)
	mk("org/b/.git", false)
	mk("wt/.git", true)
	mk("deep/x/y/z/.git", false)
	mk("node_modules/p/.git", false)

	got := scanRepos(filepath.ToSlash(root), 2)
	want := []string{"a", "org/b", "wt"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v under %s", got, want, root)
	}
	for i, w := range want {
		if got[i] != filepath.ToSlash(filepath.Join(root, w)) {
			t.Fatalf("got %v, want %v under %s", got, want, root)
		}
	}
}
