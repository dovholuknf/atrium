package store

import (
	"strings"
	"time"
)

// An agent saying it finished, and what it did.
//
// This is the largest hole in what atrium does, and it is a hole in the shape
// of a missing verb. Everything an agent reported landed in `needs-input`, so
// the board could not tell "finished, go and look at the result" from "stuck,
// answer me". Only a human moving a card by hand ever produced `done`.
//
// The v2 design named `submit(kind="task-complete")` for this and it was never
// built. What carries it now is a command, because a command is the one
// channel every runner already has: an agent that can run `ls` can run this,
// with no MCP server, no tool description and no cooperation from the harness.

// MaxRecap bounds what a session may write about itself.
//
// A recap is the two or three sentences a session would say if you asked it
// what it just did. It is not a transcript, not a diff, and not a summary of
// its own reasoning, and a column with no limit on it is how a card ends up
// holding one of those. Two thousand characters is several paragraphs and far
// more than the good ones use.
const MaxRecap = 2000

// SetRecap records what a session says it did.
//
// Truncated rather than refused. A session that wrote too much still wrote
// something worth keeping, and failing the call would make an agent retry, and
// the retry would be longer.
//
// Empty CLEARS the recap, which is the operator deciding an account was wrong
// rather than an agent deciding it never happened.
func (s *Store) SetRecap(id, recap string) error {
	recap = strings.TrimSpace(recap)
	if len(recap) > MaxRecap {
		// On a rune boundary, so truncating never produces invalid UTF-8 in a
		// field the board renders.
		cut := MaxRecap
		for cut > 0 && !utf8Start(recap[cut]) {
			cut--
		}
		recap = strings.TrimSpace(recap[:cut]) + "..."
	}
	at := ""
	if recap != "" {
		at = ts(now())
	}
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE task SET recap = ?, recap_at = ? WHERE id = ?`,
			recap, at, id)
		return err
	})
}

// utf8Start reports whether a byte begins a rune. Continuation bytes are
// 10xxxxxx.
func utf8Start(b byte) bool { return b&0xC0 != 0x80 }

// Recapped reports whether this card left an account of itself.
//
// The useful cut through everything that has ever run here. A session that
// ended with a recap said what it did. One that did not is either still worth
// writing up or was never worth starting, and both of those are worth seeing.
func (t *Task) Recapped() bool { return strings.TrimSpace(t.Recap) != "" }

// FinishedAt is when the work on this card was declared over, or nil.
func (t *Task) FinishedAt() *time.Time { return t.RecapAt }
