package daemon

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// Culling a finished worker: asking it to leave, then removing its worktree and
// its branch. Backlog-2 item 36.
//
// A merged worker used to sit idle at needs-input, holding a launch-cap slot,
// until somebody exited it and removed its worktree and branch by hand. On
// 2026-09-28 four of them filled the cap and a new launch was refused.
//
// WHO DECIDES. The caller of `atrium_cull`, which is whoever accepted the work:
// the orchestrator, or the merger once the orchestrator has said so. Not the
// worker. A worker's own done report means it thinks it is finished, and a
// worker that culled itself would remove the worktree its reviewer has not read
// yet. So the tool is the acceptance, and the room adds the one check it can
// make on its own: the branch is merged. A merge is not an acceptance: a branch
// can land and still be sent back, and the worker is the cheapest place to make
// the fix while its conversation is still warm. A hook on the merge was first
// turned down for that reason, and then built with a grace period in which to send
// the work back: a merge only MARKS a worker, and the sweep calls this when the
// time comes, with every check re-run. See mergedcull.go.
//
// WHAT IT WILL NOT DO, each checked here on the room rather than trusted from
// the caller:
//
//   - Touch a card not tagged `atrium:subagent`. An orchestrator, the merger or
//     a human's own terminal is never culled, whoever asks.
//   - Exit anything whose branch is not merged into the target branch. The
//     whole cull is refused, so the worker is still there to be told why.
//   - Remove a worktree with uncommitted or untracked changes. The worker is
//     still asked to leave, since its work is merged and the slot is what was
//     wanted, and the worktree and branch are kept and the answer says why.
//     The one file skipped is the BRIEF.md atrium itself wrote there.
//   - Remove the main checkout, or delete main, master or the target branch.
//   - Delete a branch while its worktree is still there.
//
// `git worktree remove` is run without `--force`, so git refuses on its own a
// worktree these checks somehow passed as clean.

// SubagentTag marks a worker an orchestrator launched to do one piece of work.
// The same string as `link.SubagentTag`, which the launch cap counts. The
// launcher puts it on, and only a card carrying it can be culled.
const SubagentTag = "atrium:subagent"

// errCullNewContext is the refusal while a new context is running on the worker.
var errCullNewContext = errors.New("a new context is running on it, try again")

// DefaultCullInto is the branch a worker's branch has to be merged into.
const DefaultCullInto = "claude/main"

// briefFile is what atrium writes into a launched worker's directory. Left
// untracked by design, so it is the one untracked file a cull ignores.
const briefFile = "BRIEF.md"

// cullGitTimeout bounds each git command a cull runs.
const cullGitTimeout = 20 * time.Second

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
	// BriefRemoved and ScratchRemoved say what became of the BRIEF.md atrium wrote and of a directory that was
	// not a git worktree. See reclaim.go.
	BriefRemoved   bool   `json:"brief_removed"`
	ScratchRemoved bool   `json:"scratch_removed"`
	Kept           string `json:"kept,omitempty"`
}

// Cull asks a merged worker to leave and removes its worktree and branch. See
// the top of this file for what it refuses.
func (d *Daemon) Cull(taskID, into string) (*CullResult, error) {
	return d.CullProved(taskID, into, "")
}

