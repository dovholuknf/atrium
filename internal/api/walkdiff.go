package api

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/base/gitexec"
	"github.com/dovholuknf/atrium/internal/base/safepath"
)

// maxWalkDiffBytes bounds the patch the walk is handed.
const maxWalkDiffBytes = 4 << 20

// maxWalkUntracked bounds the new files read into it, one git each.
const maxWalkUntracked = 200

// walkBases are the refs a branch is compared with when the card is not a pull request worktree, in order.
var walkBases = []string{"origin/HEAD", "origin/main", "origin/master", "hub/claude/main", "claude/main", "main", "master"}

// walkClaudeMains are tried before walkBases: a claude/* branch measured from an origin/HEAD far behind claude/main
// walked hundreds of files it never touched.
var walkClaudeMains = []string{"hub/claude/main", "claude/main"}

// walkModes are what the walk's switch compares: the working tree against the index, the index against HEAD, and
// HEAD plus edits against the merge base with the branch's base.
var walkModes = []string{"unstaged", "staged", "branch"}

type walkDiffOut struct {
	Branch    string         `json:"branch"`
	Base      string         `json:"base"`
	MergeBase string         `json:"merge_base"`
	Head      string         `json:"head"`
	Diff      string         `json:"diff"`
	Truncated bool           `json:"truncated"`
	Mode      string         `json:"mode"`
	Modes     map[string]int `json:"modes"`
	Why       string         `json:"why,omitempty"`
}

