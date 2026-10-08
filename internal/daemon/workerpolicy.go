package daemon

import (
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/dovholuknf/atrium/internal/store"
)

// THE WORKER DEFAULTS. Two settings the hub owns and hands to every room, both off until somebody sets them. See
// store/workerpolicy.go.
//
//   - A DEFAULT MODEL. An agent-launched card started with no model gets it. A launch that names a model wins, a card
//     that already has one keeps it, a resume is left alone, and a card the operator starts is never touched.
//   - A BUDGET in dollars at list price, the same prices the turn cost uses. A card that has spent past it has its
//     launcher told once and shows it on its own card. IT NEVER STOPS, PARKS OR EXITS THE CARD: a budget that kills
//     a worker mid-edit costs more than it saves, and the number is only an estimate.

// NoticeBudget is a worker past the budget. One per worker: the key never changes, so a card is told about once.
const NoticeBudget = "budget"

// workerDefaultModel is the model an agent-launched fresh launch gets when it named none, or "" to leave the
// runner's own default.
func (d *Daemon) workerDefaultModel(h *store.Harness, req LaunchRequest, task *store.Task, reportTo string) string {
	if req.Resume != "" || task != nil || !isClaude(h) {
		return ""
	}
	if !hasTag(req.Tags, OriginAgentTag) && reportTo == "" {
		return ""
	}
	return d.st.WorkerPolicy().Model
}

// budgetMarks is the cards found past the budget, kept in memory and worked out again from the ledger at the next row.
type budgetMarks struct {
	mu sync.Mutex
	by map[string]*BudgetView
}

// BudgetView is what a card past its budget shows.
type BudgetView struct {
	SpentUSD  float64 `json:"spent_usd"`
	BudgetUSD float64 `json:"budget_usd"`
	Text      string  `json:"text"`
}

func (m *budgetMarks) put(id string, v *BudgetView) (changed bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	old := m.by[id]
	if v == nil {
		delete(m.by, id)
		return old != nil
	}
	if m.by == nil {
		m.by = map[string]*BudgetView{}
	}
	m.by[id] = v
	return old == nil || old.BudgetUSD != v.BudgetUSD
}

// budgetFor adapts the marks to what the api package expects, with an untyped nil for none.
func (d *Daemon) budgetFor(taskID string) any {
	d.budgets.mu.Lock()
	defer d.budgets.mu.Unlock()
	if v := d.budgets.by[taskID]; v != nil {
		out := *v
		return &out
	}
	return nil
}

// cardSpendUSD is what a card has spent at list price, each row priced on its model. A model with no price counts at
// zero, as the turn cost does.
func (d *Daemon) cardSpendUSD(taskID string) (float64, error) {
	rows, err := d.st.SessionUsageByModel(taskID)
	if err != nil {
		return 0, err
	}
	total := 0.0
	for _, r := range rows {
		if p, ok := usagePriceFor(r.Model); ok {
			total += usageCost(&store.SessionUsage{Input: r.Input, Output: r.Output, CacheRead: r.CacheRead,
				CacheWrite5m: r.CacheWrite5m, CacheWrite1h: r.CacheWrite1h}, p)
		}
	}
	return total, nil
}

// checkBudget is looked at as each row of a card's spend lands. Best effort and never on anybody's path: a failure is
// logged and the next row looks again.
func (d *Daemon) checkBudget(t *store.Task) {
	if d == nil || t == nil || d.usage == nil {
		return
	}
	limit := d.st.WorkerPolicy().BudgetUSD
	if limit <= 0 || !t.Launched() {
		if d.budgets.put(t.ID, nil) {
			d.publishTask(t.ID)
		}
		return
	}
	spent, err := d.cardSpendUSD(t.ID)
	if err != nil {
		log.Printf("[atrium] could not total the spend of %s: %v", t.ID, err)
		return
	}
	if spent < limit {
		if d.budgets.put(t.ID, nil) {
			d.publishTask(t.ID)
		}
		return
	}
	text := fmt.Sprintf("%s has spent about $%.2f, past the $%.2f worker budget, at list price", t.WireName, spent, limit)
	if d.budgets.put(t.ID, &BudgetView{SpentUSD: spent, BudgetUSD: limit, Text: text}) {
		d.publishTask(t.ID)
	}
	// Once per card, however many rows follow. A report, never an action.
	d.notifyLauncher(t, NoticeBudget, "budget", strings.TrimSpace(text)+". a report, not an action. card "+t.ID)
}
