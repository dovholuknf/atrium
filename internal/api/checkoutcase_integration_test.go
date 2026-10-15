//go:build integration

package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindProviderCheckoutOnDisk(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "O", "R", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "o")); err == nil {
		t.Skip("this filesystem folds case")
	}
	want := filepath.ToSlash(filepath.Join(root, "O", "R"))
	for _, c := range [][2]string{{"O", "R"}, {"o", "r"}} {
		if got := findProviderCheckout(filepath.ToSlash(root), c[0], c[1]); got != want {
			t.Errorf("%v = %q, want %q", c, got, want)
		}
	}
}
