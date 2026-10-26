package worktrees

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/gitsync"
)

// Update is one worktree's outcome. Skipped says why nothing was done.
type Update struct {
	Path    string `json:"path"`
	Branch  string `json:"branch"`
	Done    string `json:"done,omitempty"`
	Skipped string `json:"skipped,omitempty"`
}

// Updates fast-forwards every clean worktree of the repository whose branch has a live upstream, after one fetch that
// prunes. It never touches a dirty worktree, and every skip is reported with its reason.
func Updates(ctx context.Context, repo string, run *gitsync.Runner) ([]Update, error) {
	if run == nil {
		run = gitsync.NewRunner()
		defer run.Stop(time.Second)
	}
	if run.Bound == 0 {
		run.Bound = CommandBound
	}
	repo = filepath.FromSlash(repo)
	out, err := run.Git(ctx, repo, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, &Refusal{"git could not list the worktrees: " + firstLine(err)}
	}
	if _, err := run.Git(ctx, repo, "fetch", "--prune"); err != nil {
		return nil, &Refusal{"git could not fetch, so no upstream is known to be live: " + firstLine(err)}
	}
	var res []Update
	for _, l := range parseList(out) {
		res = append(res, updateOne(ctx, run, l))
	}
	return res, nil
}

func updateOne(ctx context.Context, run *gitsync.Runner, l Line) Update {
	u := Update{Path: l.Path, Branch: l.Branch}
	dir := filepath.FromSlash(l.Path)
	switch {
	case l.Missing:
		u.Skipped = "directory is gone"
		return u
	case l.Detached || l.Branch == "" || l.Branch == "(bare)":
		u.Skipped = "no branch checked out"
		return u
	case l.Locked:
		u.Skipped = ReasonLocked
		return u
	}
	st, err := run.Git(ctx, dir, "-c", "core.quotepath=off", "status", "--porcelain")
	if err != nil {
		u.Skipped = "status: " + firstLine(err)
		return u
	}
	if n, _ := countDirty(st); n > 0 {
		u.Skipped = ReasonDirty
		return u
	}
	up, err := run.Git(ctx, dir, "for-each-ref", "--format=%(upstream:short)|%(upstream:track)", "refs/heads/"+l.Branch)
	if err != nil {
		u.Skipped = "upstream: " + firstLine(err)
		return u
	}
	name, track, _ := strings.Cut(strings.TrimSpace(up), "|")
	switch {
	case name == "":
		u.Skipped = "no upstream"
		return u
	case strings.Contains(track, "gone"):
		u.Skipped = "upstream " + name + " is gone"
		return u
	}
	before, err := run.Git(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		u.Skipped = "rev-parse: " + firstLine(err)
		return u
	}
	if _, err := run.Git(ctx, dir, "merge", "--ff-only", name); err != nil {
		u.Skipped = "cannot fast-forward to " + name + ": " + firstLine(err)
		return u
	}
	after, _ := run.Git(ctx, dir, "rev-parse", "HEAD")
	if strings.TrimSpace(before) == strings.TrimSpace(after) {
		u.Done = "already up to date with " + name
		return u
	}
	u.Done = "fast-forwarded to " + name + " (" + short(strings.TrimSpace(before), 7) + ".." + short(strings.TrimSpace(after), 7) + ")"
	return u
}
