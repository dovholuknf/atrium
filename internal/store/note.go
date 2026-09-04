package store

import "strings"

// A note is for YOU. Sending it is a second, separate act.
//
// The message queue already exists and does the opposite thing: it fires as
// soon as there is anything to deliver, reaching the session on its next tool
// call or at the end of its turn. A note is written now and sent when you say
// so, and the two are different promises.
//
// What that buys is ordering. Three things thought of during a long turn, sent
// as one instruction at the end, rather than three interruptions in the
// middle. Claude Code takes input while it is thinking, so the send is less
// urgent than it once was, and the ordering is the part that still matters.
//
// A scratch pad per card rather than a list of notes with their own
// lifecycle. It accumulates while you type and empties when you send, and
// anything more than that is a to-do list, which atrium already refuses to be.

// MaxNote bounds a card's scratch pad.
//
// Eight thousand characters is several screens of thinking out loud and far
// more than anybody writes before sending. The bound exists because this is
// operator text with no other limit on it, and every other such field in the
// schema has one.
const MaxNote = 8000

// SetNote replaces a card's note. Empty clears it.
func (s *Store) SetNote(id, note string) error {
	note = strings.TrimSpace(note)
	if len(note) > MaxNote {
		cut := MaxNote
		for cut > 0 && !utf8Start(note[cut]) {
			cut--
		}
		note = strings.TrimSpace(note[:cut])
	}
	return s.guard(func() error {
		// last_activity_at is deliberately NOT touched, for the same reason
		// tagging does not touch it: writing a note is you thinking, not the
		// session doing anything, and bumping it would move a silent card to
		// the top of an activity-sorted list.
		_, err := s.db.Exec(`UPDATE task SET note = ? WHERE id = ?`, note, id)
		return err
	})
}

// HasNote reports whether there is anything written down on this card.
func (t *Task) HasNote() bool { return strings.TrimSpace(t.Note) != "" }
