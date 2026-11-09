package daemon

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/base/gitexec"
	"github.com/dovholuknf/atrium/internal/base/safepath"
	"github.com/dovholuknf/atrium/internal/daemon/home"
	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"github.com/dovholuknf/atrium/internal/small"
	"github.com/dovholuknf/atrium/internal/store"
)

// CLONING A CARD: a new card on this room, in a new worktree from the original's commit, with its uncommitted changes
// and, on claude, a fork of its conversation. The original's folder and conversation are only read.

// cloneSource is what the board's clone form is filled from, `GET /v1/tasks/{id}/clone`. NoFork is why the clone
// cannot fork the conversation, "" when it can.
type cloneSource struct {
	Repo   string   `json:"repo"`
	Head   string   `json:"head"`
	Dirty  []string `json:"dirty"`
	Brief  string   `json:"brief"`
	NoFork string   `json:"no_fork,omitempty"`
}

// cloneRefusal is why a card cannot be cloned, nil when it can.
func cloneRefusal(t *store.Task) error {
	if t.Worktree == "" {
		return errors.New("this card has no folder to copy")
	}
	if _, err := gitIn(t.Worktree, gitexec.RevParseVerify{Rev: "HEAD"}); err != nil {
		return fmt.Errorf("%s is not a git checkout with a commit: %v", filepath.ToSlash(t.Worktree), err)
	}
	return nil
}

// forkRefusal is why a clone of t cannot fork its conversation, "" when it can. `--fork-session` is claude's.
func (d *Daemon) forkRefusal(t *store.Task) string {
	if h, err := d.st.Harness(t.Runner); err != nil || !shared.IsClaude(h) {
		return "only a claude conversation can be forked for now"
	}
	if !moveConvName.MatchString(d.ConversationOf(t)) {
		return "atrium never learned this session's id, so there is no conversation to fork"
	}
	return ""
}

// cloneRepo is the clone the original's worktree belongs to: its home's, else the folder holding its git common dir.
func (d *Daemon) cloneRepo(t *store.Task) (string, error) {
	if row, ok := d.HomeOf(t.ID); ok && row.Clone != "" {
		return row.Clone, nil
	}
	common, err := gitIn(t.Worktree, gitexec.RevParseGitDirs{Absolute: true, Common: true})
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(filepath.Dir(filepath.Clean(common))), nil
}

// briefOf is the original's BRIEF.md, in its home when it has one, else in its folder.
func (d *Daemon) briefOf(t *store.Task) string {
	dir := t.Worktree
	if row, ok := d.HomeOf(t.ID); ok {
		dir = home.DirsOf(row.Path).Dir
	}
	data, err := os.ReadFile(filepath.Join(filepath.FromSlash(dir), briefFileName))
	if err != nil {
		return ""
	}
	return string(data)
}

// dirtyFiles are the paths changed against HEAD in dir, then the untracked ones.
func dirtyFiles(dir string) ([]string, error) {
	files := []string{}
	for _, op := range []gitexec.Op{gitexec.Diff{Revs: []string{"HEAD"}, NameOnly: true, NoRenames: true}, gitexec.LsFiles{Others: true, ExcludeStandard: true}} {
		out, err := gitIn(dir, op)
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				files = append(files, line)
			}
		}
	}
	return files, nil
}

func (d *Daemon) handleCloneSource(w http.ResponseWriter, r *http.Request) {
	t, err := d.st.Get(r.PathValue("id"))
	if err != nil {
		small.WriteJSONErr(w, http.StatusNotFound, err)
		return
	}
	if err := cloneRefusal(t); err != nil {
		small.WriteJSONErr(w, http.StatusConflict, err)
		return
	}
	src := cloneSource{Brief: d.briefOf(t), NoFork: d.forkRefusal(t)}
	if src.Repo, err = d.cloneRepo(t); err == nil {
		if src.Head, err = gitIn(t.Worktree, gitexec.RevParse{Rev: "HEAD"}); err == nil {
			src.Dirty, err = dirtyFiles(t.Worktree)
		}
	}
	if err != nil {
		small.WriteJSONErr(w, http.StatusInternalServerError, err)
		return
	}
	small.WriteJSON(w, http.StatusOK, src)
}

