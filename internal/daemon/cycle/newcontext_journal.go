package cycle

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
const SettingNewContextJournal = "new_context_journal"

// ncJournalRow is what a restart needs to know about one run.
type NcJournalRow struct {
	Step  string    `json:"step"`
	File  string    `json:"file"`
	Conv  string    `json:"conv"`
	Since time.Time `json:"since"`
}

// beginCaptureOnly is begin for the idle parking's run. It is marked before the
// claim saves, so it is never journalled, not even for a moment.
func (n *NewContexts) BeginCaptureOnly(taskID, file, conv string) (uint64, bool) {
	return n.Claim(taskID, &NewContext{Step: NewContextCapture, File: file, Conv: conv, CapOnly: true})
}

// typingHeld reports whether board typing is refused: for the whole cycle, from the moment it is armed, because
// a key typed while it waits lands between its steps and in the context about to be cleared. The way out never
// depends on a step finishing: "let me type" ends the run. A failed run and the idle parking's capture hold nothing.
func (n *NewContexts) TypingHeld(taskID string) bool {
	if n == nil {
		return false
	}
	n.Mu.Lock()
	defer n.Mu.Unlock()
	cur := n.By[taskID]
	return cur != nil && cur.Step != NewContextFailed && !cur.CapOnly && cur.Armed
}

// arm marks gen's run as holding input. It reports whether the run is armed, and whether this call armed it.
func (n *NewContexts) Arm(taskID string, gen uint64) (armed, changed bool) {
	n.Mu.Lock()
	defer n.Mu.Unlock()
	cur := n.By[taskID]
	if cur == nil || cur.Gen != gen || cur.Step == NewContextFailed {
		return false, false
	}
	changed = !cur.Armed
	cur.Armed = true
	return true, changed
}

// setWaiting puts what a step waits on on the card's chip, or takes it off with "". It reports whether the text
// changed, so the caller publishes only then. Never on a failed chip, whose reason is its own.
func (n *NewContexts) SetWaiting(taskID string, gen uint64, why string) bool {
	n.Mu.Lock()
	defer n.Mu.Unlock()
	cur := n.By[taskID]
	if cur == nil || cur.Gen != gen || cur.Step == NewContextFailed || cur.Reason == why {
		return false
	}
	cur.Reason = why
	return true
}

// setTyping marks atrium's own write to the terminal as under way or over, for
// gen's run only. It reports whether the run is still the card's.
func (n *NewContexts) SetTyping(taskID string, gen uint64, on bool) bool {
	n.Mu.Lock()
	defer n.Mu.Unlock()
	cur := n.By[taskID]
	if cur == nil || cur.Gen != gen {
		return false
	}
	cur.Typing = on
	return true
}

// typingNow reports whether atrium's own write to the terminal is under way.
func (n *NewContexts) TypingNow(taskID string) bool {
	n.Mu.Lock()
	defer n.Mu.Unlock()
	cur := n.By[taskID]
	return cur != nil && cur.Typing
}

// save hands persist the runs now in flight. A failed chip is not in flight.
func (n *NewContexts) Save() {
	if n == nil || n.Persist == nil {
		return
	}
	n.SaveMu.Lock()
	defer n.SaveMu.Unlock()
	n.Mu.Lock()
	snap := map[string]NcJournalRow{}
	for id, c := range n.By {
		if c.Step == NewContextFailed || c.CapOnly {
			continue
		}
		snap[id] = NcJournalRow{Step: c.Step, File: c.File, Conv: c.Conv, Since: c.Since}
	}
	n.Mu.Unlock()
	n.Persist(snap)
}

// saveNewContextJournal writes the snapshot. A store that cannot take it has
// halted, which says so on its own, so this only logs.
func (d *Service) SaveNewContextJournal(snap map[string]NcJournalRow) {
	val := ""
	if len(snap) > 0 {
		b, err := json.Marshal(snap)
		if err != nil {
			log.Printf("[atrium] could not journal the new contexts under way: %v", err)
			return
		}
		val = string(b)
	}
	if err := d.St.SetSetting(SettingNewContextJournal, val); err != nil {
		log.Printf("[atrium] could not journal the new contexts under way: %v", err)
	}
}

// endAbandonedNewContexts runs once at startup, before any runner is reopened.
// Every journalled run belonged to a process that is gone, so each is ended: a
// failed chip naming the step, an event in the card's history, and the journal
// emptied. Nothing is typed. The card's terminal is not back yet, and a clear
// that may or may not have happened is not something to guess at.
func (d *Service) EndAbandonedNewContexts() {
	raw, err := d.St.Setting(SettingNewContextJournal)
	if err != nil || raw == "" {
		return
	}
	var rows map[string]NcJournalRow
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		log.Printf("[atrium] the new context journal is unreadable, dropping it: %v", err)
		d.SaveNewContextJournal(nil)
		return
	}
	for id, row := range rows {
		if _, err := d.St.Get(id); err != nil {
			continue
		}
		// Cut off before the ack: nothing was cleared and nothing was promised, so no
		// chip. The card is still past its limit, and the trigger starts it again.
		if row.Step == NewContextLimit || row.Step == NewContextCapture {
			if err := d.St.AppendEvent(id, store.EventNotified, map[string]any{"by": NewContextBy,
				"dropped": "the room restarted before atrium ready came"}); err != nil {
				log.Printf("[atrium] could not record the abandoned new context on %s: %v", id, err)
			}
			continue
		}
		n := map[string]int{NewContextClear: 2, NewContextWake: 3}[row.Step]
		reason := fmt.Sprintf("the room restarted during step %d of 3 (%s), after %s, so the new context was "+
			"abandoned. Its handoff is %s, and running it again resumes from the clear",
			n, row.Step, time.Since(row.Since).Round(time.Second), row.File)
		d.Nctx.SeedFailed(id, row, reason)
		if err := d.St.AppendEvent(id, store.EventNotified, map[string]any{"by": NewContextBy, "failed": reason}); err != nil {
			log.Printf("[atrium] could not record the abandoned new context on %s: %v", id, err)
		}
		log.Printf("[atrium] new context on %s ended at startup, %s", id, reason)
		d.Publish(id)
	}
	d.SaveNewContextJournal(nil)
}

// seedFailed puts a failed chip on a card for a run that no goroutine owns.
func (n *NewContexts) SeedFailed(taskID string, row NcJournalRow, reason string) {
	n.Mu.Lock()
	defer n.Mu.Unlock()
	n.Gens++
	// abandoned: the run began in row.Conv, and after a /clear the card resumes on
	// another, so its SessionStart would otherwise read as proof the clear happened
	// and delete this chip seconds after startup. See sessionStarted.
	// Past the ack, so a rerun resumes from the clear, or the wake once it was typed.
	n.By[taskID] = &NewContext{Step: NewContextFailed, File: row.File, Conv: row.Conv, Reason: reason,
		Since: time.Now(), Gen: n.Gens, Abandoned: true, Acked: true, Cleared: row.Step == NewContextWake}
}
