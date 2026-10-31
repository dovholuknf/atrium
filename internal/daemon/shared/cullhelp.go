package shared

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/base/nowindow"
)

// DefaultCullInto is the branch a worker's branch has to be merged into.
const DefaultCullInto = "claude/main"

// KeepOpenTag is the tag that says a card is not closed by a done report and is never reclaimed.
const KeepOpenTag = "atrium:keep-open"

// PreflightOutput caps what one preflight command may say.
const PreflightOutput = 4 << 10

// CullGitTimeout bounds each git command a cull runs.
const CullGitTimeout = 20 * time.Second

// CullResult is what a cull did and did not do. Kept is the reason the
// worktree or branch is still there, empty when both went.
type CullResult struct {
	Card            string `json:"card"`
	Exited          bool   `json:"exited"`
	Branch          string `json:"branch,omitempty"`
	Into            string `json:"into"`
	Worktree        string `json:"worktree,omitempty"`
	WorktreeRemoved bool   `json:"worktree_removed"`
	BranchDeleted   bool   `json:"branch_deleted"`
	// BytesFreed is what the worktree held on disk, ignored files included, counted only once its path is gone.
	BytesFreed int64 `json:"bytes_freed"`
	// BriefRemoved and ScratchRemoved say what became of the BRIEF.md atrium wrote and of a directory that was
	// not a git worktree. See reclaim.go.
	BriefRemoved   bool `json:"brief_removed"`
	ScratchRemoved bool `json:"scratch_removed"`
	// ScratchDirRemoved is the card's scratch_root folder, removed only when atrium made it.
	ScratchDirRemoved bool   `json:"scratch_dir_removed,omitempty"`
	Kept              string `json:"kept,omitempty"`
}

// GitIn runs git in dir and returns its trimmed output, or git's own reason.
func GitIn(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), CullGitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	nowindow.Hide(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(msg)
	}
	return strings.TrimSpace(string(out)), nil
}

// HasDotGit says whether dir has a .git entry of its own.
func HasDotGit(dir string) bool {
	_, err := os.Lstat(filepath.Join(filepath.FromSlash(dir), ".git"))
	return err == nil
}

// SamePath compares two paths the way the platform does, once resolved.
func SamePath(a, b string) bool {
	if ra, err := filepath.EvalSymlinks(a); err == nil {
		a = ra
	}
	if rb, err := filepath.EvalSymlinks(b); err == nil {
		b = rb
	}
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// Plural writes a count with its noun.
func Plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

// OrDefault is def when s is blank.
func OrDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

// ProtectedBranch is a branch a cull never deletes, whatever the worktree says.
func ProtectedBranch(branch, into string) bool {
	switch strings.ToLower(branch) {
	case "main", "master", "claude/main", strings.ToLower(into):
		return true
	}
	return false
}

// LimitedWriter passes bytes through until a budget runs out, then discards the rest. Not an error on overflow:
// returning one makes the child's next write fail, and the caller checks how much arrived.
type LimitedWriter struct {
	W    io.Writer
	Left int
}

func (l *LimitedWriter) Write(p []byte) (int, error) {
	if l.Left <= 0 {
		return len(p), nil
	}
	take := p
	if len(take) > l.Left {
		take = take[:l.Left]
	}
	n, err := l.W.Write(take)
	l.Left -= n
	if err != nil {
		return n, err
	}
	return len(p), nil
}
