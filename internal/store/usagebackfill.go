package store

import "time"

// UsageBackfill is the cause of a row written by `atrium usage backfill` from a
// transcript, for turns the room had not recorded.
const UsageBackfill = "backfill"

// UsageSpan is the time a recorded row covers.
type UsageSpan struct{ Started, Ended time.Time }

// UsageSpans lists the time every row of a card's session covers, from since on.
// Backfill leaves a reply inside one of them alone.
func (s *Store) UsageSpans(taskID, resumeID string, since time.Time) ([]UsageSpan, error) {
	var out []UsageSpan
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(`SELECT started_at, ended_at FROM session_usage
			WHERE task_id = ? AND resume_id = ? AND ended_at >= ?`, taskID, resumeID, ts(since))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var a, b string
			if err := rows.Scan(&a, &b); err != nil {
				return err
			}
			var sp UsageSpan
			if sp.Started, err = parseTS(a); err != nil {
				return err
			}
			if sp.Ended, err = parseTS(b); err != nil {
				return err
			}
			out = append(out, sp)
		}
		return rows.Err()
	})
	return out, err
}