// GET /v1/tasks/{id}/diff is the card's worktree, or the folder ?dir= names, compared the way ?mode= says, for the walk
// with no findings. No mode opens on unstaged when there is any, else staged, else branch. A git folder with nothing
// to walk answers 200 with why, so every git card has a walk.
func (s *Server) walkDiff(w http.ResponseWriter, r *http.Request) {
	task, err := s.st.Get(r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	dir := strings.TrimSpace(task.Worktree)
	if dir == "" {
		writeErr(w, http.StatusBadRequest, errors.New("this card has no directory"))
		return
	}
	if want := strings.TrimSpace(r.URL.Query().Get("dir")); want != "" {
		real, _, err := containedOrOpen(dir, filepath.FromSlash(want), s.openRoots)
		if err != nil {
			writeErr(w, http.StatusForbidden, safepath.ErrOutside)
			return
		}
		dir = filepath.ToSlash(real)
	}
	mode := r.URL.Query().Get("mode")
	if mode != "" && !slices.Contains(walkModes, mode) {
		writeErr(w, http.StatusBadRequest, errors.New("mode is unstaged, staged or branch"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if _, err := gitIn(ctx, dir, gitexec.RevParseShowToplevel{}); err != nil {
		writeErr(w, http.StatusConflict, errors.New(filepath.ToSlash(dir)+" is not a git checkout"))
		return
	}
	out, status, err := walkModeDiff(ctx, dir, mode)
	if err != nil {
		writeErr(w, status, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// walkModeDiff is the patch for mode, with how many files each mode holds.
func walkModeDiff(ctx context.Context, dir, mode string) (walkDiffOut, int, error) {
	untracked := walkNames(ctx, dir, gitexec.LsFiles{Others: true, ExcludeStandard: true, Z: true})
	modes := map[string]int{
		"unstaged": walkNames(ctx, dir, gitexec.Diff{NameOnly: true, Z: true}) + untracked,
		"staged":   walkNames(ctx, dir, gitexec.Diff{Cached: true, NameOnly: true, Z: true}),
	}
	if mode == "" {
		mode = "branch"
		if modes["unstaged"] > 0 {
			mode = "unstaged"
		} else if modes["staged"] > 0 {
			mode = "staged"
		}
	}
	var out walkDiffOut
	status, berr := walkBranchHead(ctx, dir, &out)
	onBase := berr == nil && out.Base == out.Branch
	if onBase {
		modes["branch"] = 0
	} else if berr == nil {
		modes["branch"] = walkNames(ctx, dir, gitexec.Diff{Revs: []string{out.MergeBase}, NameOnly: true, Z: true}) + untracked
	}
	out.Mode, out.Modes = mode, modes
	var patch []byte
	var err error
	switch mode {
	case "branch":
		if berr != nil {
			if status != http.StatusConflict {
				return out, status, berr
			}
			out.Why = berr.Error()
			return out, http.StatusOK, nil
		}
		if onBase {
			out.Why = "nothing to walk: " + out.Branch + " is a base, so only its unstaged and staged changes are walked"
			return out, http.StatusOK, nil
		}
		patch, err = gitOut(ctx, dir, nil, gitexec.Diff{Revs: []string{out.MergeBase}, NoExtDiff: true, NoTextconv: true,
			NoColor: true, FindRenames: true})
		if err == nil {
			patch = append(patch, walkUntracked(ctx, dir, maxWalkDiffBytes-len(patch))...)
		}
	case "unstaged":
		patch, err = gitOut(ctx, dir, nil, gitexec.Diff{NoExtDiff: true, NoTextconv: true, NoColor: true, FindRenames: true})
		if err == nil {
			patch = append(patch, walkUntracked(ctx, dir, maxWalkDiffBytes-len(patch))...)
		}
	case "staged":
		patch, err = gitOut(ctx, dir, nil, gitexec.Diff{Cached: true, NoExtDiff: true, NoTextconv: true, NoColor: true,
			FindRenames: true})
	}
	if err != nil {
		return out, http.StatusInternalServerError, err
	}
	if len(patch) > maxWalkDiffBytes {
		patch, out.Truncated = patch[:maxWalkDiffBytes], true
	}
	out.Diff = string(patch)
	if out.Diff == "" {
		name := out.Branch
		if name == "" {
			name = filepath.Base(filepath.FromSlash(dir))
		}
		out.Why = "nothing " + mode + " in " + name
		if mode == "branch" {
			out.Why = "nothing to walk: " + name + " is at its base " + out.Base + " and nothing is uncommitted"
		}
	}
	return out, http.StatusOK, nil
}

// walkNames is how many paths a -z listing names, 0 when git cannot say.
func walkNames(ctx context.Context, dir string, op gitexec.Op) int {
	list, err := gitOut(ctx, dir, nil, op)
	if err != nil {
		return 0
	}
	n := 0
	for _, p := range strings.Split(string(list), "\x00") {
		if p != "" {
			n++
		}
	}
	return n
}

// walkBranchHead fills in HEAD, the branch, its base and their merge base.
func walkBranchHead(ctx context.Context, dir string, out *walkDiffOut) (int, error) {
	head, err := gitIn(ctx, dir, gitexec.RevParseVerify{Rev: "HEAD^{commit}", Quiet: true})
	if err != nil {
		return http.StatusConflict, errors.New(filepath.ToSlash(dir) + " has no commit to walk")
	}
	out.Head = strings.TrimSpace(head)
	if b, err := gitIn(ctx, dir, gitexec.RevParseAbbrevRef{Rev: "HEAD"}); err == nil {
		out.Branch = strings.TrimSpace(b)
	}
	base, why := walkBase(ctx, dir, out.Branch)
	if base == "" {
		return http.StatusConflict, errors.New("no base to compare " + out.Branch + " with: " + why)
	}
	mb, err := gitIn(ctx, dir, gitexec.MergeBase{A: base, B: "HEAD"})
	if err != nil {
		return http.StatusConflict, errors.New(out.Branch + " shares no history with " + base)
	}
	out.Base, out.MergeBase = base, strings.TrimSpace(mb)
	return http.StatusOK, nil
}

// walkUntracked is each new file git does not ignore as a patch from nothing, until room runs out.
func walkUntracked(ctx context.Context, dir string, room int) []byte {
	list, err := gitOut(ctx, dir, nil, gitexec.LsFiles{Others: true, ExcludeStandard: true, Z: true})
	if err != nil {
		return nil
	}
	var out []byte
	for i, rel := range strings.Split(string(list), "\x00") {
		if rel == "" || len(out) > room || i >= maxWalkUntracked {
			break
		}
		p, err := gitexec.Run(ctx, gitexec.Opts{Dir: filepath.FromSlash(dir), Env: []string{"GIT_TERMINAL_PROMPT=0"}},
			gitexec.DiffNoIndex{A: "/dev/null", B: rel})
		// git diff --no-index exits 1 when the sides differ
		var ee *exec.ExitError
		if err == nil || errors.As(err, &ee) && ee.ExitCode() == 1 {
			out = append(out, p...)
		}
	}
	return out
}

// walkBase is the ref to compare with: the pull request's merge base when the worktree carries one, the branch itself
// when it is one of walkBases, else the one of walkClaudeMains, then of walkBases, that HEAD is fewest commits ahead of.
// why is what was looked for when there is none.
func walkBase(ctx context.Context, dir, branch string) (base, why string) {
	if raw, err := os.ReadFile(filepath.Join(filepath.FromSlash(dir), filepath.FromSlash(prBriefFile))); err == nil {
		for _, l := range strings.Split(string(raw), "\n") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(l), "Merge base:"); ok && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v), ""
			}
		}
	}
	if slices.Contains(walkBases, branch) {
		return branch, ""
	}
	if base = walkNearest(ctx, dir, walkClaudeMains); base != "" {
		return base, ""
	}
	if base = walkNearest(ctx, dir, walkBases); base != "" {
		return base, ""
	}
	return "", "looked for " + strings.Join(walkBases, ", ")
}

// walkNearest is the one of refs that HEAD is fewest commits ahead of, the earlier on a tie, "" when none shares
// history with HEAD.
func walkNearest(ctx context.Context, dir string, refs []string) (base string) {
	ahead := -1
	for _, ref := range refs {
		if _, err := gitIn(ctx, dir, gitexec.RevParseVerify{Rev: ref + "^{commit}", Quiet: true}); err != nil {
			continue
		}
		mb, err := gitIn(ctx, dir, gitexec.MergeBase{A: ref, B: "HEAD"})
		if err != nil {
			continue
		}
		n, err := gitIn(ctx, dir, gitexec.RevListCount{Revs: []string{strings.TrimSpace(mb) + "..HEAD"}})
		c, cerr := strconv.Atoi(strings.TrimSpace(n))
		if err != nil || cerr != nil {
			continue
		}
		if ahead < 0 || c < ahead {
			base, ahead = ref, c
		}
	}
	return base
}
