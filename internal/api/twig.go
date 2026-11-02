package api

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/dovholuknf/atrium/internal/base/nowindow"
	"github.com/dovholuknf/atrium/internal/base/safepath"
)

// TWIG: `gwt twig <name>`, from `atrium twig`. POST /v1/new with `twig`, the worktree the work is found in. The new
// branch starts at that worktree's HEAD (its local HEAD, never origin's), its tracked changes are carried over as a
// binary patch applied unstaged, and its untracked files are copied. A patch that does not apply is left beside the
// new worktree and named. Then the setup hook runs and a card starts, as for new work. A name that is already a branch
// or a worktree is refused, since a twig that answered an old worktree would have dropped the edits it was asked to
// carry.

// twigSource is the top of the worktree a twig starts from and its HEAD. It must sit under an atrium path or an
// open_paths folder, since its files are read.
func (s *Server) twigSource(ctx context.Context, dir string) (top, head string, status int, err error) {
	if !filepath.IsAbs(filepath.FromSlash(dir)) {
		return "", "", http.StatusBadRequest, errors.New("twig is the absolute path of the worktree to start from")
	}
	real := ""
	for _, root := range s.openRoots() {
		if r, rerr := safepath.Contained(root, filepath.FromSlash(dir)); rerr == nil {
			real = r
			break
		}
	}
	if real == "" {
		return "", "", http.StatusForbidden, errors.New(filepath.ToSlash(dir) +
			" is not under an atrium path or an open_paths folder, so it cannot be twigged from")
	}
	out, err := gitIn(ctx, real, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", "", http.StatusBadRequest, errors.New(filepath.ToSlash(dir) + " is not a git worktree")
	}
	top = strings.TrimSpace(out)
	out, err = gitIn(ctx, top, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return "", "", http.StatusBadRequest, errors.New(top + " has no commit to branch from")
	}
	return top, strings.TrimSpace(out), http.StatusOK, nil
}

// twigBase refuses a name that is already a branch or a worktree in repo, and fetches head from src when repo does
// not have it, which is so when src is a worktree of another clone.
func twigBase(ctx context.Context, repo, branch, src, head string) (int, error) {
	for _, wt := range worktreesOf(repo) {
		if strings.EqualFold(wt.Branch, branch) {
			return http.StatusConflict, errors.New(wt.Path + " already has the branch " + branch +
				". a twig makes a new branch: pick another name")
		}
	}
	if branchExists(ctx, repo, branch) || remoteBranch(ctx, repo, branch) != "" {
		return http.StatusConflict, errors.New("the branch " + branch + " already exists in " + repo +
			". a twig makes a new branch: pick another name")
	}
	if _, err := gitIn(ctx, repo, "rev-parse", "--verify", "--quiet", head+"^{commit}"); err == nil {
		return http.StatusOK, nil
	}
	if _, err := gitIn(ctx, repo, "fetch", "--no-tags", filepath.FromSlash(src), "HEAD"); err != nil {
		return http.StatusBadRequest, errors.New("could not fetch " + src + "'s HEAD into " + repo + ": " + err.Error())
	}
	if _, err := gitIn(ctx, repo, "rev-parse", "--verify", "--quiet", head+"^{commit}"); err != nil {
		return http.StatusConflict, errors.New(src + " moved its HEAD while it was twigged. try again")
	}
	return http.StatusOK, nil
}

// carryTwig brings src's uncommitted work to dest: the tracked diff applied unstaged, then the untracked files. A
// patch that does not apply is written to `<dest>.twig.patch` and answered as patch, with why as failed. An
// untracked file that does not copy is named in failed and the rest still copy.
func carryTwig(ctx context.Context, src, dest string) (patch, failed string) {
	diff, err := gitOut(ctx, src, nil, "diff", "HEAD", "--binary")
	if err != nil {
		return "", "the diff of " + src + " was not read: " + err.Error()
	}
	if len(diff) > 0 {
		if _, err := gitOut(ctx, dest, diff, "apply", "--binary", "--whitespace=nowarn"); err != nil {
			patch = filepath.ToSlash(filepath.FromSlash(dest)) + ".twig.patch"
			if werr := os.WriteFile(filepath.FromSlash(patch), diff, 0o644); werr != nil {
				return "", "the patch did not apply (" + err.Error() + ") and was not saved: " + werr.Error()
			}
			failed = "the patch did not apply: " + err.Error()
		}
	}
	list, err := gitOut(ctx, src, nil, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return patch, strings.TrimSpace(failed + " the untracked files were not listed: " + err.Error())
	}
	for _, rel := range strings.Split(string(list), "\x00") {
		if rel == "" {
			continue
		}
		if err := copyUntracked(filepath.Join(filepath.FromSlash(src), filepath.FromSlash(rel)),
			filepath.Join(filepath.FromSlash(dest), filepath.FromSlash(rel))); err != nil {
			failed = strings.TrimSpace(failed + " " + rel + " was not copied: " + err.Error())
		}
	}
	return patch, failed
}

// gitOut runs git in dir with stdin and answers its stdout alone, so a warning on stderr is not taken as the patch.
func gitOut(ctx context.Context, dir string, stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = filepath.FromSlash(dir)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	nowindow.Hide(cmd)
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(tailLines(stderr.String()))
		if msg == "" {
			msg = err.Error()
		}
		return out, errors.New("git " + args[0] + ": " + msg)
	}
	return out, nil
}

func copyUntracked(from, to string) error {
	info, err := os.Lstat(from)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(from)
		if err != nil {
			return err
		}
		return os.Symlink(target, to)
	}
	if info.IsDir() {
		return copyTree(from, to)
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	b, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	return os.WriteFile(to, b, info.Mode().Perm())
}
