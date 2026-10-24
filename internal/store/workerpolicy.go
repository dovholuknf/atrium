package store

import (
	"github.com/dovholuknf/atrium/internal/rules"
)

// WorkerPolicy reads the policy. Unset or unreadable is off.
func (s *Store) WorkerPolicy() rules.WorkerPolicy {
	v, err := s.Setting(rules.SettingWorkerPolicy)
	if err != nil {
		return rules.WorkerPolicy{}
	}
	return rules.ParseWorkerPolicy(v)
}

// UsageByModel is a card's token spend summed per model, for pricing. A row's model is the last reply's.
type UsageByModel struct {
	Model string
	UsageTotals
}

// SessionUsageByModel sums a card's rows per model.
func (s *Store) SessionUsageByModel(taskID string) ([]UsageByModel, error) {
	var out []UsageByModel
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(`SELECT model, COALESCE(SUM(input), 0), COALESCE(SUM(output), 0),
			COALESCE(SUM(cache_write_5m), 0), COALESCE(SUM(cache_write_1h), 0), COALESCE(SUM(cache_read), 0)
			FROM session_usage WHERE task_id = ? GROUP BY model`, taskID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var u UsageByModel
			if err := rows.Scan(&u.Model, &u.Input, &u.Output, &u.CacheWrite5m, &u.CacheWrite1h, &u.CacheRead); err != nil {
				return err
			}
			out = append(out, u)
		}
		return rows.Err()
	})
	return out, err
}
