package link

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/base/tokenprice"
	"github.com/dovholuknf/atrium/internal/hubstore"
)

// WHAT AN ITEM COST, THE PURE PART (item lifecycle L5, docs/rnd/item-lifecycle-design.md section 6). ItemCost reads the
// rows the hub ingested (workusage.go), the item's steps and its cards, and answers per step and for coordination.
// It reads no clock and no store, so a table feeds it and a repricing is a recompute.
//
//   - THE HEADLINE IS THE STEPS ALONE, and is labelled "+ coordination" wherever it is shown. The sum is in the detail.
//   - A DOLLAR FIGURE IS AN ESTIMATE at list price, says so, and names the price table's version. It is worked out from
//     the real token counts per type on every read and stored nowhere. The tokens are always beside it.
//   - A MODEL WITH NO PRICE SHOWS `?`, never `$0.00`, and a step with no spend at all shows `-`.
//   - A ROW IS DIVIDED ACROSS STEPS IN THE TIME RATIO (6.4) with no token lost, in integers, so the steps plus
//     coordination equal the sum of the rows to the cent.

// stepWindow is one step's time as a card may be on it.
type stepWindow struct {
	step       string
	start, end time.Time
}

// rowTime parses a stored time, the zero time when it is not one.
func rowTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(s))
	if err != nil {
		return time.Time{}
	}
	return t
}

// windowsByCard is, for each card of the item, the steps it is on that have a window, in step order. A card is on the
// step its launch named, and on any step whose `cards` lists it (a builder on `tested`).
func windowsByCard(cards []hubstore.WorkCard, steps []hubstore.WorkStepRow, now time.Time) (wins map[string][]stepWindow, home map[string]string) {
	wins, home = map[string][]stepWindow{}, map[string]string{}
	on := map[string]map[string]bool{}
	mark := func(card, step string) {
		if on[card] == nil {
			on[card] = map[string]bool{}
		}
		on[card][step] = true
	}
	for _, c := range cards {
		k := cardKey(c.Room, c.Card)
		home[k] = StepOfCard(c)
		mark(k, home[k])
	}
	for _, s := range steps {
		var listed []string
		_ = json.Unmarshal([]byte(s.Cards), &listed)
		for _, k := range listed {
			if _, ok := home[k]; ok {
				mark(k, s.Step)
			}
		}
	}
	for _, name := range hubstore.Steps {
		for _, s := range steps {
			if s.Step != name || s.State == hubstore.StepSkipped || s.StartedAt == "" {
				continue
			}
			start := rowTime(s.StartedAt)
			if start.IsZero() {
				continue
			}
			end := rowTime(s.EndedAt)
			if end.IsZero() || end.Before(start) {
				end = now
				if end.Before(start) {
					end = start
				}
			}
			for k := range home {
				if on[k][name] {
					wins[k] = append(wins[k], stepWindow{step: name, start: start, end: end})
				}
			}
		}
	}
	return wins, home
}

// SplitRow says which steps a row of one card belongs to and in what ratio, the weights in milliseconds of the row's own
// time spent in each step (6.4). A row inside one step is that step alone. A row that crosses a boundary is divided in
// the time ratio. A row inside no window goes to the nearest step the card has, the first before them all and the last
// after. A card with no window at all is on its home step.
func SplitRow(started, ended time.Time, wins []stepWindow, home string) (steps []string, weights []int64) {
	if ended.Before(started) {
		started, ended = ended, started
	}
	if len(wins) == 0 {
		return []string{home}, []int64{1}
	}
	if !ended.After(started) {
		// A moment, not a span: the first step whose window holds it.
		for _, w := range wins {
			if !started.Before(w.start) && !started.After(w.end) {
				return []string{w.step}, []int64{1}
			}
		}
	} else {
		var total int64
		ws := make([]int64, len(wins))
		for i, w := range wins {
			lo, hi := started, ended
			if w.start.After(lo) {
				lo = w.start
			}
			if w.end.Before(hi) {
				hi = w.end
			}
			if hi.After(lo) {
				ws[i] = hi.Sub(lo).Milliseconds()
				if ws[i] == 0 {
					ws[i] = 1
				}
				total += ws[i]
			}
		}
		if total > 0 {
			// One entry per step: two windows of one step (it was reopened) are one part.
			idx := map[string]int{}
			for i, w := range wins {
				if ws[i] == 0 {
					continue
				}
				if j, ok := idx[w.step]; ok {
					weights[j] += ws[i]
					continue
				}
				idx[w.step] = len(steps)
				steps = append(steps, w.step)
				weights = append(weights, ws[i])
			}
			return steps, weights
		}
	}
	// Outside every window: the nearest step the card has.
	best, bestGap := wins[0].step, time.Duration(math.MaxInt64)
	for _, w := range wins {
		var gap time.Duration
		switch {
		case ended.Before(w.start):
			gap = w.start.Sub(ended)
		case started.After(w.end):
			gap = started.Sub(w.end)
		}
		if gap < bestGap {
			best, bestGap = w.step, gap
		}
	}
	return []string{best}, []int64{1}
}

