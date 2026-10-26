package rules

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
	// InterviewModel is the model an interview launch (kind interview) gets when it names none. Empty means opus.
	InterviewModel string `json:"interview_model,omitempty"`
}

// MaxWorkerBudgetUSD is the largest budget taken, so a typo is refused rather than quietly meaning never.
const MaxWorkerBudgetUSD = 100000

// CheckWorkerPolicy validates a typed policy and returns it as it is stored. Both off is "".
func CheckWorkerPolicy(in WorkerPolicy) (string, error) {
	in.Model = strings.TrimSpace(in.Model)
	if len(in.Model) > 80 || strings.ContainsAny(in.Model, " \t\r\n\"'`;|&<>$") {
		return "", fmt.Errorf("worker_policy model is a model name like sonnet or claude-sonnet-5-5, not %q", in.Model)
	}
	in.InterviewModel = strings.TrimSpace(in.InterviewModel)
	if len(in.InterviewModel) > 80 || strings.ContainsAny(in.InterviewModel, " \t\r\n\"'`;|&<>$") {
		return "", fmt.Errorf("worker_policy interview_model is a model name like opus or claude-opus-5-5, not %q",
			in.InterviewModel)
	}
	if math.IsNaN(in.BudgetUSD) || in.BudgetUSD < 0 || in.BudgetUSD > MaxWorkerBudgetUSD {
		return "", fmt.Errorf("worker_policy budget_usd takes dollars from 0 (off) to %d, not %v", MaxWorkerBudgetUSD,
			in.BudgetUSD)
	}
	in.BudgetUSD = math.Round(in.BudgetUSD*100) / 100
	if in.Model == "" && in.BudgetUSD == 0 && in.InterviewModel == "" {
		return "", nil
	}
	b, err := json.Marshal(in)
	return string(b), err
}

// ParseWorkerPolicy reads a stored policy. Unset or unreadable is off.
func ParseWorkerPolicy(v string) WorkerPolicy {
	if strings.TrimSpace(v) == "" {
		return WorkerPolicy{}
	}
	var p WorkerPolicy
	if json.Unmarshal([]byte(v), &p) != nil {
		return WorkerPolicy{}
	}
	p.Model = strings.TrimSpace(p.Model)
	p.InterviewModel = strings.TrimSpace(p.InterviewModel)
	if math.IsNaN(p.BudgetUSD) || p.BudgetUSD < 0 || p.BudgetUSD > MaxWorkerBudgetUSD {
		p.BudgetUSD = 0
	}
	return p
}
