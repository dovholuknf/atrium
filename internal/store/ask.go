package store

import "strings"

// A session saying what it needs, and who it asked.
//
// The mirror of `recap.go`. That records what a session did when its work is
// over; this records what it cannot get past while the work is still going.
// Both are written by the only party that knows, and both are bounded on the
// way in for the same reason: a card is a thing somebody reads at a glance.
//
// WHY THIS IS NOT `Why`. An ask landed there first, because `why` was already
// drawn under the title and putting it anywhere else meant touching the board.
// The two facts do not belong together. `Why` is what this card is FOR,
// written once and read in a week. An ask is a question outstanding this
// minute. Sharing one field meant an ask overwrote the standing intent with
// something stale by lunchtime, with no way to get it back, and the board drew
// whichever it was in the same italic line.

// MaxAsk bounds what a session may say it needs.
//
// A sentence or two. This is a question, not a transcript: an agent that needs
// to explain at length has somewhere to do that, which is its own terminal,
// and this is going on a card.
const MaxAsk = 500

// SetAsk records what a session needs, and the peer it was routed to.
//
// An empty peer means the ask is on the board for a human, which is what it
// has always meant. An empty ask CLEARS the whole thing, peer and timestamp
// with it, which is what answering one does.
//
// Truncated rather than refused, the same rule SetRecap follows: a session
// that wrote too much still wrote something worth keeping, and refusing makes
// an agent retry, and the retry is longer.
func (s *Store) SetAsk(id, ask, peer string) error {
	ask = strings.TrimSpace(ask)
	if len(ask) > MaxAsk {
		// On a rune boundary, so truncating never leaves invalid UTF-8 in a
		// field the board renders.
		cut := MaxAsk
		for cut > 0 && !utf8Start(ask[cut]) {
			cut--
		}
		ask = strings.TrimSpace(ask[:cut]) + "..."
	}
	at, who := "", strings.TrimSpace(peer)
	if ask != "" {
		at = ts(now())
	} else {
		// Nothing outstanding means nobody owes it an answer, so the peer goes
		// too. Leaving it behind is how a card ends up naming a session that
		// answered it an hour ago.
		who = ""
	}
	return s.guard(func() error {
		_, err := s.db.Exec(
			`UPDATE task SET ask = ?, ask_at = ?, ask_peer = ?, last_activity_at = ? WHERE id = ?`,
			ask, at, who, ts(now()), id)
		return err
	})
}

// ClearAsk marks a card's question as no longer outstanding.
func (s *Store) ClearAsk(id string) error { return s.SetAsk(id, "", "") }

// Asking reports whether this card has a question nobody has answered.
func (t *Task) Asking() bool { return strings.TrimSpace(t.Ask) != "" }

// AskedAPeer reports whether the outstanding question went to another session
// rather than onto the board for a human.
func (t *Task) AskedAPeer() bool { return t.Asking() && t.AskPeer != "" }
