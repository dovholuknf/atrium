package daemon

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/dovholuknf/atrium/internal/safepath"
	"github.com/dovholuknf/atrium/internal/store"
)

// How a worker's card ends, and what is taken back when it does. See docs/changes/r-card-reclaim-on-done.md.
//
// TWO STEPS, TWO SPEAKERS, because the worker and its launcher know different things.
//
//  1. THE WORKER SAYS DONE, and the card closes: it goes to `done`, its work item to `reported`, and its runner is
//     asked to leave. That is `atrium_report`, `atrium finish`, and the words the launch text prescribes, an
//     `atrium_say` to the launcher that starts `done <sha>`. All three land in finish(), so they cannot drift.
//  2. THE LAUNCHER SAYS MERGED AND DEPLOYED, and the card is reclaimed: its session, its worktree and branch, its
//     BRIEF.md, its scratch directory and whatever its inventory lists. The card's row, its transcript and its
//     history stay, because cards are durable. The signal is `atrium_cull`, the call that was already the
//     launcher's acceptance. A new verb would be a second way to say the same thing, and a mark on the card would
//     be a claim nobody made: only the launcher knows the work is deployed, so only its call can mean it. Nothing
//     on the room reclaims on a merge alone any more, because a merge is not a deploy. See mergedGrace.
//
// WHAT IS NEVER RECLAIMED, whoever asks and whatever the tags say:
//
//   - A card clint started. Only a card an agent launched, `origin:agent`, is in scope.
//   - A card whose work is not done: ended, dead, blocked, waiting on a question or never reported. Those stay in
//     Terminals for the operator, for good (H4).
//   - A card tagged to stay open or an investigation. See staysOpen.
//   - The main checkout, which inspectCull refuses by itself.
//
// Everything on disk goes by way of safepath, and only what atrium or the card itself made: BRIEF.md, a directory
// that is empty once it is gone, and the git worktree and branch of a card whose directory is one.

// saidDone is the one sentence the launch text prescribes: `done` and the commit. Anything else a worker says to
// its launcher is free text, which is never read as a verdict.
var saidDone = regexp.MustCompile(`(?is)^\s*done\s+([0-9a-f]{7,40})\b(.*)$`)

// doneBySay closes a launched worker's card when what it said to its launcher is `done <sha>`. The words are
// already on their way to the launcher, so no second notice is sent. Best effort, like every hook: a failure is
// logged and the say has already gone.
func (d *Daemon) doneBySay(sender *store.Task, text string) {
	if sender == nil || !agentLaunched(sender) {
		return
	}
	m := saidDone.FindStringSubmatch(text)
	if m == nil || sender.Status == store.StatusDone {
		return
	}
	if _, _, err := d.finish(sender, FinishRequest{
		Agent: sender.WireName, TaskID: sender.ID, Status: ReportDone, SHA: m[1],
		Recap: strings.TrimSpace(m[2]), viaSay: true,
	}); err != nil {
		log.Printf("[atrium] could not close %s on its done say: %v", sender.DisplayTitle(), err)
	}
}

// reclaimBlock is why a card is not reclaimed, or "". The one place H4 and the clint-started rule are enforced, read
// by the explicit cull and by the sweep alike.
func (d *Daemon) reclaimBlock(t *store.Task) string {
	switch {
	case t == nil:
		return "there is no card"
	case !hasTag(t.Tags, OriginAgentTag):
		return "an agent did not launch it, so atrium never reclaims it"
	case staysOpen(t):
		return "it is tagged to stay open or is an investigation. take the tag off to reclaim it"
	case t.Status != store.StatusDone:
		return "its work is not done (" + t.Status + "), so it stays in Terminals"
	}
	w, err := d.st.WorkItem(t.ID)
	if err != nil {
		// A card with no work item has no report to read. The tag rules above already hold.
		return ""
	}
	if w.LastReport == nil || w.LastReport.Status != ReportDone {
		return "its last report was not done, so it stays in Terminals"
	}
	return ""
}