// CullProved is Cull with a merge proof made elsewhere. `tip` is the commit the
// area branch was checked to contain, and this room's worktree has to be sitting
// on exactly it, because the proof was made where the area branch lives. Empty
// means the proof is made here, against a branch of this repository.
func (d *Daemon) CullProved(taskID, into, tip string) (*CullResult, error) {
	t, err := d.st.Get(taskID)
	if err != nil {
		return nil, err
	}
	into = strings.TrimSpace(into)
	if into == "" {
		into = DefaultCullInto
	}
	if !d.isWorker(t) {
		return nil, fmt.Errorf("%s is not tagged %s, so it is not a worker atrium culls. "+
			"exit it with atrium_exit if it should go", t.DisplayTitle(), SubagentTag)
	}
	// A new context in progress is not culled under: the run would fail on a closed
	// terminal after the capture, and nothing would be cleared. Try again after it.
	if d.nctx.holding(taskID) {
		return nil, fmt.Errorf("%s was not culled: %w", t.DisplayTitle(), errCullNewContext)
	}
	if why := d.reclaimBlock(t); why != "" {
		return nil, fmt.Errorf("%s was not culled: %s", t.DisplayTitle(), why)
	}
	if strings.TrimSpace(t.Worktree) == "" {
		return nil, fmt.Errorf("%s has no directory recorded, so atrium cannot check its "+
			"branch is merged", t.DisplayTitle())
	}

	// Held so a restart or a launch onto this card cannot start a runner in the
	// worktree while it is being removed. See RestartRunner.
	unlock := d.launching.lock(launchKeys(taskID, t.ResumeID)...)
	defer unlock()

	// A directory that is not a git checkout is the scratch directory a launcher made. There is no branch of its
	// own to prove merged, so the launcher's call is the claim. See reclaim.go.
	if fi, err := os.Stat(filepath.FromSlash(t.Worktree)); err == nil && fi.IsDir() && !hasDotGit(t.Worktree) &&
		strings.TrimSpace(tip) == "" {
		return d.reclaimPlain(t, into)
	}

	res := &CullResult{Card: taskID, Into: into, Worktree: t.Worktree}
	plan, err := inspectCullProved(t.Worktree, into, tip)
	if err != nil {
		return nil, fmt.Errorf("%s was not culled: %w", t.DisplayTitle(), err)
	}
	res.Branch = plan.branch
	if plan.moved != "" {
		return nil, fmt.Errorf("%s was not culled: new commits since the merged branch was fetched: %s vs %s",
			t.DisplayTitle(), shortRef(plan.moved), shortRef(plan.tip))
	}
	if !plan.merged {
		return nil, fmt.Errorf("%s was not culled: its branch %s is not merged into %s",
			t.DisplayTitle(), plan.branch, into)
	}

	if err := d.leaveForCull(t, res); err != nil {
		return nil, err
	}

	// Again, after the exit: a runner writes on its way out.
	plan, err = inspectCullProved(t.Worktree, into, tip)
	if err != nil {
		res.Kept = "the worktree could not be read after the exit: " + err.Error()
		return d.culled(t, res), nil
	}
	if len(plan.dirty) > 0 {
		res.Kept = "the worktree has uncommitted changes, so it and its branch were kept: " +
			strings.Join(plan.dirty, ", ")
		return d.culled(t, res), nil
	}
	if !plan.merged || plan.moved != "" {
		res.Kept = "its branch moved on the way out and is no longer merged, so it and the worktree were kept"
		return d.culled(t, res), nil
	}
	if err := removeWorktree(plan); err != nil {
		res.Kept = err.Error()
		return d.culled(t, res), nil
	}
	res.WorktreeRemoved, res.BriefRemoved = true, true
	if err := deleteMergedBranch(plan, into); err != nil {
		res.Kept = err.Error()
		return d.culled(t, res), nil
	}
	res.BranchDeleted = true
	return d.culled(t, res), nil
}

// leaveForCull asks a worker to leave first. Its process holds the worktree as its directory, and on Windows a
// directory in use cannot be removed.
func (d *Daemon) leaveForCull(t *store.Task, res *CullResult) error {
	taskID := t.ID
	if d.sup.get(taskID) != nil {
		if err := d.StopRunnerBy(taskID, "atrium (cull)"); err != nil {
			return err
		}
		if !d.waitRunnerGone(taskID, restartGoneWait) {
			return fmt.Errorf("%s did not stop in time, so nothing was removed. try again", t.DisplayTitle())
		}
		res.Exited = true
	} else if t.PID > 0 && !finishedStatus(t.Status) {
		return fmt.Errorf("%s is still live and atrium does not own its terminal, so it "+
			"cannot be asked to leave from here and nothing was removed", t.DisplayTitle())
	}
	return nil
}