// CostLine is one sum, as the board reads it.
type CostLine struct {
	Tokens tokenprice.Tokens `json:"tokens"`
	// TotalTokens is every token of every type, so a bar is drawn without adding.
	TotalTokens int64 `json:"total_tokens"`
	// Usd is the dollars of what was priced, a number to compute with. Display is what to show: `-`, `?` or `$1.23`.
	Usd   float64 `json:"usd"`
	Cents int64   `json:"cents"`
	// Units is the amount exactly, in 1e-10 dollar, which is what adds: steps plus coordination are the total in Units
	// to the last digit, where each rounded to the cent may differ from the rounded total by one.
	Units   int64  `json:"units"`
	Display string `json:"display"`
	// Known is false when some of the tokens are of a model with no price, which Display says as `?`.
	Known          bool     `json:"known"`
	UnpricedTokens int64    `json:"unpriced_tokens,omitempty"`
	UnpricedModels []string `json:"unpriced_models,omitempty"`
}

func lineOf(p tokenprice.Priced) CostLine {
	return CostLine{Tokens: p.Tokens, TotalTokens: p.Tokens.Total(), Usd: float64(p.Cost.Cents()) / 100,
		Cents: p.Cost.Cents(), Units: int64(p.Cost), Display: p.Display(), Known: p.Known(), UnpricedTokens: p.Unpriced, UnpricedModels: p.Models}
}

// StepCost is one step's spend.
type StepCost struct {
	// Name, not `step`: an item's detail holds this beside its own steps, and a reader looking for one must not find the other.
	Step string `json:"name"`
	CostLine
}

// CostView is what an item cost.
type CostView struct {
	// Estimated is always true: list price for the tokens, not a bill.
	Estimated bool `json:"estimated"`
	// Prices is the version of the table the dollars were worked out on.
	Prices string `json:"prices"`
	// Steps is all eight, in order, `-` where nothing was spent.
	Steps []StepCost `json:"steps"`
	// Headline is the steps alone, to be shown with HeadlineSuffix.
	Headline       CostLine `json:"headline"`
	HeadlineSuffix string   `json:"headline_suffix"`
	// Coordination is the relay and the orchestrator's spend because the item exists, never in a step.
	Coordination CostLine `json:"coordination"`
	// Total is the steps plus coordination, for the detail.
	Total CostLine `json:"total"`
	// Summary is the sentence the page may print as it stands.
	Summary string `json:"summary"`
	Rows    int    `json:"rows"`
}

