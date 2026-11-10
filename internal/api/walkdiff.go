package api

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/base/gitexec"
)

// maxWalkDiffBytes bounds the patch the walk is handed.
const maxWalkDiffBytes = 4 << 20

// walkBases are the refs a branch is compared with when the card is not a pull request worktree, in order.
var walkBases = []string{"origin/HEAD", "origin/main", "origin/master", "hub/claude/main", "claude/main", "main", "master"}

type walkDiffOut struct {
	Branch    string `json:"branch"`
	Base      string `json:"base"`
	MergeBase string `json:"merge_base"`
	Head      string `json:"head"`
	Diff      string `json:"diff"`
	Truncated bool   `json:"truncated"`
}

// GET /v1/tasks/{id}/diff is the card's worktree against its base, for the walk when there are no findings.
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
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	out, status, err := walkBranchDiff(ctx, dir)
	if err != nil {
		writeErr(w, status, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func walkBranchDiff(ctx context.Context, dir string) (walkDiffOut, int, error) {
	var out walkDiffOut
	head, err := gitIn(ctx, dir, gitexec.RevParseVerify{Rev: "HEAD^{commit}", Quiet: true})
	if err != nil {
		return out, http.StatusConflict, errors.New(filepath.ToSlash(dir) + " has no commit to walk")
	}
	out.Head = strings.TrimSpace(head)
	if b, err := gitIn(ctx, dir, gitexec.RevParseAbbrevRef{Rev: "HEAD"}); err == nil {
		out.Branch = strings.TrimSpace(b)
	}
	base, why := walkBase(ctx, dir, out.Head)
	if base == "" {
		return out, http.StatusConflict, errors.New("no base to compare " + out.Branch + " with: " + why)
	}
	mb, err := gitIn(ctx, dir, gitexec.MergeBase{A: base, B: "HEAD"})
	if err != nil {
		return out, http.StatusConflict, errors.New(out.Branch + " shares no history with " + base)
	}
	out.Base, out.MergeBase = base, strings.TrimSpace(mb)
	patch, err := gitOut(ctx, dir, nil, gitexec.Diff{Revs: []string{out.MergeBase, "HEAD"}, NoExtDiff: true, NoTextconv: true,
		NoColor: true, FindRenames: true})
	if err != nil {
		return out, http.StatusInternalServerError, err
	}
	if len(patch) > maxWalkDiffBytes {
		patch, out.Truncated = patch[:maxWalkDiffBytes], true
	}
	out.Diff = string(patch)
	return out, http.StatusOK, nil
}

// walkBase is the ref to compare with: the pull request's merge base when the worktree carries one, else the first
// of walkBases that exists and is not HEAD itself. why is what was looked for when there is none.
func walkBase(ctx context.Context, dir, head string) (base, why string) {
	if raw, err := os.ReadFile(filepath.Join(filepath.FromSlash(dir), filepath.FromSlash(prBriefFile))); err == nil {
		for _, l := range strings.Split(string(raw), "\n") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(l), "Merge base:"); ok && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v), ""
			}
		}
	}
	for _, ref := range walkBases {
		sha, err := gitIn(ctx, dir, gitexec.RevParseVerify{Rev: ref + "^{commit}", Quiet: true})
		if err != nil || strings.TrimSpace(sha) == head {
			continue
		}
		return ref, ""
	}
	return "", "looked for " + strings.Join(walkBases, ", ")
}