// culled logs what a cull did, and hands the result back. Reaching here means
// the branch was proved merged and the worker was asked to leave, which is the
// acceptance: the work item closes as accepted, even when a dirty worktree was
// kept.
func (d *Daemon) culled(t *store.Task, res *CullResult) *CullResult {
	log.Printf("[atrium] culled %s: exited=%v worktree removed=%v branch %s deleted=%v %s",
		t.DisplayTitle(), res.Exited, res.WorktreeRemoved, res.Branch, res.BranchDeleted, res.Kept)
	if err := d.st.AcceptMerged(t.ID); err != nil {
		log.Printf("[atrium] could not close %s as accepted: %v", t.DisplayTitle(), err)
	}
	// Off the board too. Not recorded as dead, which would be a death in the
	// ledger for accepted work: the card keeps its status and its history.
	if err := d.st.ArchiveCulled(t.ID, "culled: its branch merged and it was asked to leave"); err != nil {
		log.Printf("[atrium] could not archive %s: %v", t.DisplayTitle(), err)
	} else {
		d.ap.Broadcast("task-removed", nil)
	}
	return res
}

func shortRef(s string) string {
	if len(s) > 10 {
		return s[:10]
	}
	return s
}

func finishedStatus(s string) bool {
	switch s {
	case store.StatusDone, store.StatusDead, store.StatusShelved:
		return true
	}
	return false
}

// cullPlan is what git says about a worker's worktree.
type cullPlan struct {
	wt     string
	common string
	branch string
	merged bool
	// tip is the commit a proof made elsewhere named, and moved is the commit
	// the branch is on when it is not that one. Both empty for a proof made here.
	tip, moved string
	// dirty lists the changes git reports, minus atrium's own BRIEF.md.
	dirty []string
	// brief is BRIEF.md sitting untracked in the worktree, removed before git
	// removes the worktree so git does not refuse over it.
	brief bool
}

// inspectCull reads the worktree's git state and refuses anything a cull must
// never remove. Merged and dirty are reported, not refused, so the caller
// decides what each means.
func inspectCull(wt, into string) (*cullPlan, error) { return inspectCullProved(wt, into, "") }

// inspectCullProved is inspectCull where the merge was proved elsewhere. With a
// tip, this repository has no `into` to check against, so merged means the
// worktree is sitting on exactly that commit, and `moved` names the commit it is
// on when it is not.
func inspectCullProved(wt, into, tip string) (*cullPlan, error) {
	wt = filepath.FromSlash(wt)
	if fi, err := os.Stat(wt); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("its directory %s is not there, so atrium cannot check its branch", wt)
	}
	p := &cullPlan{wt: wt}
	gitDir, err := gitIn(wt, "rev-parse", "--path-format=absolute", "--git-dir")
	if err != nil {
		return nil, fmt.Errorf("%s is not a git worktree: %w", wt, err)
	}
	p.common, err = gitIn(wt, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return nil, err
	}
	if filepath.Clean(gitDir) == filepath.Clean(p.common) {
		return nil, fmt.Errorf("%s is the repository's main checkout, not a worktree of its own", wt)
	}
	p.branch, err = gitIn(wt, "symbolic-ref", "--short", "-q", "HEAD")
	if err != nil || p.branch == "" {
		return nil, fmt.Errorf("%s is not on a branch", wt)
	}
	if protectedBranch(p.branch, into) {
		return nil, fmt.Errorf("%s is on %s, which a cull never deletes", wt, p.branch)
	}
	if tip = strings.TrimSpace(tip); tip != "" {
		p.tip, err = resolveTip(wt, tip)
		if err != nil {
			return nil, err
		}
		head, err := gitIn(wt, "rev-parse", "--verify", "-q", "refs/heads/"+p.branch)
		if err != nil {
			return nil, fmt.Errorf("could not read branch %s: %w", p.branch, err)
		}
		if head == p.tip {
			p.merged = true
		} else {
			p.moved = head
		}
	} else {
		if _, err := gitIn(wt, "rev-parse", "--verify", "-q", "refs/heads/"+into); err != nil {
			return nil, fmt.Errorf("there is no branch %s to check it against", into)
		}
		p.merged, err = isAncestor(wt, "refs/heads/"+p.branch, "refs/heads/"+into)
		if err != nil {
			return nil, err
		}
	}
	status, err := gitIn(wt, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(status, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		if line == "?? "+briefFile {
			p.brief = true
			continue
		}
		p.dirty = append(p.dirty, strings.TrimSpace(line))
	}
	return p, nil
}

