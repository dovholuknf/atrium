package safepath

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Containment. Every test here is one of the ways the three rules get broken,
// because a containment check that is right most of the time is a containment
// check that is wrong.

func root(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// Resolved, because a temp directory is itself often a symlink (macOS
	// /var, and Windows short paths), and a test that compares against the
	// unresolved form tests the wrong thing.
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

func TestAPathInsideIsAllowed(t *testing.T) {
	dir := root(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Contained(dir, filepath.Join(dir, "a.txt"))
	if err != nil {
		t.Fatalf("a file inside the root was refused: %v", err)
	}
	if !strings.HasSuffix(got, "a.txt") {
		t.Fatalf("resolved to %q", got)
	}
}

func TestARelativePathIsResolvedAgainstTheRoot(t *testing.T) {
	dir := root(t)
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := Contained(dir, "sub")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(got) != dir {
		t.Fatalf("a relative path resolved to %q, outside %q", got, dir)
	}
}

// The obvious one, and the only one a lexical check catches.
func TestDotDotIsRefused(t *testing.T) {
	dir := root(t)
	for _, p := range []string{"..", "../..", "sub/../../elsewhere", "../sibling/file.txt"} {
		if _, err := Contained(dir, p); !errors.Is(err, ErrOutside) {
			t.Fatalf("%q was allowed out (err=%v)", p, err)
		}
	}
}

func TestAnAbsolutePathElsewhereIsRefused(t *testing.T) {
	dir := root(t)
	other := root(t)
	if _, err := Contained(dir, filepath.Join(other, "x.txt")); !errors.Is(err, ErrOutside) {
		t.Fatalf("a path in another directory was allowed: %v", err)
	}
}

// Rule two. `worktree-evil` is not inside `worktree`, and a plain HasPrefix
// says it is. This is the one that reads as paranoid and is not: two sibling
// worktrees for the same repo are routinely named that way.
func TestASiblingWithTheSamePrefixIsRefused(t *testing.T) {
	parent := root(t)
	inside := filepath.Join(parent, "work")
	sibling := filepath.Join(parent, "work-evil")
	for _, d := range []string{inside, sibling} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(sibling, "x.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Contained(inside, filepath.Join(sibling, "x.txt")); !errors.Is(err, ErrOutside) {
		t.Fatalf("a sibling sharing a prefix was treated as inside: %v", err)
	}
}

// Rule one, and the reason this function touches the filesystem at all. A
// lexical check on unresolved paths passes this every time.
func TestASymlinkPointingOutIsRefused(t *testing.T) {
	dir := root(t)
	outside := root(t)
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(dir, "escape")
	if err := os.Symlink(outside, link); err != nil {
		// Windows without developer mode refuses to make one. The rule still
		// holds, there is just no way to build the case here.
		t.Skipf("cannot create a symlink on this machine: %v", err)
	}

	if _, err := Contained(dir, filepath.Join(link, "secret.txt")); !errors.Is(err, ErrOutside) {
		t.Fatalf("a symlink out of the root was followed and allowed: %v", err)
	}
}

// A symlink that stays inside is fine, and refusing it would break an ordinary
// repository layout.
func TestASymlinkPointingInsideIsAllowed(t *testing.T) {
	dir := root(t)
	real := filepath.Join(dir, "real")
	if err := os.MkdirAll(real, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "a.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "alias")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("cannot create a symlink on this machine: %v", err)
	}

	if _, err := Contained(dir, filepath.Join(link, "a.txt")); err != nil {
		t.Fatalf("a symlink staying inside the root was refused: %v", err)
	}
}

// The case that gets missed. An upload names a file that is not there yet, so
// EvalSymlinks on the whole path fails, and the obvious implementation returns
// an error that reads like a permission problem.
func TestAPathThatDoesNotExistYetIsStillChecked(t *testing.T) {
	dir := root(t)
	got, err := Contained(dir, filepath.Join(dir, "not-yet", "file.png"))
	if err != nil {
		t.Fatalf("a file about to be created was refused: %v", err)
	}
	if !strings.HasSuffix(got, "file.png") {
		t.Fatalf("resolved to %q", got)
	}
	// And one that does not exist AND is outside is still refused, which is
	// the half that matters.
	if _, err := Contained(dir, filepath.Join(dir, "..", "nope", "file.png")); !errors.Is(err, ErrOutside) {
		t.Fatalf("a non-existent path outside the root was allowed: %v", err)
	}
}

// The root itself is inside the root. A download of the directory is refused
// elsewhere, on its own merits, and not by pretending it is out of bounds.
func TestTheRootIsInsideItself(t *testing.T) {
	dir := root(t)
	if _, err := Contained(dir, dir); err != nil {
		t.Fatalf("the root was refused against itself: %v", err)
	}
}

// Rule three. Getting this backwards fails open on Windows, which is the
// platform this mostly runs on.
func TestCaseFoldsOnWindowsAndNotElsewhere(t *testing.T) {
	dir := root(t)
	if err := os.MkdirAll(filepath.Join(dir, "Work"), 0o700); err != nil {
		t.Fatal(err)
	}
	upper := filepath.Join(dir, "Work")
	lower := filepath.Join(dir, "work")

	_, err := Contained(upper, filepath.Join(lower, "x.txt"))
	if runtime.GOOS == "windows" {
		if err != nil {
			t.Fatalf("windows treated two spellings of one directory as two: %v", err)
		}
		return
	}
	if !errors.Is(err, ErrOutside) {
		t.Fatalf("a case-different directory was treated as the same one: %v", err)
	}
}

func TestEmptyInputsAreRefused(t *testing.T) {
	dir := root(t)
	if _, err := Contained("", "x"); err == nil {
		t.Fatal("an empty root was accepted")
	}
	if _, err := Contained(dir, ""); err == nil {
		t.Fatal("an empty path was accepted")
	}
}

// SafeName. Belt and braces, because the upload path computes its own
// destination, but a name that can mean something is a name worth neutering.
func TestSafeNameStripsEverythingStructural(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"screenshot.png", "screenshot.png"},
		{"../../etc/passwd", "passwd"},
		{`..\..\windows\system32\a.dll`, "a.dll"},
		{"C:evil.txt", "evil.txt"},
		{"/absolute/thing.log", "thing.log"},
		{"..", "file"},
		{"...", "file"},
		{"", "file"},
		{"   ", "file"},
		{".hidden", "hidden"},
		{"a<b>c.txt", "a-b-c.txt"},
		{"tab\there.txt", "tabhere.txt"},
	} {
		if got := SafeName(tc.in); got != tc.want {
			t.Fatalf("SafeName(%q) = %q, wanted %q", tc.in, got, tc.want)
		}
	}
}

// The right-to-left override renders `gpj.exe` as `exe.jpg`, which is the
// oldest trick there is for making an executable look like an image.
func TestSafeNameStripsDirectionOverrides(t *testing.T) {
	got := SafeName("photo‮gpj.exe")
	if strings.ContainsRune(got, '‮') {
		t.Fatalf("a direction override survived: %q", got)
	}
}

// A very long name is cut and keeps its extension, which is the part that
// tells a model what it is looking at.
func TestSafeNameKeepsTheExtensionWhenItTruncates(t *testing.T) {
	got := SafeName(strings.Repeat("a", 400) + ".png")
	if len(got) > 120 {
		t.Fatalf("a long name came back %d characters", len(got))
	}
	if !strings.HasSuffix(got, ".png") {
		t.Fatalf("truncation lost the extension: %q", got)
	}
}
