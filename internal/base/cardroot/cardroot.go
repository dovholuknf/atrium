package cardroot

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// WHERE EVERY CARD'S HOME GOES: one folder per room. See docs/runtime/card-home-design.md section 1.

// Setting is the room setting that names the card root.
const Setting = "card_root"

// ErrNone is the answer on Windows with no setting, no WORKTREE_ROOT, no data drive, no LOCALAPPDATA and no home folder.
var ErrNone = errors.New("this room has no place for card homes. " + WindowsWhy + ". " + SetHow)

// WindowsWhy is the one clause for why Windows found no default card root.
const WindowsWhy = "Windows found no fixed drive besides C:, no LOCALAPPDATA and no home folder to put them in"

// SetHow names the command and the board setting that set card_root.
const SetHow = "set card_root to a folder of its own, best on a drive other than C:, for example " +
	"`atrium room set card_root D:/atrium/cards` (run it with the room stopped) or in the board's room settings"

// Env is what the functions ask the machine: the home folder, the OS name, and on Windows the fixed drives other than
// the system drive and LOCALAPPDATA. A nil func answers nothing.
type Env struct {
	GOOS         string
	HomeDir      func() (string, error)
	DataDrives   func() []string
	LocalAppData func() string
}

var system = Env{GOOS: runtime.GOOS, HomeDir: os.UserHomeDir, DataDrives: dataDrives,
	LocalAppData: func() string { return os.Getenv("LOCALAPPDATA") }}

// roomDefault is <ATRIUM_ROOT>/<room>/cards for a room started by name, set once before the room opens.
var roomDefault string

// SetRoomDefault is the card root Resolve answers when the setting is unset, ahead of WORKTREE_ROOT.
func SetRoomDefault(path string) { roomDefault = strings.TrimSpace(path) }

// StateDir is where atrium keeps its own state: under WORKTREE_ROOT when it is set, else the home folder.
func StateDir() string { return system.StateDir() }

// StateDir is the package function against this Env.
func (e Env) StateDir() string {
	if root := strings.TrimRight(os.Getenv("WORKTREE_ROOT"), `\/`); root != "" {
		return filepath.Join(root, "hub")
	}
	if home, err := e.HomeDir(); err == nil {
		return filepath.Join(home, ".atrium")
	}
	return ".atrium"
}

// Resolve is the card root for a stored setting: the setting when set, else the room's own folder when it was started
// by name, else atrium-cards under WORKTREE_ROOT, else ~/atrium/cards off Windows, else on Windows atrium/cards on the
// first fixed drive that is not the system drive, else under LOCALAPPDATA, else ~/atrium/cards, which a room stores
// once with Pin. scmRoot is the effective
// git.scm_root, "" when there is none. The result is refused by Check when it is not safe to remove folders in.
func Resolve(set, scmRoot string) (string, error) { return system.Resolve(set, scmRoot) }

// Resolve is the package function against this Env.
func (e Env) Resolve(set, scmRoot string) (string, error) {
	root := strings.TrimSpace(set)
	if root == "" {
		if roomDefault != "" {
			root = roomDefault
		} else if w := strings.TrimRight(os.Getenv("WORKTREE_ROOT"), `\/`); w != "" {
			root = filepath.Join(w, "atrium-cards")
		} else if e.GOOS != "windows" {
			home, err := e.HomeDir()
			if err != nil || home == "" {
				return "", ErrNone
			}
			root = filepath.Join(home, "atrium", "cards")
		} else if root = e.windowsDefault(); root == "" {
			return "", ErrNone
		}
	}
	return e.Check(root, e.StateDir(), scmRoot)
}

// Pin is the Windows default a room stores as its setting while the setting is unset, so a drive added later never
// moves it, "" when the default would not come from a drive.
func (e Env) Pin(set string) string {
	if strings.TrimSpace(set) != "" || roomDefault != "" || e.GOOS != "windows" ||
		strings.TrimRight(os.Getenv("WORKTREE_ROOT"), `\/`) != "" {
		return ""
	}
	return filepath.ToSlash(e.windowsDefault())
}

func (e Env) windowsDefault() string {
	if e.DataDrives != nil {
		if d := e.DataDrives(); len(d) > 0 {
			return filepath.Join(d[0], "atrium", "cards")
		}
	}
	if e.LocalAppData != nil {
		if l := strings.TrimSpace(e.LocalAppData()); l != "" {
			return filepath.Join(l, "atrium", "cards")
		}
	}
	if e.HomeDir != nil {
		if h, err := e.HomeDir(); err == nil && strings.TrimSpace(h) != "" {
			return filepath.Join(h, "atrium", "cards")
		}
	}
	return ""
}

// Check refuses a card root a home's removal could do harm in: a drive root, the home folder or an ancestor of it,
// anything holding or inside the state directory, anything inside git.scm_root. It returns the absolute path.
func Check(path, stateDir, scmRoot string) (string, error) {
	return system.Check(path, stateDir, scmRoot)
}

// Check is the package function against this Env.
func (e Env) Check(path, stateDir, scmRoot string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		if h, err := e.HomeDir(); err == nil && h != "" {
			path = filepath.Join(h, path[1:])
		}
	}
	if strings.ContainsAny(path, "\x00\n\r") {
		return "", errors.New("card_root is one path on one line")
	}
	if strings.Contains(path, "..") {
		return "", errors.New("card_root is a path with no ..")
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
	if h, err := e.HomeDir(); err == nil && h != "" && within(abs, h) {
		return "", fmt.Errorf("card_root %s is the home folder or holds it. name a folder of its own for card homes", abs)
	}
	if stateDir = strings.TrimSpace(stateDir); stateDir != "" && (within(abs, stateDir) || within(stateDir, abs)) {
		return "", fmt.Errorf("card_root %s overlaps atrium's state directory %s. name a folder of its own for card homes",
			abs, stateDir)
	}
	if scmRoot = strings.TrimSpace(scmRoot); scmRoot != "" && within(scmRoot, abs) {
		return "", fmt.Errorf("card_root %s is inside git.scm_root %s. homes and clones are kept apart", abs, scmRoot)
	}
	if err := examinable(abs); err != nil {
		return "", fmt.Errorf("card_root %s cannot be used: %v. claude must be able to examine every folder on the way to it", abs, err)
	}
	return abs, nil
}

// examinable answers the first existing component of path, links followed, that lstat refuses.
func examinable(path string) error {
	pending := []string{path}
	for links := 0; len(pending) > 0 && links < 40; {
		cur := pending[0]
		pending = pending[1:]
		for p := cur; ; {
			fi, err := os.Lstat(noNUL(p))
			if errors.Is(err, fs.ErrPermission) {
				return fmt.Errorf("%s is not readable", p)
			}
			if err == nil && fi.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
				if to, err := os.Readlink(noNUL(p)); err == nil {
					if !filepath.IsAbs(to) {
						to = filepath.Join(filepath.Dir(p), to)
					}
					pending = append(pending, to)
					links++
				}
			}
			if parent := filepath.Dir(p); parent != p {
				p = parent
				continue
			}
			break
		}
	}
	return nil
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

// SystemEnv is this machine's Env.
func SystemEnv() Env { return system }
