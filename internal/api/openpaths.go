package api

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/base/cardroot"
	"github.com/dovholuknf/atrium/internal/base/safepath"
	"github.com/dovholuknf/atrium/internal/gitsync"
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
	return s.rootMemo.resolve(raw)
}

// openRootsFresh is how long a resolved root is reused. Resolving one follows every link up its path, which on a
// room with many cards cost more than the rest of a probe together.
const openRootsFresh = 20 * time.Second

type rootMemo struct {
	mu   sync.Mutex
	real map[string]memoRoot
}

type memoRoot struct {
	real string
	at   time.Time
}

// resolve is resolveRoots with each entry's answer, a miss too, kept for openRootsFresh.
func (m *rootMemo) resolve(raw []string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	next := make(map[string]memoRoot, len(raw))
	seen := map[string]bool{}
	var out []string
	for _, p := range raw {
		e, ok := next[p]
		if !ok {
			if e, ok = m.real[p]; !ok || now.Sub(e.at) > openRootsFresh {
				e = memoRoot{at: now}
				if r := resolveRoots([]string{p}); len(r) > 0 {
					e.real = r[0]
				}
			}
			next[p] = e
		}
		if e.real != "" && !seen[strings.ToLower(e.real)] {
			seen[strings.ToLower(e.real)] = true
			out = append(out, e.real)
		}
	}
	m.real = next
	sort.Strings(out)
	return out
}

// containedOrOpen resolves want inside the card, else, for an absolute path, inside an open root. Resolving follows
// symlinks on both sides, so a link or a `..` cannot leave a root. outside is true when the card did not hold it.
func containedOrOpen(card, want string, roots func() []string) (real string, outside bool, err error) {
	real, err = safepath.Contained(card, want)
	if err == nil || !filepath.IsAbs(want) {
		return real, false, err
	}
	resolved, werr := safepath.Real(want)
	for _, root := range roots() {
		// Resolved once here, so a root that cannot hold it is skipped without resolving every root's links again.
		rel, rerr := filepath.Rel(root, resolved)
		if werr == nil && (rerr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
			continue
		}
		if r, rerr := safepath.Contained(root, want); rerr == nil {
			return r, true, nil
		}
	}
	return "", false, err
}

// ContainedOrOpen is containedOrOpen against this server's open roots, for the room to bound a path a card names.
func (s *Server) ContainedOrOpen(card, want string) (string, error) {
	real, _, err := containedOrOpen(card, want, s.openRoots)
	return real, err
}
