//go:build integration

package safepath

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

func deniedBelow(t *testing.T, open string) {
	t.Helper()
	orig := evalSymlinks
	t.Cleanup(func() { evalSymlinks = orig })
	evalSymlinks = func(p string) (string, error) {
		if len(p) > len(open) && strings.EqualFold(p[:len(open)], open) {
			return "", &fs.PathError{Op: "CreateFile", Path: p, Err: fs.ErrPermission}
		}
		return orig(p)
	}
}

func TestADeniedAncestorIsWalkedPastLikeAMissingOne(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "work", "cards")
	deniedBelow(t, base+string(filepath.Separator)+"work")
	home := filepath.Join(root, "second-room-hooks")

	got, err := Contained(root, home)
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
	deniedBelow(t, base+string(filepath.Separator)+"work")

	for _, p := range []string{
		filepath.Join(base, "work", "cards-evil", "x"),
		filepath.Join(root, "..", "other"),
		filepath.Join(base, "elsewhere"),
	} {
		if _, err := Contained(root, p); !errors.Is(err, ErrOutside) {
			t.Fatalf("%s: err %v, want ErrOutside", p, err)
		}
	}
}
