package daemon

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/state"
	"github.com/dovholuknf/atrium/internal/store"
)

// The gwt session ledger already tracks every claude session on this machine:
// its worktree, branch, terminal window, pid, current state, and the id needed
// to resume the conversation. Reading it turns those sessions into cards
// without wiring anything into them, which is the only way to adopt a session
// that is already running.
//
// This is strictly a reader. gwt owns those files.

// ledgerStatus maps a gwt state onto a board column.
//
// idle deserves a note: gwt calls a finished turn "idle" and shows it as done,
// but from the board's point of view a session that finished its turn is
// waiting for a human to say something next. That is exactly needs-input, and
// treating it as such is what makes the stack answer "who is waiting longest".
func ledgerStatus(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "needs-input":
		return store.StatusNeedsInput
	case "thinking", "subagent", "sub-done", "startup", "resume", "clear", "compact":
		return store.StatusRunning
	case "idle":
		return store.StatusNeedsInput
	case "ended":
		return store.StatusDead
	default:
		return store.StatusRunning
	}
}

func parseLedgerTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	// gwt writes RFC3339 with an offset.
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Time{}
}

// syncLedger folds one pass of the ledger into the task table.
func (d *Daemon) syncLedger() error {
	sessions, err := state.ReadAll()
	if err != nil {
		return err
	}
	known, err := d.st.ExternalIDs()
	if err != nil {
		return err
	}

	cutoff := d.opts.LedgerMaxAge
	if cutoff <= 0 {
		cutoff = 7 * 24 * time.Hour
	}

	seen := map[string]bool{}
	for _, s := range sessions {
		if s.ID == "" {
			continue
		}

		status := ledgerStatus(s.State)
		// No pid means the process is gone, whatever the last state said.
		alive := s.PID > 0
		if !alive && status != store.StatusDead {
			status = store.StatusDead
		}

		// The ledger keeps every session ever recorded. Adopting all of them
		// buries the board under work that finished months ago. A dead session
		// is adopted only when it is recent, or when it already has a card
		// whose history is worth keeping.
		if !alive && known[s.ID] == "" {
			last := parseLedgerTime(s.LastStateChange)
			if last.IsZero() || time.Since(last) > cutoff {
				continue
			}
		}
		seen[s.ID] = true
		title := s.Label
		if title == "" {
			title = s.Branch
		}

		_, created, err := d.st.UpsertExternal(store.External{
			ID:           s.ID,
			ResumeID:     s.ClaudeSessionID,
			Title:        title,
			Repo:         s.Repo,
			Worktree:     strings.ReplaceAll(s.WorktreePath, `\`, "/"),
			Branch:       s.Branch,
			WindowName:   s.WindowName,
			Runner:       "claude",
			PID:          s.PID,
			Status:       status,
			LastActivity: parseLedgerTime(s.LastStateChange),
		})
		if err != nil {
			return err
		}
		if created {
			log.Printf("[atrium] adopted session %s (%s) from the gwt ledger", s.Branch, s.ID)
		}
	}

	// A session whose ledger entry vanished is gone. Mark it rather than
	// deleting it, so its history survives.
	for ext, taskID := range known {
		if seen[ext] {
			continue
		}
		t, err := d.st.Get(taskID)
		if err != nil || t.Status == store.StatusDead || t.Status == store.StatusShelved ||
			t.Status == store.StatusDone {
			continue
		}
		if err := d.st.SetStatus(taskID, store.StatusDead); err != nil {
			return err
		}
	}
	return nil
}

// watchLedger polls until the context is cancelled. Polling rather than
// watching because the ledger is many small files written by other processes,
// and a poll is both simpler and immune to missed change notifications.
func (d *Daemon) watchLedger(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = 3 * time.Second
	}
	if _, err := state.ReadAll(); err != nil {
		log.Printf("[atrium] not reading the gwt ledger: %v", err)
		return
	}
	log.Printf("[atrium] ledger  -> %s (polling every %s)", state.SessionDir(), every)

	tick := time.NewTicker(every)
	defer tick.Stop()
	var lastErr string
	for {
		if err := d.syncLedger(); err != nil {
			// One line per distinct problem, not one per tick.
			if msg := err.Error(); msg != lastErr {
				log.Printf("[atrium] ledger sync: %v", err)
				lastErr = msg
			}
		} else if lastErr != "" {
			log.Printf("[atrium] ledger sync recovered")
			lastErr = ""
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
