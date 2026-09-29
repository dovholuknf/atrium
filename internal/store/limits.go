package store

import (
	"database/sql"
	"errors"
	"time"
)

// Room-kept limit readings, for the usage tab's first paint. See
// 0074_limit_reading. A row is a card's reported percent and reset time for one
// limit, written only when either changed since the last row for that card and
// kind, so a statusline that says the same thing writes nothing.

// The kinds of limit a reading is of.
const (
	LimitFiveHour = "five_hour"
	LimitWeekly   = "weekly"
)

// How long a reading is kept, by kind.
const (
	LimitKeepFiveHour = 8 * time.Hour
	LimitKeepWeekly   = 7 * 24 * time.Hour
)

// LimitReading is one card's figure for one limit at one time.
type LimitReading struct {
	At       time.Time `json:"at"`
	Card     string    `json:"card"`
	Kind     string    `json:"kind"`
	Pct      int       `json:"pct"`
	ResetsAt time.Time `json:"resets_at,omitempty"`
}

// AddLimitReading writes a reading when its percent or reset differs from the
// card's last one of that kind. It reports whether a row was written.
func (s *Store) AddLimitReading(r LimitReading) (bool, error) {
	if r.At.IsZero() {
		r.At = now()
	}
	wrote := false
	err := s.guard(func() error {
		wrote = false
		var pct int
		var resets string
		err := s.db.QueryRow(`SELECT pct, resets_at FROM limit_reading WHERE task_id = ? AND kind = ?
			ORDER BY at DESC, rowid DESC LIMIT 1`, r.Card, r.Kind).Scan(&pct, &resets)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil && pct == r.Pct && resets == limitTS(r.ResetsAt) {
			return nil
		}
		if _, err := s.db.Exec(`INSERT INTO limit_reading (task_id, kind, pct, resets_at, at) VALUES (?, ?, ?, ?, ?)`,
			r.Card, r.Kind, r.Pct, limitTS(r.ResetsAt), ts(r.At)); err != nil {
			return err
		}
		wrote = true
		return nil
	})
	return wrote, err
}

func limitTS(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return ts(t)
}

// LimitReadings lists the readings at or after since, oldest first.
func (s *Store) LimitReadings(since time.Time) ([]LimitReading, error) {
	out := []LimitReading{}
	err := s.guard(func() error {
		out = []LimitReading{}
		rows, err := s.db.Query(`SELECT task_id, kind, pct, resets_at, at FROM limit_reading WHERE at >= ?
			ORDER BY at, rowid`, ts(since))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				r          LimitReading
				resets, at string
			)
			if err := rows.Scan(&r.Card, &r.Kind, &r.Pct, &resets, &at); err != nil {
				return err
			}
			if r.At, err = parseTS(at); err != nil {
				return err
			}
			if resets != "" {
				if r.ResetsAt, err = parseTS(resets); err != nil {
					return err
				}
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}

// PruneLimitReadings deletes the readings past their keep, and returns how many.
func (s *Store) PruneLimitReadings(at time.Time) (int64, error) {
	var n int64
	err := s.guard(func() error {
		res, err := s.db.Exec(`DELETE FROM limit_reading WHERE (kind = ? AND at < ?) OR (kind = ? AND at < ?)
			OR (kind NOT IN (?, ?) AND at < ?)`,
			LimitFiveHour, ts(at.Add(-LimitKeepFiveHour)), LimitWeekly, ts(at.Add(-LimitKeepWeekly)),
			LimitFiveHour, LimitWeekly, ts(at.Add(-LimitKeepFiveHour)))
		if err != nil {
			return err
		}
		n, err = res.RowsAffected()
		return err
	})
	return n, err
}
