package api

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dovholuknf/atrium/internal/safepath"
)

// Where the directory picker may look.
//
// A BOUND EXISTS BECAUSE THE BOARD CAN BE PUBLISHED. A share terminates on this
// machine, so every request over one presents as `127.0.0.1` and nothing can be
// decided by source address. Unbounded, `GET /v1/browse` over a share is an
// unauthenticated recursive directory listing of the whole machine. It
// enumerates rather than discloses, since nothing reads file contents through
// it, which is why this is a bound rather than an incident.
//
// Everything outside the root set is refused the same way whether or not it
// exists. The default keeps the picker useful without being the machine:
//
//   - the home directory
//   - every directory a card, fixture, source or harness already names, which
//     are directories atrium has already been pointed at
//
// `SettingBrowseRoots` widens it for anyone whose work is somewhere else. It
// is a setting rather than a flag because it is a fact about one machine's
// layout, and it is a list rather than a boolean because "off" would mean the
// old behaviour and there is no good reason to keep that reachable.
const SettingBrowseRoots = "browse_roots"

// browseRootsFor is the allowed set, resolved.
//
// Resolved rather than compared as text, because the whole point of the check
// below is that a symlink is not a string. A root that cannot be resolved is
// dropped: a configured path that does not exist is not a permission to
// anything.
func (s *Server) browseRootsFor() []string {
	var raw []string

	if v, err := s.st.Setting(SettingBrowseRoots); err == nil && strings.TrimSpace(v) != "" {
		// Newlines or semicolons. Not the platform's list separator: that is
		// `;` on Windows and `:` on Unix, and `:` is in every Windows path.
		for _, p := range strings.FieldsFunc(v, func(r rune) bool {
			return r == '\n' || r == '\r' || r == ';'
		}) {
			raw = append(raw, strings.TrimSpace(p))
		}
	}

	if len(raw) == 0 {
		if home, err := os.UserHomeDir(); err == nil {
			raw = append(raw, home)
		}
		// EVERY DIRECTORY ATRIUM HAS ALREADY BEEN POINTED AT, and that is four
		// kinds of row rather than one.
		//
		// The picker is opened from four fields: the launch form, a source, a
		// fixture and a harness. Taking only task worktrees looked right and
		// broke three of the four: a fixture whose directory is outside home
		// could not browse to its own directory, and the board's `browseTo`
		// catches the refusal and silently drops back to the root list, so it
		// reads as the picker forgetting where it was.
		if tasks, err := s.st.List(); err == nil {
			for _, t := range tasks {
				raw = append(raw, filepath.FromSlash(t.Worktree))
			}
		}
		if fixtures, err := s.st.Fixtures(); err == nil {
			for _, f := range fixtures {
				raw = append(raw, filepath.FromSlash(f.Cwd))
			}
		}
		if sources, err := s.st.Sources(); err == nil {
			for _, src := range sources {
				raw = append(raw, filepath.FromSlash(src.Cwd))
			}
		}
		if harnesses, err := s.st.Harnesses(); err == nil {
			for _, h := range harnesses {
				raw = append(raw, filepath.FromSlash(h.Cwd))
			}
		}
	}

	seen := map[string]bool{}
	var out []string
	for _, p := range raw {
		if p == "" {
			continue
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		// `Contained(p, p)` is the cheapest way to get the resolved form
		// through the same code path everything else is checked with.
		real, err := safepath.Contained(abs, abs)
		if err != nil || seen[strings.ToLower(real)] {
			continue
		}
		seen[strings.ToLower(real)] = true
		out = append(out, real)
	}
	sort.Strings(out)
	return out
}

// errOutsideRoots is the one refusal this endpoint gives. It names the setting,
// because "not allowed" with no way to change it reads as a bug.
var errOutsideRoots = browseErr("that directory is outside the places atrium may browse. " +
	"it lists your home directory and every directory a card already uses. " +
	"add others under settings, this machine, browse roots")

type browseErr string

func (e browseErr) Error() string { return string(e) }

// eqPath compares two paths the way the platform does.
func eqPath(a, b string) bool {
	if filepath.Separator == '\\' {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// insideARoot reports whether a path is inside any allowed root, and returns
// the resolved path when it is.
//
// One answer for outside, missing and unreadable, which is the same posture
// every other path endpoint here takes: a refusal that distinguishes them is an
// oracle for what is on the machine.
func insideARoot(roots []string, path string) (string, bool) {
	for _, root := range roots {
		if real, err := safepath.Contained(root, path); err == nil {
			return real, true
		}
	}
	return "", false
}

// browseRootEntries is what the picker shows when nothing is open yet.
//
// The allowed roots themselves, rather than the machine's drives. On a machine
// where the roots are the home directory and three worktrees, that is a better
// starting list than `C: D:` anyway: it is where the work is.
func browseRootEntries(roots []string) []browseEntry {
	out := make([]browseEntry, 0, len(roots))
	for _, r := range roots {
		name := filepath.Base(r)
		if name == "" || name == string(filepath.Separator) {
			name = r
		}
		repo := false
		if _, err := os.Stat(filepath.Join(r, ".git")); err == nil {
			repo = true
		}
		out = append(out, browseEntry{
			Name: name, Path: filepath.ToSlash(r), Repo: repo,
		})
	}
	return out
}
