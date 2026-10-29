package daemon

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/store"
)

// Clean up throwaways after their processes exit: remove the directory,
// card, and Claude Code transcripts, which are stored separately. Run from
// awaitExit because Windows cannot remove a live process's working directory.
// Promotion replaces cleanup when the operator keeps the work.

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

// endThrowaway cleans up an exiting runner and returns immediately for
// ordinary cards. All exit paths reach it through cmd.Wait, including
// normal completion, termination, and crashes.
func (d *Daemon) endThrowaway(taskID string) {
	t, err := d.st.Get(taskID)
	if err != nil || !t.Throwaway {
		return
	}
	// Somebody decided this mattered while it was running. The directory moves
	// instead of going, which is the whole reason it is safe to start work in
	// a throwaway before knowing whether the work matters.
	if row, ok := d.homeOf(t.ID); ok && row.Kind == store.CardHomeThrowaway {
		d.endHomeThrowaway(t, row)
		return
	}
	if t.PromoteTo != "" {
		d.promoteThrowaway(t.ID, t.Worktree, filepath.FromSlash(t.PromoteTo))
		return
	}
	d.discardThrowaway(t.ID, t.Worktree)
}

// promoteThrowaway performs a deferred move. If it fails, keep the original
// path and clear the temporary flag so cleanup cannot delete the work.
// Record the failure on the card.
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

// discardThrowaway removes the directory, transcripts, then card. Stop if
// the directory cannot be removed; keep the card until cleanup finishes.
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

// sweepThrowaways cleans up leftovers after a daemon exit that bypassed
// awaitExit. Throwaways are excluded from reopening and need cleanup at startup.
func (d *Daemon) sweepThrowaways() {
	tasks, err := d.st.List()
	if err != nil {
		log.Printf("[atrium] could not look for throwaway cards: %v", err)
		return
	}
	for _, t := range tasks {
		if !t.Throwaway || d.sup.Get(t.ID) != nil {
			continue
		}
		d.endThrowaway(t.ID)
	}
}

// endHomeThrowaway is the end of a throwaway that lives in a home. Promoted, the home is kept where it is and the ledger
// says so: nothing moves any more. Otherwise the home goes whole, then the transcripts and the card.
func (d *Daemon) endHomeThrowaway(t *store.Task, row store.CardHome) {
	if t.PromoteTo != "" {
		if err := d.st.Promoted(t.ID, t.Worktree); err != nil {
			log.Printf("[atrium] record the promote of %s: %v", t.ID, err)
		}
		_ = d.st.CardHomeSet(row.Path, store.CardHomeKept, "promoted, so it was kept where it is", row.Owed)
		if err := d.st.SetWhy(t.ID, ""); err != nil {
			log.Printf("[atrium] clear the throwaway note on %s: %v", t.ID, err)
		}
		d.publishTask(t.ID)
		return
	}
	d.CloseShell(t.ID)
	if err := api.ForgetTranscripts(t.Worktree); err != nil {
		log.Printf("[atrium] could not delete the conversations for %s: %v", t.ID, err)
	}
	if removed, kept, _ := d.reclaimHome(t, row, true); !removed {
		log.Printf("[atrium] could not delete the home of throwaway %s: %s", t.ID, kept)
		return
	}
	if err := d.st.Forget(t.ID); err != nil {
		log.Printf("[atrium] could not delete the throwaway card %s: %v", t.ID, err)
		return
	}
	d.ap.Broadcast("task-removed", map[string]string{"id": t.ID})
	log.Printf("[atrium] threw away %s and its home", t.ID)
}