// cloneCheck refuses a clone launch before anything is made, and answers the original card, nil for no clone.
func (d *Daemon) cloneCheck(req shared.LaunchRequest, h *store.Harness) (*store.Task, error) {
	if req.CloneOf == "" {
		return nil, nil
	}
	if req.Resume != "" || req.TaskID != "" || strings.TrimSpace(req.Cwd) != "" || !home.RepoIsRef(req.Repo) {
		return nil, errors.New("a clone is a new card in a new worktree: send repo and branch, and no cwd, resume or task_id")
	}
	id := req.CloneOf
	if i := strings.IndexByte(id, '~'); i > 0 {
		id = id[i+1:]
	}
	src, err := d.st.Get(id)
	if err != nil {
		return nil, fmt.Errorf("no card %s on this room to clone: %w", req.CloneOf, err)
	}
	if err := cloneRefusal(src); err != nil {
		return nil, err
	}
	if req.CloneFork {
		if why := d.forkRefusal(src); why != "" {
			return nil, errors.New(why)
		}
		if !shared.IsClaude(h) {
			return nil, errors.New("a forked conversation runs on claude")
		}
	}
	return src, nil
}

// cloneInto carries the original's uncommitted changes into the new worktree at cwd and copies its conversation
// where claude looks for it from cwd. Answers the conversation to fork, "" for none.
func (d *Daemon) cloneInto(src *store.Task, cwd, tmp string, carry, fork bool) (string, error) {
	if carry {
		if err := carryChanges(src.Worktree, cwd, tmp); err != nil {
			return "", fmt.Errorf("could not carry the uncommitted changes: %w", err)
		}
	}
	if !fork {
		return "", nil
	}
	conv := d.ConversationOf(src)
	if err := copySession(src.Worktree, cwd, conv); err != nil {
		return "", fmt.Errorf("could not copy the conversation to fork: %w", err)
	}
	return conv, nil
}

// carryChanges applies from's diff against its HEAD to to, and copies its untracked files. tmp holds the patch.
func carryChanges(from, to, tmp string) error {
	patch := filepath.Join(tmp, "clone.patch")
	if _, err := gitIn(from, gitexec.Diff{Revs: []string{"HEAD"}, Binary: true, Output: patch}); err != nil {
		return err
	}
	defer os.Remove(patch)
	if fi, err := os.Stat(patch); err == nil && fi.Size() > 0 {
		if _, err := gitIn(to, gitexec.Apply{Binary: true, Whitespace: "nowarn", Patch: patch}); err != nil {
			return err
		}
	}
	out, err := gitIn(from, gitexec.LsFiles{Others: true, ExcludeStandard: true})
	if err != nil {
		return err
	}
	for _, name := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		if name = strings.TrimSpace(name); name != "" {
			if err := copyInside(from, to, name); err != nil {
				return err
			}
		}
	}
	return nil
}

// copyInside copies one file from one root to the same place under another, through safepath on both sides.
func copyInside(fromRoot, toRoot, name string) error {
	src, err := safepath.Contained(fromRoot, filepath.Join(fromRoot, filepath.FromSlash(name)))
	if err != nil {
		return err
	}
	dst, err := safepath.Contained(toRoot, filepath.Join(toRoot, filepath.FromSlash(name)))
	if err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// copySession copies conversation conv's transcript from fromCwd's claude project folder to toCwd's, since
// `claude --resume` finds a conversation by the directory it starts in.
func copySession(fromCwd, toCwd, conv string) error {
	if !moveConvName.MatchString(conv) {
		return fmt.Errorf("%q is not a conversation id", conv)
	}
	from, err := api.ProjectDirFor(fromCwd)
	if err != nil {
		return err
	}
	to, err := api.ProjectDirFor(toCwd)
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(from, conv+".jsonl")); err != nil {
		return fmt.Errorf("its transcript is not on this room: %w", err)
	}
	return copyInside(from, to, conv+".jsonl")
}

// forkArgs is a clone's command line: the resume arguments on conv, `--fork-session`, and the opening prompt, which a
// fork takes and a resume refuses.
func forkArgs(h *store.Harness, conv, prompt string, o launchOptions) ([]string, string, error) {
	args, _, err := runnerArgsWith(h, conv, "", o)
	if err != nil {
		return nil, "", err
	}
	n := len(h.ResumeArgs)
	args = append(append(append([]string{}, args[:n]...), "--fork-session"), args[n:]...)
	logged := shellJoin(append([]string{h.Cmd}, args...))
	if p := strings.TrimSpace(prompt); p != "" {
		if len(h.PromptArgs) == 0 {
			return nil, "", fmt.Errorf("%s has no way to take an opening prompt", h.Label)
		}
		for _, a := range h.PromptArgs {
			args = append(args, strings.ReplaceAll(a, "{prompt}", p))
		}
	}
	return args, logged, nil
}
