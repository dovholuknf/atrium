package daemon

import (
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// A supervised worker whose worktree has been removed is asked to leave.
//
// Removing a worktree by hand while its runner is still up leaves the directory
// held by the runner's own process, so git unregisters the worktree, deletes
// what it can, and fails on the rest. The runner sits at a prompt on a card in
// `done` on purpose (a director can send it back, and a say can reach it), so
// being done is never what ends it. Having nowhere to work is.
//
// ONLY `atrium:subagent` CARDS, and only runners in the supervisor's `runners`
// map. A human's terminal in a scratch directory, a fixture, or a shell is
// never looked at. See docs/worktree-gone-design.md.

// windDownGrace is how long a runner asked to leave is given, the same as
// shutdown gives each one.
const windDownGrace = 10 * time.Second

// goneWatch is what the reaper remembers between ticks, in memory only. A
// restart forgets it and costs one more tick.
type goneWatch struct {
	mu sync.Mutex
	// seen is the path each card read as gone on the last tick.
	seen map[string]string
	// leaving holds the cards already being wound down, so a later tick does not
	// start a second one for the same runner.
	leaving map[string]bool
	// ask is how a runner is asked to leave. Nil means `windDown`. A field so a
	// test can watch the ask without a real terminal.
	ask func(r *runner)
}

// worktreeGone says whether dir is definitely no longer a working checkout.
//
// Only a definite "does not exist" counts. A permission failure or a sharing
// violation says nothing about whether the directory is there, and ending a
// session on a guess is the one mistake this cannot afford.
func worktreeGone(dir string) bool {
	if dir == "" {
		return false
	}
	if _, err := os.Stat(dir); err != nil {
		return os.IsNotExist(err)
	}
	// A worktree's .git is a file and a checkout's is a directory: either counts.
	// Neither is what git leaves after unregistering a worktree it could not
	// finish deleting.
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return err != nil && os.IsNotExist(err)
}

// runnerDir is the directory to ask about: the one the process was launched in,
// which is exactly the one it holds, else the one the card last reported.
func runnerDir(r *runner, t *store.Task) string {
	if r.spec != nil && r.spec.cwd != "" {
		return r.spec.cwd
	}
	return filepath.FromSlash(t.Worktree)
}

// reapGoneWorktrees runs once a tick. A directory has to read as gone on two
// consecutive ticks before anything is asked of the runner.
func (d *Daemon) reapGoneWorktrees() {
	w := &d.gone
	w.mu.Lock()
	if w.seen == nil {
		w.seen = map[string]string{}
		w.leaving = map[string]bool{}
	}
	w.mu.Unlock()

	live := map[string]bool{}
	for _, r := range d.sup.all() {
		live[r.taskID] = true
		t, err := d.st.Get(r.taskID)
		if err != nil || !hasTag(t.Tags, SubagentTag) {
			continue
		}
		dir := runnerDir(r, t)

		w.mu.Lock()
		if w.leaving[r.taskID] {
			w.mu.Unlock()
			continue
		}
		if !worktreeGone(dir) {
			delete(w.seen, r.taskID)
			w.mu.Unlock()
			continue
		}
		if w.seen[r.taskID] != dir {
			w.seen[r.taskID] = dir
			w.mu.Unlock()
			continue
		}
		w.leaving[r.taskID] = true
		delete(w.seen, r.taskID)
		w.mu.Unlock()

		if err := d.st.AppendEvent(t.ID, store.EventNotified, map[string]any{
			"by": "reaper", "detected": "its worktree was removed", "path": dir,
		}); err != nil {
			log.Printf("[atrium] record worktree removal for %s: %v", t.ID, err)
		}
		log.Printf("[atrium] %s: its worktree %s was removed, asking the runner to leave",
			t.DisplayTitle(), dir)
		go func(r *runner) {
			if w.ask != nil {
				w.ask(r)
			} else {
				windDown(r, windDownGrace, d.exitKeysFor(r.taskID))
			}
			w.mu.Lock()
			delete(w.leaving, r.taskID)
			w.mu.Unlock()
		}(r)
	}

	// Runners that have gone leave nothing behind.
	w.mu.Lock()
	for id := range w.seen {
		if !live[id] {
			delete(w.seen, id)
		}
	}
	w.mu.Unlock()
}