// ItemCost prices an item's rows and divides them across its steps.
func ItemCost(tb tokenprice.Table, cards []hubstore.WorkCard, steps []hubstore.WorkStepRow, rows []hubstore.WorkUsageRow, now time.Time) CostView {
	wins, home := windowsByCard(cards, steps, now)
	per := map[string]*tokenprice.Priced{}
	for _, s := range hubstore.Steps {
		per[s] = &tokenprice.Priced{}
	}
	var coord, all tokenprice.Priced
	for _, r := range rows {
		at := r.EndedAt
		all.Add(tb, r.Model, at, r.Tokens)
		if r.Class == hubstore.ClassCoord {
			coord.Add(tb, r.Model, at, r.Tokens)
			continue
		}
		k := cardKey(r.Room, r.Card)
		h := home[k]
		if h == "" {
			h = hubstore.StepBuilding
		}
		names, weights := SplitRow(rowTime(r.StartedAt), rowTime(r.EndedAt), wins[k], h)
		for i, part := range tokenprice.Divide(r.Tokens, weights) {
			if part.Zero() {
				continue
			}
			p := per[names[i]]
			if p == nil {
				p = per[hubstore.StepBuilding]
			}
			p.Add(tb, r.Model, at, part)
		}
	}
	v := CostView{Estimated: true, Prices: tb.Version(), Rows: len(rows), HeadlineSuffix: "+ coordination"}
	var head tokenprice.Priced
	for _, s := range hubstore.Steps {
		v.Steps = append(v.Steps, StepCost{Step: s, CostLine: lineOf(*per[s])})
		head.Merge(*per[s])
	}
	v.Headline, v.Coordination, v.Total = lineOf(head), lineOf(coord), lineOf(all)
	v.Summary = fmt.Sprintf("%s %s %s coordination, estimated at list price (%s)", v.Headline.Display, "+", v.Coordination.Display, v.Prices)
	return v
}

// equalWeights is n parts of one weight, for a turn divided equally among items.
func equalWeights(n int) []int64 {
	w := make([]int64, n)
	for i := range w {
		w[i] = 1
	}
	return w
}

// sortedKeys is a map's keys in order.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// costOf is an item's cost as it stands, from the rows the hub holds. An unreadable store answers an empty view with no
// rows, which shows `-` everywhere rather than a made-up zero.
func (p *Proxy) costOf(st *hubstore.Store, id string, cards []hubstore.WorkCard, steps []hubstore.WorkStepRow) CostView {
	tb, _ := st.Prices()
	rows, _ := st.WorkUsage(id)
	return ItemCost(tb, cards, steps, rows, time.Now())
}

// writeWorkCost answers GET /_hub/work/<id>/cost.
func (p *Proxy) writeWorkCost(w http.ResponseWriter, st *hubstore.Store, id string) {
	wk, err := st.WorkByID(id)
	if err != nil {
		wfail(w, err)
		return
	}
	cards, err := st.WorkCards(id)
	if err != nil {
		wfail(w, err)
		return
	}
	steps, err := st.WorkSteps(id)
	if err != nil {
		wfail(w, err)
		return
	}
	crJSON(w, http.StatusOK, map[string]any{"backlog_id": wk.BacklogID, "cost": p.costOf(st, id, cards, steps)})
}

// writeWorkCosts answers GET /_hub/work/cost: every item, with one read of the rows and one of the prices.
func (p *Proxy) writeWorkCosts(w http.ResponseWriter, st *hubstore.Store) {
	list, err := st.WorkList(0)
	if err != nil {
		wfail(w, err)
		return
	}
	tb, err := st.Prices()
	if err != nil {
		wfail(w, err)
		return
	}
	rows, err := st.AllWorkUsage()
	if err != nil {
		wfail(w, err)
		return
	}
	steps, err := st.AllWorkSteps()
	if err != nil {
		wfail(w, err)
		return
	}
	type line struct {
		BacklogID string   `json:"backlog_id"`
		Title     string   `json:"title"`
		Cost      CostView `json:"cost"`
	}
	out := make([]line, 0, len(list))
	for _, wk := range list {
		cards, _ := st.WorkCards(wk.BacklogID)
		out = append(out, line{wk.BacklogID, wk.Title, ItemCost(tb, cards, steps[wk.BacklogID], rows[wk.BacklogID], time.Now())})
	}
	crJSON(w, http.StatusOK, map[string]any{"estimated": true, "prices": tb.Version(), "items": out})
}

// servePrices answers GET /_hub/prices: the table the dollars are worked out on.
func (p *Proxy) servePrices(w http.ResponseWriter, r *http.Request) {
	st := p.itemStore()
	if st == nil {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		crFail(w, http.StatusMethodNotAllowed, "the price table is read here")
		return
	}
	tb, err := st.Prices()
	if err != nil {
		wfail(w, err)
		return
	}
	if tb.Rows == nil {
		tb.Rows = []tokenprice.Price{}
	}
	crJSON(w, http.StatusOK, map[string]any{"estimated": true, "version": tb.Version(), "per": "million tokens", "prices": tb.Rows})
}
