package rolebin

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestInstalledMapsEveryRoleNameBack(t *testing.T) {
	dir := t.TempDir()
	canon := filepath.Join(dir, "atrium"+ext())
	write(t, canon, "x")
	for _, n := range []string{"atrium-hub", "atrium-room", "atrium-pty", "atrium.ptyhost"} {
		if got := installedFrom(filepath.Join(dir, n+ext())); got != canon {
			t.Fatalf("%s mapped to %s, want %s", n, got, canon)
		}
	}
	if got := installedFrom(canon); got != canon {
		t.Fatalf("atrium mapped to %s", got)
	}
}

func TestInstalledKeepsARoleNameWhenAtriumIsGone(t *testing.T) {
	self := filepath.Join(t.TempDir(), "atrium-pty"+ext())
	if got := installedFrom(self); got != self {
		t.Fatalf("got %s", got)
	}
}

func TestEnsureCopiesToTheRoleName(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "atrium"+ext())
	write(t, exe, "build-one")
	got := EnsureIn("pty", exe, dir)
	if want := filepath.Join(dir, "atrium-pty"+ext()); got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	if read(t, got) != "build-one" {
		t.Fatal("the copy differs")
	}
}

func TestEnsureReplacesAStaleCopyOfTheSameSize(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "atrium"+ext())
	dst := filepath.Join(dir, "atrium-pty"+ext())
	write(t, exe, "build-two")
	write(t, dst, "build-one")
	if got := EnsureIn("pty", exe, dir); got != dst || read(t, dst) != "build-two" {
		t.Fatalf("got %s with %q", got, read(t, dst))
	}
}

func TestEnsureUsesAHeldCopyAsItIs(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "atrium"+ext())
	dst := filepath.Join(dir, "atrium-pty"+ext())
	write(t, exe, "build-two")
	write(t, dst, "build-one")
	old := rename
	rename = func(a, b string) error { return errors.New("held") }
	t.Cleanup(func() { rename = old })
	if got := EnsureIn("pty", exe, dir); got != dst || read(t, dst) != "build-one" {
		t.Fatalf("got %s with %q", got, read(t, dst))
	}
	if _, err := os.Stat(dst + ".new"); err == nil {
		t.Fatal("the .new file was left behind")
	}
}

func TestEnsureNeverMovesThePtyCopyAside(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "atrium"+ext())
	write(t, exe, "build-two")
	write(t, filepath.Join(dir, "atrium-pty"+ext()), "build-one")
	var moved []string
	old := rename
	rename = func(a, b string) error { moved = append(moved, a); return old(a, b) }
	t.Cleanup(func() { rename = old })
	EnsureIn("pty", exe, dir)
	if len(moved) != 1 || filepath.Base(moved[0]) != "atrium-pty"+ext()+".new" {
		t.Fatalf("renames: %v", moved)
	}
}

func TestEnsureMovesAHubCopyAsideAndSweepsItNextTime(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "atrium"+ext())
	dst := filepath.Join(dir, "atrium-hub"+ext())
	write(t, exe, "build-two")
	write(t, dst, "build-one")
	EnsureIn("hub", exe, dir)
	aside := filepath.Join(dir, "atrium-hub.old"+ext())
	if read(t, dst) != "build-two" || read(t, aside) != "build-one" {
		t.Fatal("the hub copy was not swapped aside")
	}
	EnsureIn("hub", exe, dir)
	if _, err := os.Stat(aside); err == nil {
		t.Fatal("the aside copy survived the next Ensure")
	}
}

func TestEnsureFallsBackToTheBinaryOnFailure(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "missing"+ext())
	if got := EnsureIn("pty", exe, t.TempDir()); got != exe {
		t.Fatalf("got %s", got)
	}
}

func TestEnsureIsOffWhenAsked(t *testing.T) {
	t.Setenv("ATRIUM_ROLEBIN", "off")
	dir := t.TempDir()
	exe := filepath.Join(dir, "atrium"+ext())
	write(t, exe, "x")
	if got := EnsureIn("pty", exe, dir); got != exe {
		t.Fatalf("got %s", got)
	}
}

func TestRemoveLegacyDeletesTheOldCopy(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "atrium.ptyhost"+ext())
	write(t, old, "x")
	RemoveLegacy(dir)
	if _, err := os.Stat(old); err == nil {
		t.Fatal("the legacy copy is still there")
	}
}
