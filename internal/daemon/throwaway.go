package daemon

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/dovholuknf/atrium/internal/api"
)

// A session with nowhere to live.
//
// The case this is for is wanting to try something without first deciding
// where it belongs. The alternative is making a folder, remembering what it
// was for, and finding it three weeks later beside a card that has sat in
// `finished` ever since. Small friction, paid every single time, and that is
// the kind that decides whether a tool gets reached for at all.
//
// "GONE FOREVER" IS THREE DELETIONS and the third is the one that gets
// forgotten: the directory, the card, and the conversation. Claude Code keys
// its transcripts on the working directory, so deleting the directory alone
// leaves a transcript orphaned under an encoded name for a path that no longer
// exists, one per throwaway, accumulating forever. A throwaway that leaves its
// transcript behind is not a throwaway.
//
// AND IT ALL HAPPENS AFTER THE PROCESS IS GONE. The runner's working directory
// IS the directory, and Windows will not let a live process have its cwd
// removed. So this is called from `awaitExit`, where the process has already
// been waited on, rather than from wherever the exit was asked for. An
// implementation that deletes where the exit was requested appears to work and
// leaves the directory behind.

// throwawayPattern is what a temporary directory is called, so that one found
// on disk later says what it was.
const throwawayPattern = "atrium-throwaway-"

// makeThrowawayDir is the launching half of the feature, in full.
func makeThrowawayDir() (string, error) {
	dir, err := os.MkdirTemp("", throwawayPattern)
	if err != nil {
		return "", fmt.Errorf("could not make a temporary directory: %w", err)
	}
	return filepath.ToSlash(dir), nil
}

// throwawayWhy is what the card says about itself, so that "gone forever" is
// never a surprise. Written only where the operator typed nothing.
const throwawayWhy = "temporary. this directory, this card and the conversation " +
	"are deleted when the session ends. promote it to keep the work."

// endThrowaway is what the end of a throwaway session means.
//
// Called for every card as its runner exits, and returns immediately for the
// cards that are not throwaways, which is nearly all of them. Cheap enough to
// ask every time and much safer than a caller remembering to.
//
// AN EXIT IS THREE DIFFERENT EVENTS: `atrium finish`, the process ending on
// its own, and the operator killing it. All three arrive here, because all
// three go through `cmd.Wait`. A crash cleans up as thoroughly as a tidy
// finish for the same reason.
func (d *Daemon) endThrowaway(taskID string) {
	t, err := d.st.Get(taskID)
	if err != nil || !t.Throwaway {
		return
	}
	// Somebody decided this mattered while it was running. The directory moves
	// instead of going, which is the whole reason it is safe to start work in
	// a throwaway before knowing whether the work matters.
	if t.PromoteTo != "" {
		d.promoteThrowaway(t.ID, t.Worktree, filepath.FromSlash(t.PromoteTo))
		return
	}
	d.discardThrowaway(t.ID, t.Worktree)
}

// promoteThrowaway carries out a promote that had to wait for the session.
//
// A FAILED MOVE STOPS THE DELETE ANYWAY. The directory is somewhere it was not
// meant to stay and that is a great deal better than gone: the card keeps its
// old directory, stops being temporary, and says what happened, which leaves
// the operator with the work and a sentence explaining where it is.
func (d *Daemon) promoteThrowaway(taskID, from, to string) {
	if err := api.MoveWorktree(from, to); err != nil {
		log.Printf("[atrium] could not promote %s to %s: %v", taskID, to, err)
		if err := d.st.Promoted(taskID, from); err != nil {
			log.Printf("[atrium] clear the throwaway flag on %s: %v", taskID, err)
		}
		if err := d.st.SetWhy(taskID,
			"could not be moved to "+to+", so it stays in "+from); err != nil {
			log.Printf("[atrium] note the failed promote on %s: %v", taskID, err)
		}
		d.publishTask(taskID)
		return
	}
	if err := d.st.Promoted(taskID, filepath.ToSlash(to)); err != nil {
		log.Printf("[atrium] record the promote of %s: %v", taskID, err)
	}
	if err := d.st.SetWhy(taskID, ""); err != nil {
		log.Printf("[atrium] clear the throwaway note on %s: %v", taskID, err)
	}
	log.Printf("[atrium] promoted %s to %s", taskID, to)
	d.publishTask(taskID)
}

// discardThrowaway performs the three deletions.
//
// The directory first, because it is the one that can fail: a file still open
// in it leaves everything else describing something that is still there. The
// card last, because it is the only remaining way to find the other two.
func (d *Daemon) discardThrowaway(taskID, dir string) {
	// A shell opened beside the runner holds the directory open, and on
	// Windows that is enough to refuse the removal. Closed here for the same
	// reason `deleteTask` closes one before forgetting a card.
	d.CloseShell(taskID)
	if isThrowawayDir(dir) {
		if err := api.ForgetTranscripts(dir); err != nil {
			log.Printf("[atrium] could not delete the conversations for %s: %v", taskID, err)
		}
		if err := os.RemoveAll(filepath.FromSlash(dir)); err != nil {
			log.Printf("[atrium] could not delete %s: %v", dir, err)
			return
		}
	} else if dir != "" {
		// Refused rather than logged and done anyway. This deletes a directory
		// tree outright, and a card that says it is temporary while pointing
		// somewhere real is a bug whose consequence would be somebody's work.
		log.Printf("[atrium] %s is marked temporary but %s is not a temporary directory. "+
			"leaving it alone", taskID, dir)
		return
	}
	if err := d.st.Forget(taskID); err != nil {
		log.Printf("[atrium] could not delete the throwaway card %s: %v", taskID, err)
		return
	}
	d.ap.Broadcast("task-removed", map[string]string{"id": taskID})
	log.Printf("[atrium] threw away %s and its directory", taskID)
}

// isThrowawayDir is the guard on the delete: a directory atrium made, in the
// place the operating system keeps temporary ones, named the way this file
// names them.
func isThrowawayDir(dir string) bool {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return false
	}
	leaf := filepath.Base(filepath.FromSlash(dir))
	if !strings.HasPrefix(leaf, throwawayPattern) {
		return false
	}
	parent := filepath.Clean(filepath.Dir(filepath.FromSlash(dir)))
	return strings.EqualFold(parent, filepath.Clean(os.TempDir()))
}

// sweepThrowaways is the backstop, and it is the case that will happen.
//
// `awaitExit` cleans up after every runner it waited on, and a daemon that was
// killed waited on none of them. So a throwaway whose directory and card are
// still there at start up is one whose session ended when the daemon did, and
// it is finished either way: its directory cannot be reopened into, because
// `reopenWanted` refuses a throwaway, and a card nothing can start again is a
// card with nothing left to do.
func (d *Daemon) sweepThrowaways() {
	tasks, err := d.st.List()
	if err != nil {
		log.Printf("[atrium] could not look for throwaway cards: %v", err)
		return
	}
	for _, t := range tasks {
		if !t.Throwaway || d.sup.get(t.ID) != nil {
			continue
		}
		d.endThrowaway(t.ID)
	}
}