// hasDotGit says whether a directory is a git checkout or worktree of its own.
func hasDotGit(dir string) bool {
	_, err := os.Lstat(filepath.Join(filepath.FromSlash(dir), ".git"))
	return err == nil
}

// reclaimPlain reclaims a worker whose directory is not a git checkout: the scratch directory a launcher made for
// it, holding its BRIEF.md. There is no branch of its own to prove merged. The launcher's call is the claim.
func (d *Daemon) reclaimPlain(t *store.Task, into string) (*CullResult, error) {
	res := &CullResult{Card: t.ID, Into: into, Worktree: t.Worktree}
	if err := d.leaveForCull(t, res); err != nil {
		return nil, err
	}
	res.BriefRemoved, res.ScratchRemoved, res.Kept = reclaimScratch(t.Worktree)
	// Not a worktree or a branch, so those answers are true of nothing to remove.
	res.WorktreeRemoved, res.BranchDeleted = res.ScratchRemoved, true
	return d.culled(t, res), nil
}

// reclaimScratch removes the BRIEF.md atrium wrote into a directory and then the directory, which goes only when
// that leaves it empty. It says what it kept and why.
//
// os.Remove, never RemoveAll: whatever else the worker left there is its output, and a directory that is not empty
// is not atrium's to delete. A directory inside a git checkout keeps its place, and a BRIEF.md that git tracks is
// somebody's file and is left alone.
func reclaimScratch(path string) (briefGone, dirGone bool, kept string) {
	dir := filepath.Clean(filepath.FromSlash(strings.TrimSpace(path)))
	if !filepath.IsAbs(dir) || filepath.Dir(dir) == dir {
		return false, false, "its directory " + path + " is not a path atrium will remove"
	}
	if home, err := os.UserHomeDir(); err == nil && samePath(dir, home) {
		return false, false, "its directory is the home folder, which atrium never removes"
	}
	fi, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return true, true, ""
	}
	if err != nil || !fi.IsDir() {
		return false, false, "its directory " + path + " could not be read as a directory"
	}
	inRepo := false
	if _, err := gitIn(dir, "rev-parse", "--git-dir"); err == nil {
		inRepo = true
	}

	brief := filepath.Join(dir, briefFile)
	briefGone = true
	if bfi, err := os.Lstat(brief); err == nil {
		switch {
		case !bfi.Mode().IsRegular():
			briefGone, kept = false, briefFile+" is not a plain file, so it was kept"
		case inRepo && trackedByGit(dir, briefFile):
			briefGone, kept = false, briefFile+" is tracked by git, so it was kept"
		default:
			real, err := safepath.Contained(dir, briefFile)
			if err == nil {
				err = os.Remove(real)
			}
			if err != nil {
				briefGone, kept = false, "could not remove "+briefFile+": "+err.Error()
			}
		}
	}
	if inRepo {
		if kept == "" {
			kept = "its directory is inside a git checkout, so it was kept"
		}
		return briefGone, false, kept
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return briefGone, false, "its directory is a link, so it was kept"
	}
	if err := os.Remove(dir); err != nil {
		names := leftIn(dir)
		if len(names) > 0 {
			return briefGone, false, "its directory has other files in it (" + strings.Join(names, ", ") +
				"), so it was kept"
		}
		return briefGone, false, "its directory did not remove: " + err.Error()
	}
	return briefGone, true, kept
}

// leftIn names up to five entries of a directory.
func leftIn(dir string) []string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for i, e := range ents {
		if i == 5 {
			out = append(out, fmt.Sprintf("and %d more", len(ents)-5))
			break
		}
		out = append(out, e.Name())
	}
	return out
}

// trackedByGit says whether a file in dir is in the index of the repository holding it.
func trackedByGit(dir, name string) bool {
	_, err := gitIn(dir, "ls-files", "--error-unmatch", "--", name)
	return err == nil
}

// samePath compares two paths the way the platform does, once resolved.
func samePath(a, b string) bool {
	if ra, err := filepath.EvalSymlinks(a); err == nil {
		a = ra
	}
	if rb, err := filepath.EvalSymlinks(b); err == nil {
		b = rb
	}
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}
