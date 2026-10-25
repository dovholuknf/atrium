// Package tokenprice prices tokens: one row per model, the per-model matcher, and the arithmetic that keeps a sum of
// parts equal to the whole.
//
// A DOLLAR FIGURE IS NEVER STORED. It is worked out from the real token counts per type (input, cache write 5m, cache
// write 1h, cache read, output) at the moment someone reads it, so a repricing is a recompute and tokens are never
// hidden behind dollars. It is an ESTIMATE: list price, which is what the same tokens would cost off a subscription.
// See docs/rnd/item-lifecycle-design.md section 6.
//
// THE MONEY IS INTEGER. A price is dollars per million tokens to at most four places, so a row's cost is
// `tokens * price` in units of 1e-10 dollar with no division at all. Integers add exactly, so the steps of an item
// plus its coordination equal the sum of its rows to the cent, and a row divided across steps loses nothing.
package tokenprice

import (
	"math"
	"math/big"
	"sort"
	"strings"
)

// Units is how many Money units make a dollar.
const Units = 1e10

// Money is an amount in units of 1e-10 dollar.
type Money int64

// Dollars is the amount as a float, for display only. Never add these, add Money.
func (m Money) Dollars() float64 { return float64(m) / Units }

// Cents is the amount rounded to the cent, half away from zero.
func (m Money) Cents() int64 {
	const perCent = Units / 100
	if m < 0 {
		return -int64((-m + perCent/2) / perCent)
	}
	return int64((m + perCent/2) / perCent)
}

// Tokens is a count per type.
type Tokens struct {
	Input        int64 `json:"input"`
	CacheWrite5m int64 `json:"cache_write_5m"`
	CacheWrite1h int64 `json:"cache_write_1h"`
	CacheRead    int64 `json:"cache_read"`
	Output       int64 `json:"output"`
}

// Add is the sum of two counts.
func (t Tokens) Add(o Tokens) Tokens {
	return Tokens{t.Input + o.Input, t.CacheWrite5m + o.CacheWrite5m, t.CacheWrite1h + o.CacheWrite1h,
		t.CacheRead + o.CacheRead, t.Output + o.Output}
}

// Total is every token, of every type.
func (t Tokens) Total() int64 {
	return t.Input + t.CacheWrite5m + t.CacheWrite1h + t.CacheRead + t.Output
}

// Zero is whether nothing was spent.
func (t Tokens) Zero() bool { return t == Tokens{} }

// Price is one model's list price, in dollars per million tokens, as it is written down.
type Price struct {
	Model string `json:"model"`
	// ValidFrom is an RFC 3339 time. The row in force for a turn is the newest one at or before the turn.
	ValidFrom    string  `json:"valid_from"`
	Input        float64 `json:"input"`
	CacheWrite5m float64 `json:"cache_write_5m"`
	CacheWrite1h float64 `json:"cache_write_1h"`
	CacheRead    float64 `json:"cache_read"`
	Output       float64 `json:"output"`
}

// perM is a price as an integer count of 1e-4 dollar per million tokens, which is the unit that makes a cost an exact
// product. Rounded, so 1.25 and 0.1 are what they say.
func perM(d float64) int64 { return int64(math.Round(d * 1e4)) }

// Cost prices a count. tokens times (1e-4 dollar per million) is 1e-10 dollar, so there is nothing to divide.
func (p Price) Cost(t Tokens) Money {
	return Money(t.Input*perM(p.Input) + t.CacheWrite5m*perM(p.CacheWrite5m) + t.CacheWrite1h*perM(p.CacheWrite1h) +
		t.CacheRead*perM(p.CacheRead) + t.Output*perM(p.Output))
}

// Table is every price row, in no order.
type Table struct {
	Rows []Price
}

// Version names the table: the newest ValidFrom in it, so a row added to it changes what the page says.
func (tb Table) Version() string {
	v := ""
	for _, r := range tb.Rows {
		if r.ValidFrom > v {
			v = r.ValidFrom
		}
	}
	if v == "" {
		return "none"
	}
	if i := strings.IndexByte(v, 'T'); i > 0 {
		v = v[:i]
	}
	return "prices-" + v
}

// Normal is a model id as the matcher reads it: lower case, with a `[1m]` style suffix dropped.
func Normal(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	if i := strings.IndexByte(m, '['); i >= 0 {
		m = strings.TrimSpace(m[:i])
	}
	return m
}

// Matches is whether a model id is the priced name, alone or with an eight digit date stamp. It is deliberately not a
// prefix match: `claude-sonnet-5` is not `claude-sonnet-5-5`, and taking one for the other would price a turn wrongly
// and say nothing.
func Matches(model, name string) bool {
	m := Normal(model)
	if m == name {
		return true
	}
	date, ok := strings.CutPrefix(m, name+"-")
	return ok && len(date) == 8 && strings.Trim(date, "0123456789") == ""
}

// For is the price of a model for a turn that ended at `at` (RFC 3339, "" for now), and whether there is one. A model
// with no row is NOT priced at zero: ok is false and the caller shows `?`. When every row of the model starts after the
// turn, the earliest row stands in, because a price that did not exist yet is better than none.
func (tb Table) For(model, at string) (Price, bool) {
	var best, earliest *Price
	for i := range tb.Rows {
		r := &tb.Rows[i]
		if !Matches(model, r.Model) {
			continue
		}
		if earliest == nil || r.ValidFrom < earliest.ValidFrom {
			earliest = r
		}
		if at == "" || r.ValidFrom <= at {
			if best == nil || r.ValidFrom > best.ValidFrom {
				best = r
			}
		}
	}
	if best == nil {
		best = earliest
	}
	if best == nil {
		return Price{}, false
	}
	return *best, true
}

