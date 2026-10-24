package api

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/dovholuknf/atrium/internal/cardroot"
	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/safepath"
)

// SettingOpenPaths is the folders the board may open files from besides the card's own and the atrium paths. One
// absolute directory per line, empty by default.
const SettingOpenPaths = "open_paths"

// atriumPaths is every folder the board may open a file from with no setting, besides the clicked card's own:
//   - the room's card root
//   - the directory of every card on this room, live or archived (a card's home is its directory)
//   - every repository checkout and worktree those cards sit in, and each repository's own checkout
//   - WORKTREE_ROOT and the worktree root of every forge provider
//
// A folder that does not exist is dropped when the set is resolved.
func (s *Server) atriumPaths() []string {
	var raw []string
	set, _ := s.st.Setting(cardroot.Setting)
	scm, _ := s.st.Setting(gitsync.SettingSCMRoot)
	if root, err := cardroot.Resolve(set, gitsync.EffectiveSCMRoot(scm)); err == nil {
		raw = append(raw, root)
	}
	for _, u := range s.cardDirs() {
		raw = append(raw, filepath.FromSlash(u.dir))
	}
	for _, c := range s.checkoutList() {
		raw = append(raw, filepath.FromSlash(c.Path), filepath.FromSlash(c.Root))
	}
	if w := strings.TrimSpace(os.Getenv("WORKTREE_ROOT")); w != "" {
		raw = append(raw, w)
	}
	if providers, err := s.st.Providers(); err == nil {
		for _, p := range providers {
			raw = append(raw, filepath.FromSlash(p.WorktreeRoot))
		}
	}
	return raw
}

// openRoots is the atrium paths and the allowlist, resolved.
func (s *Server) openRoots() []string {
	raw := s.atriumPaths()
	if v, err := s.st.Setting(SettingOpenPaths); err == nil {
		for _, p := range strings.FieldsFunc(v, func(r rune) bool { return r == '\n' || r == '\r' || r == ';' }) {
			raw = append(raw, strings.TrimSpace(p))
		}
	}
	return resolveRoots(raw)
}

// containedOrOpen resolves want inside the card, else, for an absolute path, inside an open root. Resolving follows
// symlinks on both sides, so a link or a `..` cannot leave a root. outside is true when the card did not hold it.
func containedOrOpen(card, want string, roots func() []string) (real string, outside bool, err error) {
	real, err = safepath.Contained(card, want)
	if err == nil || !filepath.IsAbs(want) {
		return real, false, err
	}
	for _, root := range roots() {
		if r, rerr := safepath.Contained(root, want); rerr == nil {
			return r, true, nil
		}
	}
	return "", false, err
}
