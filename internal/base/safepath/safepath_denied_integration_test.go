//go:build integration

package safepath

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
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
	if runtime.GOOS == "windows" {
		t.Skip("chmod 0 does not deny lstat here, so the walk rightly sees no link")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "denied")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	eval := denied(dir)
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })

	for _, p := range []string{filepath.Join(dir, "link"), filepath.Join(dir, "link", "secret")} {
		if _, err := containedWith(eval, root, p); err == nil {
			t.Fatalf("%s: a denied directory under the root was walked past", p)
		}
	}
}

func TestADeniedAncestorIsWalkedPastWhenTheRootResolvesToAnotherSpelling(t *testing.T) {
	real := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	eval := func(p string) (string, error) {
		if strings.EqualFold(p, filepath.Join(alias, "work")) {
			return "", &fs.PathError{Op: "CreateFile", Path: p, Err: fs.ErrPermission}
		}
		if rel, err := filepath.Rel(alias, p); err == nil && !strings.HasPrefix(rel, "..") {
			p = filepath.Join(real, rel)
		}
		return filepath.EvalSymlinks(p)
	}
	root := filepath.Join(alias, "work", "cards")

	if _, err := containedWith(eval, root, filepath.Join(root, "home")); err != nil {
		t.Fatalf("a home under a root whose ancestor is denied was refused: %v", err)
	}
	if _, err := containedWith(eval, root, filepath.Join(alias, "work", "cards-evil", "x")); !errors.Is(err, ErrOutside) {
		t.Fatalf("err %v, want ErrOutside", err)
	}
}

func deniedWholeTree(root string) func(string) (string, error) {
	return func(p string) (string, error) {
		if strings.HasPrefix(strings.ToLower(p), strings.ToLower(root)) {
			return "", &fs.PathError{Op: "FindFirstFile", Path: p, Err: fs.ErrPermission}
		}
		return filepath.EvalSymlinks(p)
	}
}

func TestAnExistingPathBelowTheRootIsVettedWhenOnlyTheEvaluatorIsDenied(t *testing.T) {
	root := t.TempDir()
	clone := filepath.Join(root, "github", "o", "r")
	if err := os.MkdirAll(clone, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := containedWith(deniedWholeTree(root), root, clone); err != nil {
		t.Fatalf("an existing clone under the root was refused: %v", err)
	}
}

func TestADeniedEvaluatorStillSeesALinkOutOfTheRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "out")); err != nil {
		t.Skip("no symlinks here")
	}
	if _, err := containedWith(deniedWholeTree(root), root, filepath.Join(root, "out")); !errors.Is(err, ErrOutside) {
		t.Fatalf("err %v, want ErrOutside", err)
	}
}