// Priced is what a sum of turns came to. Unpriced counts the tokens of models the table has no row for: they are in
// Tokens and not in Cost, and Models names them, so a reader says `?` and not `$0.00`.
type Priced struct {
	Tokens   Tokens   `json:"tokens"`
	Cost     Money    `json:"-"`
	Unpriced int64    `json:"unpriced_tokens,omitempty"`
	Models   []string `json:"unpriced_models,omitempty"`
}

// Add prices one more count of a model and adds it in.
func (p *Priced) Add(tb Table, model, at string, t Tokens) {
	p.Tokens = p.Tokens.Add(t)
	if pr, ok := tb.For(model, at); ok {
		p.Cost += pr.Cost(t)
		return
	}
	p.Unpriced += t.Total()
	m := Normal(model)
	if m == "" {
		m = "(no model)"
	}
	i := sort.SearchStrings(p.Models, m)
	if i == len(p.Models) || p.Models[i] != m {
		p.Models = append(p.Models, "")
		copy(p.Models[i+1:], p.Models[i:])
		p.Models[i] = m
	}
}

// Merge is the sum of two.
func (p *Priced) Merge(o Priced) {
	p.Tokens = p.Tokens.Add(o.Tokens)
	p.Cost += o.Cost
	p.Unpriced += o.Unpriced
	for _, m := range o.Models {
		i := sort.SearchStrings(p.Models, m)
		if i == len(p.Models) || p.Models[i] != m {
			p.Models = append(p.Models, "")
			copy(p.Models[i+1:], p.Models[i:])
			p.Models[i] = m
		}
	}
}

// Known is whether every token in it was priced.
func (p Priced) Known() bool { return p.Unpriced == 0 }

// Display is how the dollars read: `-` for no tokens at all (so no spend yet and spent nothing differ), `?` for a sum
// that holds tokens of a model with no price, else dollars to the cent.
func (p Priced) Display() string {
	switch {
	case p.Tokens.Zero():
		return "-"
	case p.Unpriced > 0:
		return "?"
	}
	return FormatCents(p.Cost.Cents())
}

// FormatCents is 1234 as `$12.34`.
func FormatCents(c int64) string {
	sign := ""
	if c < 0 {
		sign, c = "-", -c
	}
	d, r := c/100, c%100
	return sign + "$" + itoa(d) + "." + string([]byte{byte('0' + r/10), byte('0' + r%10)})
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// Defaults are the rows a hub starts with: the two the room already prices, the 5 minute write at 1.25 times input
// (Anthropic's list rule), and the Opus and Fable rows copied from the room's keep-alive table. Per million tokens.
func Defaults(validFrom string) []Price {
	row := func(model string, in, w1h, cr, out float64) Price {
		return Price{Model: model, ValidFrom: validFrom, Input: in, CacheWrite5m: math.Round(in*1.25*1e4) / 1e4,
			CacheWrite1h: w1h, CacheRead: cr, Output: out}
	}
	return []Price{
		row("claude-haiku-4-5", 1, 2, 0.10, 5),
		row("claude-sonnet-5", 2, 4, 0.20, 10),
		row("claude-sonnet-5-5", 2, 4, 0.20, 10),
		row("claude-opus-5-5", 4, 8, 0.20, 20),
		row("claude-fable-5-1", 10, 20, 0.25, 50),
	}
}

// Divide cuts a count across parts in proportion to their weights, and LOSES NO TOKEN: each type is divided with the
// floor of its share, and what the flooring left over goes to the heaviest part (the first of equals), so the parts sum
// to the whole exactly. Weights are not all zero for a caller that means to divide: when they are, the first part takes
// everything.
func Divide(t Tokens, weights []int64) []Tokens {
	out := make([]Tokens, len(weights))
	if len(weights) == 0 {
		return out
	}
	var sum big.Int
	heavy := 0
	for i, w := range weights {
		if w < 0 {
			w = 0
		}
		sum.Add(&sum, big.NewInt(w))
		if w > weights[heavy] {
			heavy = i
		}
	}
	if sum.Sign() == 0 {
		out[0] = t
		return out
	}
	each := func(total int64, set func(*Tokens, int64)) {
		var left = total
		for i, w := range weights {
			if w < 0 {
				w = 0
			}
			share := new(big.Int).Mul(big.NewInt(total), big.NewInt(w))
			share.Quo(share, &sum)
			set(&out[i], share.Int64())
			left -= share.Int64()
		}
		var h Tokens
		set(&h, left)
		out[heavy] = out[heavy].Add(h)
	}
	each(t.Input, func(x *Tokens, n int64) { x.Input += n })
	each(t.CacheWrite5m, func(x *Tokens, n int64) { x.CacheWrite5m += n })
	each(t.CacheWrite1h, func(x *Tokens, n int64) { x.CacheWrite1h += n })
	each(t.CacheRead, func(x *Tokens, n int64) { x.CacheRead += n })
	each(t.Output, func(x *Tokens, n int64) { x.Output += n })
	return out
}
