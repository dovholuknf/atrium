//go:build integration

package safepath

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func denied(dir string) func(string) (string, error) {
	return func(p string) (string, error) {
		if strings.EqualFold(p, dir) {
			return "", &fs.PathError{Op: "CreateFile", Path: p, Err: fs.ErrPermission}
		}
		return filepath.EvalSymlinks(p)
	}
}

func TestADeniedAncestorIsWalkedPastLikeAMissingOne(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "work", "cards")
	eval := denied(filepath.Join(base, "work"))
	home := filepath.Join(root, "second-room-hooks")

	got, err := containedWith(eval, root, home)
	if err != nil {
		t.Fatalf("a home under a root whose ancestor is denied was refused: %v", err)
	}
	want, _ := filepath.EvalSymlinks(base)
	if !strings.EqualFold(got, filepath.Join(want, "work", "cards", "second-room-hooks")) {
		t.Fatalf("resolved to %s", got)
	}
}

func TestADeniedAncestorStillRefusesAPathOutsideTheRoot(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "work", "cards")
	eval := denied(filepath.Join(base, "work"))

	for _, p := range []string{
		filepath.Join(base, "work", "cards-evil", "x"),
		filepath.Join(root, "..", "other"),
		filepath.Join(base, "elsewhere"),
	} {
		if _, err := containedWith(eval, root, p); !errors.Is(err, ErrOutside) {
			t.Fatalf("%s: err %v, want ErrOutside", p, err)
		}
	}
}

func TestADeniedDirectoryUnderTheRootIsRefused(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "denied")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	eval := denied(dir)

	for _, p := range []string{dir, filepath.Join(dir, "link"), filepath.Join(dir, "link", "secret")} {
		if _, err := containedWith(eval, root, p); err == nil {
			t.Fatalf("%s: a denied directory under the root was walked past", p)
		}
	}
}
