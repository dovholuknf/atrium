// Package safepath answers one question: is this path inside that directory?
//
// It exists because nothing in atrium answered it. `internal/api/browse.go`
// applies `filepath.Clean` to caller input and nothing else, which is fine for
// a read-only directory picker on loopback and is not a containment check.
// `docs/charon.md` assumed there was an answer in there to reuse; there was
// not, and `docs/file-transfer-design.md` records the correction.
//
// Nothing here consults a card, a store or a request. One function, three
// rules, and tests for each of the ways the rules get broken.
package safepath

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"
)

// ErrOutside is returned when a path resolves outside its root.
var ErrOutside = errors.New("that path is outside where this card is allowed to reach")

// Contained resolves path against root and reports the resolved form, refusing
// anything that lands outside.
//
// Three rules, each of which is a hole if it is missed.
//
// **Symlinks are resolved on both sides.** A lexical prefix test on unresolved
// paths is the classic mistake: `worktree/link` where `link` is a junction to
// `C:/` passes every string comparison there is. This is the rule that makes
// the function worth having, and the reason it touches the filesystem rather
// than being pure string work.
//
// **The comparison is on a separator boundary.** `worktree-evil` must not be
// inside `worktree`, and a plain `strings.HasPrefix` says it is.
//
// **Case folds on Windows and nowhere else.** `D:/Work` and `d:/work` are one
// directory here and two on Linux. Getting this backwards fails OPEN on the
// platform atrium mostly runs on, which is the worst way to get it wrong.
//
// A path that does not exist yet is resolved as far as it does exist and the
// remainder is re-joined, so a file about to be created is still checked
// against the directory that will hold it. That case is the one that gets
// missed, because the obvious implementation calls EvalSymlinks on the whole
// path and returns an error that reads like a permission problem.
func Contained(root, path string) (string, error) {
	root = strings.TrimSpace(root)
	path = strings.TrimSpace(path)
	if root == "" {
		return "", errors.New("no directory to resolve against")
	}
	if path == "" {
		return "", errors.New("no path given")
	}

	absRoot, err := filepath.Abs(filepath.FromSlash(root))
	if err != nil {
		return "", err
	}
	realRoot, err := resolve(absRoot)
	if err != nil {
		return "", err
	}

	target := filepath.FromSlash(path)
	if !filepath.IsAbs(target) {
		target = filepath.Join(realRoot, target)
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	realTarget, err := resolve(absTarget)
	if err != nil {
		return "", err
	}

	if !within(realRoot, realTarget) {
		return "", ErrOutside
	}
	return realTarget, nil
}

// within compares two already-resolved absolute paths.
func within(root, target string) bool {
	root = strings.TrimRight(root, string(filepath.Separator))
	if eq(root, target) {
		return true
	}
	// The separator is what stops `worktree-evil` counting as inside
	// `worktree`.
	return eq(root+string(filepath.Separator), target[:min(len(target), len(root)+1)])
}

func eq(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// resolve follows symlinks as far as the path exists, then re-joins whatever
// is left.
//
// An upload names a file that is not there yet, so EvalSymlinks on the whole
// path fails. Walking up to the longest existing ancestor and resolving THAT
// is what makes a not-yet-existing path checkable: whatever directory will
// hold the new file does exist, and that is the thing worth resolving.
func resolve(path string) (string, error) {
	rest := ""
	cur := path
	for {
		real, err := filepath.EvalSymlinks(cur)
		if err == nil {
			if rest == "" {
				return real, nil
			}
			return filepath.Join(real, rest), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			// Walked all the way up and nothing exists. Nothing to resolve
			// against, so the cleaned form is the honest answer.
			return filepath.Clean(path), nil
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

// SafeName turns a caller's filename into one that cannot escape a directory.
//
// Everything structural goes: separators, drive letters, `..`, control
// characters, and the leading dots that make a file invisible in the place it
// was uploaded to. The extension is kept, because that is what tells a model
// what it is looking at.
//
// This is belt and braces. The upload path computes its own destination and
// never joins caller input as a path, so nothing here is load-bearing on its
// own. It exists so that the file lands under a name a person recognises
// without that name being able to mean anything.
func SafeName(name string) string {
	// Take the last segment under either separator, so `a\b\c.png` and
	// `a/b/c.png` both give `c.png` whatever platform wrote them.
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	// A Windows drive-relative name like `C:evil` has no separator in it.
	if i := strings.LastIndex(name, ":"); i >= 0 {
		name = name[i+1:]
	}

	var b strings.Builder
	for _, r := range name {
		switch {
		case r < 0x20 || r == 0x7f:
			continue
		case unicode.In(r, unicode.Cf):
			// Format characters, which are not control characters and are the
			// interesting half. U+202E, the right-to-left override, renders
			// `photo<RLO>gpj.exe` as `photo exe.jpg`, which is the oldest
			// trick there is for making an executable look like an image.
			// Nothing legitimate needs one in a filename.
			continue
		case strings.ContainsRune(`<>:"/\|?*`, r):
			b.WriteByte('-')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.Trim(b.String(), " .")
	if out == "" {
		out = "file"
	}
	if len(out) > 120 {
		// Keep the extension, which is the part that carries meaning.
		ext := filepath.Ext(out)
		if len(ext) > 16 {
			ext = ""
		}
		out = out[:120-len(ext)] + ext
	}
	return out
}
