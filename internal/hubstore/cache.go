package hubstore

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// The cache: what each room last said, so a hub whose room is offline can show
// what was there rather than nothing.
//
// ── the rule this whole file is subordinate to ──────────
//
// THE CACHE IS NEVER AUTHORITATIVE. The only authoritative answer about a room
// comes from that room, and while it is connected it is the one asked. This is
// what was last heard, shown as such. Nothing read from here is ever written
// back as fact.
//
// WRITTEN WHILE CONNECTED, READ ONLY WHEN NOT. Not a read-through cache and not
// a performance layer. A connected room is asked every time. The hub does not
// serve a remembered card next to a live one and hope they agree.
//
// That is a rule about the CALLER and this package cannot enforce it, because
// liveness is a socket in internal/link. What this package does is keep the
// shape honest: there is no merge here, no per-row reconciliation, and no way
// to update one cached card. A room says everything or says nothing.
//
// ── what is cached, without a list to maintain ──────────
//
// THE HUB CACHES EXACTLY WHAT THE ROOM ITSELF PERSISTS, AND NEVER WHAT THE ROOM
// DECLINES TO PERSIST.
//
// That comes from a rule atrium already has. docs/activity-design.md says what
// a runner is doing right now is never written down, because it would be a lie
// the moment the daemon restarted, and a hub writing it down would break that
// rule at one remove. So a card's title, status, tags, worktree and runner are
// cached; activity, subagent chatter and typing state are streamed straight
// through and never land here.
//
// The payload is opaque for the same reason. A column per field would be a
// second copy of the room's schema, and the two would drift.

// Card is one cached card: what the room said, kept as it said it.
type Card struct {
	ID string `json:"id"`
	// Status is lifted out of the payload because the board groups on it and
	// nothing else is worth an index.
	Status string `json:"status"`
	// Payload is the card as the room sent it, untouched.
	Payload json.RawMessage `json:"payload"`
}

// Changes is what one announcement did to the cache.
//
// Counted so the discard can be written down. Wholesale replacement is the
// right rule and it is also the one that can quietly lose something a person
// remembers seeing, and this is what makes that answerable afterwards instead
// of being a thing nobody can explain.
type Changes struct {
	New     int `json:"new"`
	Changed int `json:"changed"`
	Gone    int `json:"gone"`
	Same    int `json:"same"`
}

