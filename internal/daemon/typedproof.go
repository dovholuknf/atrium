package daemon

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// A say typed into a runner's queued-message box mid-turn is `typed`, not `delivered`: Claude Code has it in a box
// and reads it at its next step. The proof that it was read is the runner's own transcript, which writes a
// `queued_command` attachment holding the text when the box drains (docs/terminal/queued-box-findings.md). A
// UserPromptSubmit is NOT proof: it fires when the line goes into the box, a minute before it is read.
//
// Matching is by text, oldest say first, one transcript line to one say. A miss leaves the row `typed`, never a false
// `delivered`.

// typedProofEvery is how often a card with a typed say is looked at.
const typedProofEvery = 10 * time.Second

// typedUnreadDefault is the wait before a typed say is noted as stuck, when the setting is empty.
const typedUnreadDefault = 5 * time.Minute

// typedStuckNote is the note on a row that has gone unread past typed_unread_after.
const typedStuckNote = "typed into the session's queued box and still not read. a long tool call holds it until the " +
	"call returns."

// needleLen is how much of a say's text is matched, in runes. A long say is matched by its head.
const needleLen = 200

// typedUnreadAfter reads typed_unread_after, in minutes. A bad value answers the default.
func (d *Daemon) typedUnreadAfter() time.Duration {
	v, err := d.st.Setting(store.SettingTypedUnreadAfter)
	if err != nil {
		return typedUnreadDefault
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n <= 0 {
		return typedUnreadDefault
	}
	return time.Duration(n) * time.Minute
}

// queuedPromptsIn is the text of every message the runner took out of its queue box, in file order.
func queuedPromptsIn(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []string
	br := bufio.NewReaderSize(f, 256<<10)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 && len(line) <= 8<<20 && bytes.Contains(line, []byte(`queued_command`)) {
			if p, ok := queuedPromptOf(line); ok {
				out = append(out, p.Text)
			}
		}
		if err != nil {
			if err == io.EOF {
				return out, nil
			}
			return nil, err
		}
	}
}

// sayNeedle is the part of a say's text looked for in a transcript line.
func sayNeedle(text string) string {
	text = strings.TrimSpace(text)
	if r := []rune(text); len(r) > needleLen {
		text = string(r[:needleLen])
	}
	return text
}

// provenTyped is the ids of the typed says whose text appears among the prompts, oldest say first. A prompt proves one
// say. Typed text arrives with a banner in front of it, so a prompt matches when it contains the say.
func provenTyped(says []store.Say, prompts []string) []string {
	used := make([]bool, len(prompts))
	var ids []string
	for _, s := range says {
		n := sayNeedle(s.Text)
		if n == "" {
			continue
		}
		for i, p := range prompts {
			if !used[i] && strings.Contains(p, n) {
				used[i] = true
				ids = append(ids, s.ID)
				break
			}
		}
	}
	return ids
}

// proveTypedSays looks at one card's typed says against its transcript: a say its box has drained becomes
// delivered, and one unread past typed_unread_after is noted. It answers how many moved.
func (d *Daemon) proveTypedSays(taskID string, now time.Time) int {
	says, err := d.st.SayTypedRows(taskID)
	if err != nil || len(says) == 0 {
		return 0
	}
	var prompts []string
	if t, err := d.st.Get(taskID); err == nil && t != nil {
		if path := d.ctx.transcriptOf(t); path != "" {
			if prompts, err = queuedPromptsIn(path); err != nil {
				log.Printf("[atrium] could not read %s's transcript for typed says: %v", taskID, err)
			}
		}
	}
	proven := provenTyped(says, prompts)
	isProven := map[string]bool{}
	for _, id := range proven {
		isProven[id] = true
		if err := d.st.SayProved(id); err != nil {
			log.Printf("[atrium] could not mark say %s read: %v", id, err)
		}
	}
	after := d.typedUnreadAfter()
	for _, s := range says {
		if isProven[s.ID] || s.Note != "" {
			continue
		}
		at, err := time.Parse(store.TimeFormat, s.SentAt)
		if err != nil || now.Sub(at) < after {
			continue
		}
		note := fmt.Sprintf("%s (%d minutes)", typedStuckNote, int(now.Sub(at).Minutes()))
		if err := d.st.SayNoted(s.ID, note); err != nil {
			log.Printf("[atrium] could not note say %s as unread: %v", s.ID, err)
		}
	}
	if len(proven) > 0 {
		d.Publish(taskID)
	}
	return len(proven)
}

// typedProofLoop looks at every card with a typed say on a timer. Best effort: nothing here can fail a say.
func (d *Daemon) typedProofLoop(ctx context.Context) {
	tick := time.NewTicker(typedProofEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		ids, err := d.st.SayTypedTasks()
		if err != nil {
			continue
		}
		for _, id := range ids {
			d.proveTypedSays(id, time.Now())
		}
	}
}
