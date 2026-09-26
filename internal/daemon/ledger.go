package daemon

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// The daemon's half of the work ledger. See docs/work-ledger-design.md and
// internal/store/ledger.go, which holds the rules.
//
// The store moves items and queues notices inside its transactions. What only
// the daemon can do is here: say whether a card's session is still there,
// sweep the items no exit path reached, type a queued notice when the
// arbiter's terminal is free, and write the snapshot file.
//
// ATRIUM NEVER RESUMES AND NEVER CLOSES WORK. It flags work that ended without
// a report and tells the arbiter once. The launcher or the human decides what
// happens next.

// liveness says whether a card's session is there. Only `gone` ends work, and
// `unknown` is the case the reaper deliberately leaves alone: a waiting card
// with no pid that atrium does not own, or a window-mode session whose process
// atrium never held. Nothing is moved on a guess.
func (d *Daemon) liveness(t *store.Task) store.Liveness {
	if d.sup.get(t.ID) != nil {
		return store.Live
	}
	if t.PID > 0 {
		if processAlive(t.PID) {
			return store.Live
		}
		return store.Gone
	}
	switch t.Status {
	case store.StatusDead:
		// Filed dead by the reaper or by an exit, with no pid to ask.
		return store.Gone
	case store.StatusRunning, store.StatusNeedsInput, store.StatusNeedsPermission:
		if time.Since(t.LastActivityAt) < QuietAfter {
			return store.Live
		}
	}
	return store.LiveUnknown
}

// sweepLedger reconciles every open item whose session has no end recorded
// against the card's liveness. A card that is gone gets its `exited` event
// here, which moves the item in the same transaction, exactly as any other
// exit path would. Runs at startup and on the reaper's tick.
//
// Archived cards are included, since the store reads them by id. A pruned card
// has no row to hang an exit on and no process atrium can ask about, so its
// item keeps its state.
func (d *Daemon) sweepLedger() error {
	items, err := d.st.OpenWorkItems()
	if err != nil {
		return err
	}
	for _, w := range items {
		t, err := d.st.Get(w.TaskID)
		if err != nil {
			continue
		}
		if d.liveness(t) != store.Gone {
			continue
		}
		detected := "process is gone"
		if t.PID <= 0 {
			detected = "card is dead with no process to ask"
		}
		if err := d.st.AppendEvent(t.ID, store.EventExited, map[string]any{
			"by": "ledger sweep", "detected": detected, "pid": t.PID, "generation": w.Generation,
		}); err != nil {
			return err
		}
		log.Printf("[atrium] work ledger: %s had no end recorded and is gone", t.DisplayTitle())
	}
	return nil
}

// ledgerChanged is the store telling the daemon an item moved. It asks for the
// snapshot to be rewritten and never waits: the file is best effort, and the
// change that triggered it has already committed.
func (d *Daemon) ledgerChanged(taskID string) {
	select {
	case d.ledgerDirty <- struct{}{}:
	default:
	}
	if taskID != "" {
		d.publishTask(taskID)
	}
}

// ledgerNotice is a notice the ledger queued, now durable on the arbiter's
// queue. It is typed when the arbiter's terminal is free, exactly as a peer's
// message is, and the hooks carry it otherwise.
func (d *Daemon) ledgerNotice(n store.LedgerNotice) {
	d.publishTask(n.ArbiterID)
	d.deferPeerInjection(n.ArbiterID, n.MessageID, n.From, n.Text, d.waitsForTurn(n.ArbiterID, WhenImmediate))
	if n.Source == store.NoticeEnded {
		d.emitLifecycle("work-ended", n.Text)
	}
	log.Printf("[atrium] work ledger: queued a %s notice about %s for %s", n.Source, n.From, n.ArbiterID)
}

// ledgerFile is where this room's snapshot goes, beside its database.
func (d *Daemon) ledgerFile() string { return store.LedgerFilePath(d.opts.DBPath) }

// writeLedgerFile rewrites the snapshot. Logged and ignored on failure: the
// database is the record, and this file is only a way to read it without one.
func (d *Daemon) writeLedgerFile() {
	if err := d.st.WriteLedgerFile(d.ledgerFile()); err != nil {
		log.Printf("[atrium] work ledger: could not write %s: %v", d.ledgerFile(), err)
	}
}

// ledgerWriter rewrites the snapshot whenever it is asked to, until ctx ends.
// One write at a time, and every request that arrived during a write is one
// more write, not one each.
func (d *Daemon) ledgerWriter(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.ledgerDirty:
			d.writeLedgerFile()
		}
	}
}

// startLedger runs the one-time backfill, the first sweep, and the first
// snapshot. Nothing here can stop the daemon starting: a storage failure has
// already halted the store and says so on the board.
func (d *Daemon) startLedger() {
	res, err := d.st.BackfillWorkLedger(d.liveness)
	switch {
	case err != nil:
		log.Printf("[atrium] work ledger: backfill: %v", err)
	case !res.Skipped:
		log.Printf("[atrium] work ledger: backfilled %d launched card(s) from the last %d days, marked inferred: "+
			"%d reported, %d ended without a report, %d open (%d of unknown liveness)",
			res.Items, int(store.LedgerBackfillWindow/(24*time.Hour)), res.Reported, res.Ended, res.Open, res.Unknown)
	}
	if err := d.sweepLedger(); err != nil {
		log.Printf("[atrium] work ledger: sweep: %v", err)
	}
	d.writeLedgerFile()
}

// briefHead is what a work item keeps of the brief and the launch prompt.
func briefHead(brief, prompt string) string {
	parts := []string{}
	if b := strings.TrimSpace(brief); b != "" {
		parts = append(parts, b)
	}
	if p := strings.TrimSpace(prompt); p != "" {
		parts = append(parts, "prompt: "+p)
	}
	return strings.Join(parts, "\n\n")
}
