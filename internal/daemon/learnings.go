package daemon

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/dovholuknf/atrium/internal/store"
)

// LEARNINGS.md, THE POOL'S MEMORY. A pool card may leave one paragraph with `learned` on atrium_done, and atrium appends
// it to LEARNINGS.md in the card's directory. See docs/changes/items-stage3.md and docs/rnd/atrium-items-decisions.md
// section 8.
//
//   - ATRIUM ONLY APPENDS. Only the human edits or prunes the file, and nothing here ever rewrites or drops an entry.
//   - THE FILE IS CAPPED, at LearningsMaxEntries entries or LearningsMaxBytes bytes. An entry that would pass the cap is
//     NOT written, and atrium says so on the card (an event, and the log) instead of dropping it silently. The cap being
//     reached is also said, once, by the append that reaches it. The human prunes, and the next `learned` is written.
//   - NOTHING COMMITS IT. The file sits in the worktree for the human to read and commit.

// LearningsFile is the file's name, in the card's directory.
const LearningsFile = "LEARNINGS.md"

// LearningsMaxEntries and LearningsMaxBytes are the cap. Either one reached is the cap reached.
const (
	LearningsMaxEntries = 50
	LearningsMaxBytes   = 20 * 1024
)

// LearnedMax bounds one entry, in bytes: a paragraph.
const LearnedMax = 1500

// LearningsSource names the alert's event on the card.
const LearningsSource = "item-learnings"

var learningsMu sync.Mutex

const learningsHeader = "# LEARNINGS\n\nOne entry per item, appended by atrium from what the card reported. Edit or prune by hand: atrium only appends.\n\n"

func learningsPath(card *store.Task) string {
	if card == nil || strings.TrimSpace(card.Worktree) == "" {
		return ""
	}
	return filepath.Join(card.Worktree, LearningsFile)
}

// countEntries is the entries in a LEARNINGS.md: its lines that open with `## `.
func countEntries(body string) int {
	n := 0
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "## ") {
			n++
		}
	}
	return n
}

// learningsEntries is how many entries a card's file holds, 0 for no file.
func learningsEntries(card *store.Task) int {
	p := learningsPath(card)
	if p == "" {
		return 0
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return 0
	}
	return countEntries(string(b))
}

// learnedParagraph is what a worker left, as one paragraph and bounded.
func learnedParagraph(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > LearnedMax {
		s = strings.ToValidUTF8(s[:LearnedMax], "")
	}
	return s
}

// appendLearned appends one entry for an item, or says why not on the card. It never fails the end that carried it.
func (d *Daemon) appendLearned(card *store.Task, it *store.ItemRow, learned string) {
	para := learnedParagraph(learned)
	if para == "" {
		return
	}
	path := learningsPath(card)
	if path == "" {
		d.learningsAlert(card, it, "the card has no directory to keep "+LearningsFile+" in")
		return
	}
	learningsMu.Lock()
	defer learningsMu.Unlock()
	body := ""
	if b, err := os.ReadFile(path); err == nil {
		body = string(b)
	} else if !os.IsNotExist(err) {
		d.learningsAlert(card, it, "could not read "+LearningsFile+": "+err.Error())
		return
	}
	entry := fmt.Sprintf("## %s %s\n\n%s\n\n", it.ID, strings.TrimSpace(it.UpdatedAt), para)
	if body == "" {
		entry = learningsHeader + entry
	} else if !strings.HasSuffix(body, "\n") {
		entry = "\n\n" + entry
	}
	if n := countEntries(body); n >= LearningsMaxEntries || len(body)+len(entry) > LearningsMaxBytes {
		d.learningsAlert(card, it, fmt.Sprintf("%s is full (%d entries, %d bytes, the cap is %d entries or %d KB), so the learning of "+
			"item %s was NOT written. prune it by hand", LearningsFile, n, len(body), LearningsMaxEntries, LearningsMaxBytes/1024, it.ID))
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		d.learningsAlert(card, it, "could not write "+LearningsFile+": "+err.Error())
		return
	}
	_, werr := f.WriteString(entry)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		d.learningsAlert(card, it, "could not write "+LearningsFile+": "+werr.Error())
		return
	}
	if n := countEntries(body) + 1; n >= LearningsMaxEntries || len(body)+len(entry) >= LearningsMaxBytes {
		d.learningsAlert(card, it, fmt.Sprintf("%s has reached its cap (%d entries, %d bytes, the cap is %d entries or %d KB). "+
			"the next learning will not be written until it is pruned by hand", LearningsFile, n, len(body)+len(entry),
			LearningsMaxEntries, LearningsMaxBytes/1024))
	}
}

// learningsAlert says it on the card, and in the log.
func (d *Daemon) learningsAlert(card *store.Task, it *store.ItemRow, text string) {
	if err := d.st.AppendEvent(card.ID, store.EventNotified, map[string]any{
		"source": LearningsSource, "item_id": it.ID, "text": text,
	}); err != nil {
		log.Printf("[atrium] could not record the learnings alert for item %s: %v", it.ID, err)
	}
	log.Printf("[atrium] pool %s: %s", it.Pool, text)
	d.Publish(card.ID)
}
