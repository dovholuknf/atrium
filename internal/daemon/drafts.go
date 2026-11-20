package daemon

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// What the operator was typing when the room restarted. The pty host keeps the session, and the line atrium tracked
// for it is written out at the stop and typed back once the reattached prompt is proven empty.

const (
	draftDirName = "drafts"
	draftSuffix  = ".draft"
	// typingQuiet is how recent a key must be for the stop to wait for it, and defaultTypingGiveUp how long it waits.
	typingQuiet         = 5 * time.Second
	defaultTypingGiveUp = 60 * time.Second
)

func (d *Daemon) draftPath(id string) string {
	return filepath.Join(filepath.Dir(d.opts.DBPath), draftDirName, id+draftSuffix)
}

// typingGate waits, bounded, while anyone pressed a key in the last typingQuiet, so a restart does not land mid-word.
func (d *Daemon) typingGate() {
	poll, giveUp := d.gatePoll, d.typingGiveUp
	if poll <= 0 {
		poll = defaultGatePoll
	}
	if giveUp <= 0 {
		giveUp = defaultTypingGiveUp
	}
	start := time.Now()
	var last string
	for {
		id := d.typingCard()
		if id == "" {
			if last != "" {
				log.Printf("[atrium] the stop waited %s for typing to stop", time.Since(start).Round(time.Millisecond))
			}
			return
		}
		if id != last {
			log.Printf("[atrium] the stop is waiting for typing on %s to stop", id)
			last = id
		}
		if time.Since(start) >= giveUp {
			log.Printf("[atrium] gave up waiting after %s for typing on %s, stopping anyway", giveUp, id)
			return
		}
		time.Sleep(poll)
	}
}

// typingCard is a card with a key pressed in the last typingQuiet, or empty when there is none.
func (d *Daemon) typingCard() string {
	for _, r := range d.sup.all() {
		r.typeMu.Lock()
		at := r.lastTyped
		r.typeMu.Unlock()
		if !at.IsZero() && time.Since(at) < typingQuiet {
			return r.taskID
		}
	}
	return ""
}

// saveDrafts writes the exact part typed line of every runner the pty host keeps through this stop, and removes a
// stale one for a runner with none, so an old draft is never typed back.
func (d *Daemon) saveDrafts() {
	if d.stopRunners.Load() {
		return
	}
	for _, r := range d.sup.all() {
		if hostOf(r) == nil {
			continue
		}
		r.typeMu.Lock()
		r.line.settle()
		draft := ""
		if r.line.exactLocked() {
			draft = string(r.line.text)
		}
		r.typeMu.Unlock()
		path := d.draftPath(r.taskID)
		if strings.TrimSpace(draft) == "" {
			_ = os.Remove(path)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			log.Printf("[atrium] could not save the typed line for %s: %v", r.taskID, err)
			continue
		}
		if err := os.WriteFile(path, []byte(draft), 0o600); err != nil {
			log.Printf("[atrium] could not save the typed line for %s: %v", r.taskID, err)
			continue
		}
		log.Printf("[atrium] saved %d typed char(s) on %s to type back after the restart", len([]rune(draft)), r.taskID)
	}
}

// takeDraft is the line saved for this card at the last stop, once.
func (d *Daemon) takeDraft(id string) string {
	path := d.draftPath(id)
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	_ = os.Remove(path)
	return string(b)
}

// restoreDraft types the saved line back into a reattached runner whose line was just proven empty, unless a key
// landed since. Under the input locks, the way letThrough puts a draft back.
func (d *Daemon) restoreDraft(run *runner) {
	draft := d.takeDraft(run.taskID)
	if draft == "" {
		return
	}
	run.injectMu.Lock()
	defer run.injectMu.Unlock()
	run.pasteMu.Lock()
	defer run.pasteMu.Unlock()
	run.typeMu.Lock()
	busy := len(run.line.text) > 0 || !run.line.empty() || run.keySeq != 0
	run.typeMu.Unlock()
	if busy {
		log.Printf("[atrium] %s: the line typed before the restart was not typed back, the line is no longer empty", run.taskID)
		return
	}
	if err := run.Write(retypeDraft(draft)); err != nil {
		log.Printf("[atrium] %s: could not type back the line from before the restart: %v", run.taskID, err)
		return
	}
	run.typeMu.Lock()
	run.line.text = append([]rune(draft), run.line.text...)
	run.keySeq++
	run.typeMu.Unlock()
	log.Printf("[atrium] %s: typed back %d char(s) that were on the line before the restart", run.taskID, len([]rune(draft)))
}