// Announce replaces everything the hub was holding for a room.
//
// TAKEN WHOLE. The room is the only source of truth, so what it sends IS the
// state. ANYTHING THE HUB WAS HOLDING THAT IS NOT IN THE ANNOUNCEMENT IS
// DISCARDED ENTIRELY, because it is no longer there. No merging, no row by row
// reconciliation, and nothing kept on the chance it still exists.
//
// The audit entry is not optional decoration. See docs/decisions.md, 13.
func (s *Store) Announce(roomID string, cards []Card) (Changes, error) {
	var ch Changes
	r, err := s.Get(roomID)
	if err != nil {
		return ch, err
	}
	at := ts(now())

	err = s.tx(func(t *sql.Tx) error {
		ch = Changes{}
		was, err := heldBy(t, roomID)
		if err != nil {
			return err
		}
		seen := make(map[string]bool, len(cards))
		for _, c := range cards {
			id := strings.TrimSpace(c.ID)
			if id == "" {
				// A card with no id cannot be addressed, cached or discarded.
				// Dropped rather than refused: one malformed row must not cost
				// a room its whole announcement.
				continue
			}
			seen[id] = true
			switch old, had := was[id]; {
			case !had:
				ch.New++
			// STATUS COUNTS AS A CHANGE EVEN WHEN THE PAYLOAD DOES NOT.
			//
			// It is lifted into its own column for the board to group on, so a
			// room whose only announcement is that a card moved from running to
			// shelved would otherwise be recorded as nothing having happened.
			// That is the single most interesting kind of change there is.
			case old.status != c.Status || old.payload != payloadOf(c):
				ch.Changed++
			default:
				ch.Same++
			}
		}
		for id := range was {
			if !seen[id] {
				ch.Gone++
			}
		}

		// DELETE THEN INSERT, inside the transaction, which is why `tx` exists.
		// A failure between the two would leave a room with no cards at all,
		// and nothing could tell that from a room that genuinely has none.
		if _, err := t.Exec(`DELETE FROM room_card WHERE room_id = ?`, roomID); err != nil {
			return err
		}
		for _, c := range cards {
			id := strings.TrimSpace(c.ID)
			if id == "" {
				continue
			}
			if _, err := t.Exec(
				`INSERT INTO room_card (room_id, card_id, status, payload, cached_at)
				 VALUES (?, ?, ?, ?, ?)`,
				roomID, id, c.Status, payloadOf(c), at); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return ch, err
	}
	// LOGGED WHEN SOMETHING WAS DISCARDED, and not otherwise.
	//
	// The log exists because wholesale replacement is the right rule and is
	// also the one that can quietly lose something a person remembers seeing.
	// A card that is new or changed has not been lost: it is on the board. A
	// card that is gone is the one nobody can account for afterwards.
	//
	// The alternative is a line per announcement, which on a busy room is a
	// line every couple of seconds saying nothing happened, and a log nobody
	// can read is a log that answers nothing.
	if ch.Gone > 0 {
		s.Log(r, "announced", ch.String())
	}
	return ch, nil
}

// String is the audit line, written the way somebody would ask the question.
func (c Changes) String() string {
	return fmt.Sprintf("%d card(s) were discarded as they are no longer there. "+
		"%d are new. %d changed. %d unchanged", c.Gone, c.New, c.Changed, c.Same)
}

// payloadOf keeps an empty payload as an empty object rather than an empty
// string, so the column always holds something a JSON decoder can read.
func payloadOf(c Card) string {
	if len(c.Payload) == 0 {
		return "{}"
	}
	return string(c.Payload)
}

// held is one cached card as stored, for comparing against what just arrived.
type held struct{ status, payload string }

func heldBy(t *sql.Tx, roomID string) (map[string]held, error) {
	rows, err := t.Query(
		`SELECT card_id, status, payload FROM room_card WHERE room_id = ?`, roomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]held{}
	for rows.Next() {
		var id string
		var h held
		if err := rows.Scan(&id, &h.status, &h.payload); err != nil {
			return nil, err
		}
		out[id] = h
	}
	return out, rows.Err()
}

// Cards reads back what a room last said.
//
// ONLY CALL THIS FOR A ROOM THAT IS OFFLINE. A connected room answers for
// itself, every time, and serving a remembered card beside a live one is the
// third state decision 15 exists to rule out: some rows fresh, some remembered,
// and nothing saying which.
func (s *Store) Cards(roomID string) ([]Card, error) {
	var out []Card
	err := s.guard(func() error {
		rows, err := s.db.Query(
			`SELECT card_id, status, payload FROM room_card WHERE room_id = ? ORDER BY card_id`,
			roomID)
		if err != nil {
			return err
		}
		defer rows.Close()
		out = out[:0]
		for rows.Next() {
			var c Card
			var body string
			if err := rows.Scan(&c.ID, &c.Status, &body); err != nil {
				return err
			}
			c.Payload = json.RawMessage(body)
			out = append(out, c)
		}
		return rows.Err()
	})
	return out, err
}

// CardCount is how many cards a room was last holding.
//
// For the rooms tab, which says what would be lost sight of by forcing a room
// out. It is a cached number and the tab has to say so.
func (s *Store) CardCount(roomID string) (int, error) {
	var n int
	err := s.guard(func() error {
		return s.db.QueryRow(
			`SELECT COUNT(*) FROM room_card WHERE room_id = ?`, roomID).Scan(&n)
	})
	return n, err
}
