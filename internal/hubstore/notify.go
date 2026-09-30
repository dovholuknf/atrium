package hubstore

import (
	"database/sql"
	"time"
)

// What the hub last told the operator about, per card.
//
// ── what a row is ───────────────────────────────────────
//
// notify_sent holds one row per card: the NOTIFY IDENTITY the hub last acted on
// (card id, reason and the timestamp that made it a reason), and `at`, which is
// the last time the card appeared in an announcement. A room republishing the
// same card every two seconds therefore changes nothing here and buzzes nobody.
// The identity is computed in internal/link, which knows what a card payload
// looks like. This package only compares and remembers.
//
// ── what is NOT deleted, and when a row goes ────────────
//
// A ROW IS NOT DELETED BECAUSE ITS CARD LEFT THE CACHE. A room restarting, a
// card shelved and unshelved, or a room offline for an afternoon all take a card
// out of the cache and put it back, and a card that comes back with the same
// identity must not fire a second time. The row stays until its identity
// changes. The only prune is a row whose card has been absent for more than
// notifyKeep (seven days), which `at` measures because it is refreshed on every
// announcement the card is in. A card gone that long is gone.

// notifyKeep is how long an absent card's row is kept.
const notifyKeep = 7 * 24 * time.Hour

// notifyTouch is how stale `at` may be before an unchanged row is rewritten, so
// a two second announcement does not become a write per card per two seconds.
const notifyTouch = time.Hour

// NotifyRecord compares a room's current identities with the last stored ones
// and stores the new ones. It returns the card ids whose identity changed or is
// new, which is what the caller notifies about.
//
// `ids` is card id to identity, holding only cards that have one. A card with no
// identity (nothing wants a human) keeps whatever row it has. `present` is every
// card id in the announcement, identity or not, and only refreshes `at`.
//
// `silent` is the seed: the identities are stored and nothing is returned. It is
// how a room's first announcement ever, and the moment the feature is turned on,
// avoid a flood.
func (s *Store) NotifyRecord(roomID string, ids map[string]string, present []string, silent bool) ([]string, error) {
	var fire []string
	at := now()
	err := s.tx(func(t *sql.Tx) error {
		fire = nil
		rows, err := t.Query(`SELECT card_id, identity, at FROM notify_sent WHERE room_id = ?`, roomID)
		if err != nil {
			return err
		}
		type row struct {
			identity string
			at       time.Time
		}
		had := map[string]row{}
		for rows.Next() {
			var id, ident, when string
			if err := rows.Scan(&id, &ident, &when); err != nil {
				rows.Close()
				return err
			}
			r := row{identity: ident}
			if p := parseOrNil(when); p != nil {
				r.at = *p
			}
			had[id] = r
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()

		for id, ident := range ids {
			if old, ok := had[id]; ok && old.identity == ident {
				continue
			}
			if _, err := t.Exec(
				`INSERT INTO notify_sent (room_id, card_id, identity, at) VALUES (?, ?, ?, ?)
				 ON CONFLICT (room_id, card_id) DO UPDATE SET identity = excluded.identity,
				                                              at = excluded.at`,
				roomID, id, ident, ts(at)); err != nil {
				return err
			}
			if !silent {
				fire = append(fire, id)
			}
		}
		for _, id := range present {
			old, ok := had[id]
			if !ok || ids[id] != "" || at.Sub(old.at) <= notifyTouch {
				continue
			}
			if _, err := t.Exec(`UPDATE notify_sent SET at = ? WHERE room_id = ? AND card_id = ?`,
				ts(at), roomID, id); err != nil {
				return err
			}
		}
		// An unchanged row with an identity is refreshed the same way.
		for id, ident := range ids {
			old, ok := had[id]
			if !ok || old.identity != ident || at.Sub(old.at) <= notifyTouch {
				continue
			}
			if _, err := t.Exec(`UPDATE notify_sent SET at = ? WHERE room_id = ? AND card_id = ?`,
				ts(at), roomID, id); err != nil {
				return err
			}
		}
		return nil
	})
	return fire, err
}

// NotifyPrune drops rows whose card has not been in an announcement for more
// than seven days.
func (s *Store) NotifyPrune() (int64, error) {
	var n int64
	err := s.guard(func() error {
		res, err := s.db.Exec(`DELETE FROM notify_sent WHERE at < ?`, ts(now().Add(-notifyKeep)))
		if err != nil {
			return err
		}
		n, _ = res.RowsAffected()
		return nil
	})
	return n, err
}

// NotifyCount is how many rows a room has, for tests and for the first look.
func (s *Store) NotifyCount(roomID string) (int, error) {
	var n int
	err := s.guard(func() error {
		return s.db.QueryRow(`SELECT COUNT(*) FROM notify_sent WHERE room_id = ?`, roomID).Scan(&n)
	})
	return n, err
}
