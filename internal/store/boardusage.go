package store

// BoardUsage is one control's total across every day, and the last day it was used.
type BoardUsage struct {
	Control string `json:"control"`
	N       int    `json:"n"`
	Last    string `json:"last"`
}

// AddBoardUsage adds n uses of a control on a day (YYYY-MM-DD). One row per control and day.
func (s *Store) AddBoardUsage(control, day string, n int) error {
	return s.guard(func() error {
		_, err := s.db.Exec(
			`INSERT INTO board_usage (control, day, n) VALUES (?, ?, ?)
			 ON CONFLICT(control, day) DO UPDATE SET n = board_usage.n + excluded.n`,
			control, day, n)
		return err
	})
}

// BoardUsageTotals is every control that was ever used, most used first.
func (s *Store) BoardUsageTotals() ([]BoardUsage, error) {
	out := []BoardUsage{}
	err := s.guard(func() error {
		out = out[:0]
		rows, err := s.db.Query(
			`SELECT control, SUM(n), MAX(day) FROM board_usage GROUP BY control ORDER BY SUM(n) DESC, control`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var u BoardUsage
			if err := rows.Scan(&u.Control, &u.N, &u.Last); err != nil {
				return err
			}
			out = append(out, u)
		}
		return rows.Err()
	})
	return out, err
}
