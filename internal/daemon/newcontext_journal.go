package daemon

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// The new-context journal: which cards have a run in flight, kept in the settings
// table so a restart can find what it cut off. See the "THE STEP IS IN MEMORY"
// note in newcontext.go.
//
// A room restart ends every terminal it owns, and the goroutine typing into one
// with it, so nothing is left to finish a clear. What the restart must not do is
// leave the card as it found it with no word said: a capture was typed, or
// `/clear` was, and the card's own history would not show it. At startup each
// journalled run is ended with a failed chip that names the step it was cut off
// on and says what to do. The chip is the existing failed chip, so it clears the
// way one does: a SessionStart naming another conversation, a rerun, or a dismissal.

// settingNewContextJournal is the settings key. One value, the whole map.
const settingNewContextJournal = "new_context_journal"

// ncJournalRow is what a restart needs to know about one run.
type ncJournalRow struct {
	Step  string    `json:"step"`
	File  string    `json:"file"`
	Conv  string    `json:"conv"`
	Since time.Time `json:"since"`
}

// captureOnly marks a run as the idle parking's, which is never journalled.
func (n *newContexts) captureOnly(taskID string, gen uint64) {
	n.mu.Lock()
	if cur := n.by[taskID]; cur != nil && cur.gen == gen {
		cur.capOnly = true
	}
	n.mu.Unlock()
	n.save()
}

// save hands persist the runs now in flight. A failed chip is not in flight.
func (n *newContexts) save() {
	if n == nil || n.persist == nil {
		return
	}
	n.saveMu.Lock()
	defer n.saveMu.Unlock()
	n.mu.Lock()
	snap := map[string]ncJournalRow{}
	for id, c := range n.by {
		if c.step == NewContextFailed || c.capOnly {
			continue
		}
		snap[id] = ncJournalRow{Step: c.step, File: c.file, Conv: c.conv, Since: c.since}
	}
	n.mu.Unlock()
	n.persist(snap)
}

// saveNewContextJournal writes the snapshot. A store that cannot take it has
// halted, which says so on its own, so this only logs.
func (d *Daemon) saveNewContextJournal(snap map[string]ncJournalRow) {
	val := ""
	if len(snap) > 0 {
		b, err := json.Marshal(snap)
		if err != nil {
			log.Printf("[atrium] could not journal the new contexts under way: %v", err)
			return
		}
		val = string(b)
	}
	if err := d.st.SetSetting(settingNewContextJournal, val); err != nil {
		log.Printf("[atrium] could not journal the new contexts under way: %v", err)
	}
}

// endAbandonedNewContexts runs once at startup, before any runner is reopened.
// Every journalled run belonged to a process that is gone, so each is ended: a
// failed chip naming the step, an event in the card's history, and the journal
// emptied. Nothing is typed. The card's terminal is not back yet, and a clear
// that may or may not have happened is not something to guess at.
func (d *Daemon) endAbandonedNewContexts() {
	raw, err := d.st.Setting(settingNewContextJournal)
	if err != nil || raw == "" {
		return
	}
	var rows map[string]ncJournalRow
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		log.Printf("[atrium] the new context journal is unreadable, dropping it: %v", err)
		d.saveNewContextJournal(nil)
		return
	}
	for id, row := range rows {
		if _, err := d.st.Get(id); err != nil {
			continue
		}
		n := map[string]int{NewContextCapture: 1, NewContextClear: 2, NewContextWake: 3}[row.Step]
		reason := fmt.Sprintf("the room restarted during step %d of 3 (%s), after %s, so the new context was "+
			"abandoned. Check whether %s was written, and start it again if the context did not clear",
			n, row.Step, time.Since(row.Since).Round(time.Second), row.File)
		d.nctx.seedFailed(id, row, reason)
		if err := d.st.AppendEvent(id, store.EventNotified, map[string]any{"by": newContextBy, "failed": reason}); err != nil {
			log.Printf("[atrium] could not record the abandoned new context on %s: %v", id, err)
		}
		log.Printf("[atrium] new context on %s ended at startup, %s", id, reason)
		d.publishTask(id)
	}
	d.saveNewContextJournal(nil)
}

// seedFailed puts a failed chip on a card for a run that no goroutine owns.
func (n *newContexts) seedFailed(taskID string, row ncJournalRow, reason string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.gens++
	n.by[taskID] = &newContext{step: NewContextFailed, file: row.File, conv: row.Conv, reason: reason,
		since: time.Now(), gen: n.gens}
}
