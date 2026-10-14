package store

import "time"

// A card that goes dead or done while it waits keeps that wait here. Coming back up (a deploy, a restart, a reopen)
// runs the card through running and into waiting again, and without this each step started a new wait: the phone and
// the board rang "waiting for you" for a card that had been waiting before the deploy.
//
// The carried wait is spent on entering a waiting state with no clock of its own, and it stays until a person speaks to
// the card (ClearCarriedWait), so the resume turn's own end gets it too.

func carryWaitOn(q querier, id string, since time.Time) error {
	_, err := q.Exec(`INSERT INTO waiting_carry (task_id, since) VALUES (?, ?)
		ON CONFLICT(task_id) DO UPDATE SET since = excluded.since`, id, ts(since))
	return err
}

func carriedWaitOn(q querier, id string) (string, bool) {
	var since string
	if err := q.QueryRow(`SELECT since FROM waiting_carry WHERE task_id = ?`, id).Scan(&since); err != nil || since == "" {
		return "", false
	}
	return since, true
}

// ClearCarriedWait forgets a card's carried wait, because somebody said something to it and its next wait is new.
func (s *Store) ClearCarriedWait(id string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`DELETE FROM waiting_carry WHERE task_id = ?`, id)
		return err
	})
}
