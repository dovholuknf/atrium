package cardroot

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// WHERE EVERY CARD'S HOME GOES: one folder per room. See docs/runtime/card-home-design.md section 1.

// Setting is the room setting that names the card root.
const Setting = "card_root"

// ErrNone is the answer on Windows with neither the setting nor WORKTREE_ROOT: there is no place to put a home, and
// the home folder is C:, which is the one place agent work must not go.
var ErrNone = errors.New("this room has no place for card homes. set card_root in its settings, or set WORKTREE_ROOT")

var (
	goos    = runtime.GOOS
	homeDir = os.UserHomeDir
)

// StateDir is where atrium keeps its own state: under WORKTREE_ROOT when it is set, else the home folder.
func StateDir() string {
	if root := strings.TrimRight(os.Getenv("WORKTREE_ROOT"), `\/`); root != "" {
		return filepath.Join(root, "hub")
	}
	if home, err := homeDir(); err == nil {
		return filepath.Join(home, ".atrium")
	}
	return ".atrium"
}

// Resolve is the card root for a stored setting: the setting when set, else atrium-cards under WORKTREE_ROOT, else
// ~/atrium/cards off Windows. It is derived on every read and never written back. scmRoot is the effective
// git.scm_root, "" when there is none. The result is refused by Check when it is not safe to remove folders in.
func Resolve(set, scmRoot string) (string, error) {
	root := strings.TrimSpace(set)
	if root == "" {
		if w := strings.TrimRight(os.Getenv("WORKTREE_ROOT"), `\/`); w != "" {
			root = filepath.Join(w, "atrium-cards")
		} else if goos != "windows" {
			home, err := homeDir()
			if err != nil || home == "" {
				return "", ErrNone
			}
			root = filepath.Join(home, "atrium", "cards")
		} else {
			return "", ErrNone
		}
	}
	return Check(root, StateDir(), scmRoot)
}

// Check refuses a card root a home's removal could do harm in: a drive root, the home folder or an ancestor of it,
// anything holding or inside the state directory, anything inside git.scm_root. It returns the absolute path.
func Check(path, stateDir, scmRoot string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		if h, err := homeDir(); err == nil && h != "" {
			path = filepath.Join(h, path[1:])
		}
	}
	if strings.ContainsAny(path, "\x00\n\r") {
		return "", errors.New("card_root is one path on one line")
	}
	if !filepath.IsAbs(filepath.FromSlash(path)) {
		return "", fmt.Errorf("card_root is an absolute path on this machine, not %q", path)
	}
	abs, err := filepath.Abs(filepath.FromSlash(path))
	if err != nil {
		return "", err
	}
	if filepath.Dir(abs) == abs {
		return "", errors.New("card_root is a drive root. name a folder of its own for card homes")
	}
	if h, err := homeDir(); err == nil && h != "" && within(abs, h) {
		return "", fmt.Errorf("card_root %s is the home folder or holds it. name a folder of its own for card homes", abs)
	}
	if stateDir = strings.TrimSpace(stateDir); stateDir != "" && (within(abs, stateDir) || within(stateDir, abs)) {
		return "", fmt.Errorf("card_root %s overlaps atrium's state directory %s. name a folder of its own for card homes",
			abs, stateDir)
	}
	if scmRoot = strings.TrimSpace(scmRoot); scmRoot != "" && within(scmRoot, abs) {
		return "", fmt.Errorf("card_root %s is inside git.scm_root %s. homes and clones are kept apart", abs, scmRoot)
	}
	return abs, nil
}

// within says whether inner is outer or inside it, compared as the platform does.
func within(outer, inner string) bool {
	o, err1 := filepath.Abs(filepath.FromSlash(outer))
	i, err2 := filepath.Abs(filepath.FromSlash(inner))
	if err1 != nil || err2 != nil {
		return false
	}
	rel, err := filepath.Rel(o, i)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