// resolveTip turns the tip a caller typed, possibly an abbreviated sha, into the
// full commit sha in this worktree's repository, so it compares like for like.
// A tip that is not a commit here is refused by name. That is not the same as the
// branch having moved past a known tip. Only a hex sha is accepted, because a
// ref such as HEAD would resolve to the branch head and always match.
func resolveTip(wt, tip string) (string, error) {
	tip = strings.ToLower(tip)
	if !isHexSha(tip) {
		return "", fmt.Errorf("the tip %q is not a sha. give the 7 to 40 hex characters of the commit that was checked", tip)
	}
	full, err := gitIn(wt, "rev-parse", "--verify", tip+"^{commit}")
	if err != nil {
		if strings.Contains(err.Error(), "ambiguous") {
			return "", fmt.Errorf("the tip %s is ambiguous here, matching more than one object. give more of the sha", tip)
		}
		return "", fmt.Errorf("the tip %s is not a commit in this room's repository. it was never fetched here, "+
			"or it is not a commit", tip)
	}
	return full, nil
}

// isHexSha is 7 to 40 lowercase hex characters.
func isHexSha(s string) bool {
	if len(s) < 7 || len(s) > 40 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// protectedBranch is a branch a cull never deletes, whatever the worktree says.
func protectedBranch(branch, into string) bool {
	switch strings.ToLower(branch) {
	case "main", "master", "claude/main", strings.ToLower(into):
		return true
	}
	return false
}

// isAncestor is `git merge-base --is-ancestor`, which answers 1 for "no" and
// anything else for a failure.
func isAncestor(dir, commit, of string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cullGitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "merge-base", "--is-ancestor", commit, of)
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return true, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("git could not tell whether %s is merged: %s", commit,
		strings.TrimSpace(string(out)))
}

// removeWorktree removes atrium's BRIEF.md and then the worktree, without
// `--force`, from the repository rather than from inside the worktree.
func removeWorktree(p *cullPlan) error {
	if p.brief {
		if err := os.Remove(filepath.Join(p.wt, briefFile)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("could not remove %s, so the worktree was kept: %v", briefFile, err)
		}
	}
	if _, err := gitIn(p.common, "--git-dir="+p.common, "worktree", "remove", p.wt); err != nil {
		return fmt.Errorf("git kept the worktree: %v", err)
	}
	return nil
}

// deleteMergedBranch deletes the branch after checking once more that it is
// merged. `-D` rather than `-d`, because `-d` checks against whatever HEAD the
// main checkout is on rather than against the branch this was checked against.
func deleteMergedBranch(p *cullPlan, into string) error {
	var (
		merged bool
		err    error
	)
	if p.tip != "" {
		// Nothing here to check `into` against. The branch has to still be the
		// commit the proof was made for.
		var head string
		head, err = gitIn(p.common, "--git-dir="+p.common, "rev-parse", "--verify", "-q", "refs/heads/"+p.branch)
		merged = err == nil && head == p.tip
	} else {
		merged, err = isAncestor(p.common, "refs/heads/"+p.branch, "refs/heads/"+into)
	}
	if err != nil || !merged {
		return fmt.Errorf("the worktree was removed and branch %s kept, because it no longer "+
			"reads as merged into %s", p.branch, into)
	}
	if _, err := gitIn(p.common, "--git-dir="+p.common, "branch", "-D", p.branch); err != nil {
		return fmt.Errorf("the worktree was removed and git kept branch %s: %v", p.branch, err)
	}
	return nil
}

// gitIn runs git in dir and returns its trimmed output, or git's own reason.
func gitIn(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cullGitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	hideWindow(cmd)
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
