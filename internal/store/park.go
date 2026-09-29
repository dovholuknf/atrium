package store

import (
	"time"
)

// TouchHuman stamps the last moment a person did something to a card, and what
// they did. Not a status change and not an event: it is a fact about the card,
// read by keep-alive and by idle parking, and a keystroke must not cost a frame.
// See docs/keepalive-policy-design.md section 1.
func (s *Store) TouchHuman(id, via string, at time.Time) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE task SET human_at = ?, human_via = ? WHERE id = ?`, ts(at), via, id)
		return err
	})
}

// Park marks a card parked: `parked_at` set, and the status it had put back when
// the wind-down has since filed it `dead`. One `status-changed` event carries
// `parked: true`, because widening the event CHECK is a rebuild of the largest
// table here. `extra` is merged into the payload (who parked it, and why).
//
// A card that is already parked is left as it is and reports false.
func (s *Store) Park(id, was string, extra map[string]any) (bool, error) {
	parked := false
	err := s.inTx(func(tx *Tx) error {
		parked = false
		prev, err := getByOn(tx, `id = ?`, id)
		if err != nil {
			return err
		}
		if prev.ParkedAt != nil {
			return nil
		}
		n := now()
		status := prev.Status
		if was != "" && was != prev.Status {
			status = was
			var waiting any
			if status == StatusNeedsInput || status == StatusNeedsPermission {
				if prev.WaitingSince != nil {
					waiting = ts(*prev.WaitingSince)
				} else {
					waiting = ts(n)
				}
			}
			if _, err := tx.Exec(`UPDATE task SET status = ?, waiting_since = ? WHERE id = ?`,
				status, waiting, id); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`UPDATE task SET parked_at = ? WHERE id = ?`, ts(n), id); err != nil {
			return err
		}
		payload := map[string]any{"from": prev.Status, "to": status, "parked": true, "was": status}
		for k, v := range extra {
			payload[k] = v
		}
		if _, err := s.appendEventOn(tx, id, EventStatusChanged, payload); err != nil {
			return err
		}
		parked = true
		return nil
	})
	return parked, err
}

// Unpark clears `parked_at` and writes the matching event. It reports whether
// the card was parked.
func (s *Store) Unpark(id, by string) (bool, error) {
	was := false
	err := s.inTx(func(tx *Tx) error {
		was = false
		prev, err := getByOn(tx, `id = ?`, id)
		if err != nil {
			return err
		}
		if prev.ParkedAt == nil {
			return nil
		}
		if _, err := tx.Exec(`UPDATE task SET parked_at = '' WHERE id = ?`, id); err != nil {
			return err
		}
		if _, err := s.appendEventOn(tx, id, EventStatusChanged,
			map[string]any{"from": prev.Status, "to": prev.Status, "parked": false, "by": by}); err != nil {
			return err
		}
		was = true
		return nil
	})
	return was, err
}
