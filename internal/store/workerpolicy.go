package store

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// SettingWorkerPolicy is what an agent-launched card is held to, a JSON object. The hub owns it and hands it to
// every room, the way it does context_limits. Empty is both off.
const SettingWorkerPolicy = "worker_policy"

// WorkerPolicy is the two worker settings. The zero value is off for both.
type WorkerPolicy struct {
	// Model is the model an agent-launched card gets when its launch names none. Empty leaves the runner's own
	// default, which is today's behaviour. A launch that names a model always wins, and a card the operator starts
	// is never given one.
	Model string `json:"model"`
	// BudgetUSD is the dollars, at list price, an agent-launched card may spend before its launcher is told once.
	// Zero is off. Nothing is ever stopped by it.
	BudgetUSD float64 `json:"budget_usd"`
}

// MaxWorkerBudgetUSD is the largest budget taken, so a typo is refused rather than quietly meaning never.
const MaxWorkerBudgetUSD = 100000

// WorkerPolicy reads the policy. Unset or unreadable is off.
func (s *Store) WorkerPolicy() WorkerPolicy {
	v, err := s.Setting(SettingWorkerPolicy)
	if err != nil || strings.TrimSpace(v) == "" {
		return WorkerPolicy{}
	}
	var p WorkerPolicy
	if json.Unmarshal([]byte(v), &p) != nil {
		return WorkerPolicy{}
	}
	p.Model = strings.TrimSpace(p.Model)
	if math.IsNaN(p.BudgetUSD) || p.BudgetUSD < 0 || p.BudgetUSD > MaxWorkerBudgetUSD {
		p.BudgetUSD = 0
	}
	return p
}

// CheckWorkerPolicy validates a typed policy and returns it as it is stored. Both off is "".
func CheckWorkerPolicy(in WorkerPolicy) (string, error) {
	in.Model = strings.TrimSpace(in.Model)
	if len(in.Model) > 80 || strings.ContainsAny(in.Model, " \t\r\n\"'`;|&<>$") {
		return "", fmt.Errorf("worker_policy model is a model name like sonnet or claude-sonnet-5-5, not %q", in.Model)
	}
	if math.IsNaN(in.BudgetUSD) || in.BudgetUSD < 0 || in.BudgetUSD > MaxWorkerBudgetUSD {
		return "", fmt.Errorf("worker_policy budget_usd takes dollars from 0 (off) to %d, not %v", MaxWorkerBudgetUSD,
			in.BudgetUSD)
	}
	in.BudgetUSD = math.Round(in.BudgetUSD*100) / 100
	if in.Model == "" && in.BudgetUSD == 0 {
		return "", nil
	}
	b, err := json.Marshal(in)
	return string(b), err
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
