package link

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

// EFFICACY, THE ROLL-UPS (item lifecycle L9, docs/rnd/item-lifecycle-design.md sections 12.3 to 12.5). Queries over the derived
// outcomes, so a changed rule is a recompute and never a data fix. Pure: the clock and the zone come in with the query.
//
//   - THE UNIT IS A LANDED ITEM, counted in the period it landed (a day, or a week from Monday, in the hub's zone). Items still
//     in flight count toward nothing here but waste, which is dollars on items given up on, counted when they were.
//   - A DASH IS NOT A ZERO. A figure with nothing to divide by has no value, and a rate over fewer than EffMinItems shows
//     greyed with its count ("2 of 3") and is left out of the trend.
//   - THE TREND IS GOOD OR BAD, not up or down. A change under the noise floor (less than one landed item's worth in a rate, or
//     less than 10 percent in a median) is "same". The current period is partial, so it is compared with the last one
//     SCALED TO THE SAME DAYS, never with a whole one.
//   - EVERY FIGURE SAYS OBSERVED OR REPORTED. It is observed only when all its inputs are, and otherwise says how many of its
//     items leaned on a reported one. ObservedOnly drops every reported input from every figure.

// The figures, in the order the view draws them.
const (
	FigCleanPass = "clean_pass"
	FigFirstPass = "first_pass"
	FigSendBacks = "send_backs"
	FigRedAfter  = "red_after"
	FigBugs      = "bugs"
	FigTimeLand  = "time_to_land"
	FigCost      = "cost"
	FigWaste     = "waste"
)

// EffFigureKeys is every figure, in order.
var EffFigureKeys = []string{FigCleanPass, FigFirstPass, FigSendBacks, FigRedAfter, FigBugs, FigTimeLand, FigCost, FigWaste}

// The units a figure is in.
const (
	UnitPct     = "pct"      // a fraction 0..1 of Den
	UnitPerItem = "per_item" // a count per landed item
	UnitSeconds = "seconds"
	UnitUSD     = "usd"
	UnitCount   = "count" // Num of Den
)

// lowerBetter is the figures for which down is good.
var lowerBetter = map[string]bool{FigSendBacks: true, FigRedAfter: true, FigBugs: true, FigTimeLand: true, FigCost: true, FigWaste: true}

// EffFigure is one roll-up for one period.
type EffFigure struct {
	Key  string `json:"key"`
	Unit string `json:"unit"`
	// Value is nil for a dash: nothing reached the step, so there is nothing to say, which is not zero.
	Value *float64 `json:"value"`
	Num   int      `json:"num"`
	Den   int      `json:"den"`
	// Greyed is a denominator under EffMinItems. The figure is still shown, with its count, and is not trended.
	Greyed bool `json:"greyed"`
	// Source is observed, or reported when ReportedN of the items counted leaned on a reported input.
	Source    string             `json:"source"`
	ReportedN int                `json:"reported_n"`
	Parts     map[string]float64 `json:"parts,omitempty"`
}

// EffPeriod is one day or week.
type EffPeriod struct {
	Key   string `json:"key"`
	Start string `json:"start"`
	End   string `json:"end"`
	// Partial is the current period, drawn open.
	Partial bool `json:"partial"`
	Landed  int  `json:"landed"`
	// TooFew is a period with fewer than EffMinItems landed items: "too few to trend".
	TooFew  bool                 `json:"too_few"`
	Figures map[string]EffFigure `json:"figures"`
}

// EffTrend is one figure this period against the last, to the same days.
type EffTrend struct {
	// Verdict is better, worse, same, too_few or none (nothing to compare).
	Verdict string `json:"verdict"`
	// Dir is the way the number went: up, down or flat.
	Dir   string   `json:"dir"`
	Delta *float64 `json:"delta,omitempty"`
	Text  string   `json:"text"`
	// Was is the figure the comparison was made with, so the view can say "was 0 of 6".
	Was *EffFigure `json:"was,omitempty"`
}

// effView is one item as one query sees it, with the reported inputs dropped when asked.
type effView struct {
	w          hubstore.Work
	o          hubstore.WorkOutcome
	d          EffDetail
	landed     time.Time
	sbDesign   int
	sbReview   int
	sbNotAcc   int
	sbByStep   map[string]int
	firstPass  string
	bugs       int
	reverts    int
	red        bool
	verdict    string
	reported   bool
	stepCost   int64
	coordCost  int64
	costKnown  bool
	abandoned  time.Time
	abandonedC int64
}

// ViewOf is one outcome as a query sees it.
func ViewOf(w hubstore.Work, o hubstore.WorkOutcome, observedOnly bool, now time.Time) *effView {
	d := ParseEffDetail(o)
	v := &effView{w: w, o: o, d: d, landed: rowTime(o.LandedAt), abandoned: rowTime(o.AbandonedAt), sbByStep: map[string]int{},
		stepCost: o.StepCost, coordCost: o.CoordCost, costKnown: o.CostKnown, abandonedC: o.AbandonedCost,
		firstPass: o.FirstPass, red: d.RedEpisodes > 0, reverts: o.Reverted}
	rep := 0
	pick := func(kind string) int {
		n := d.SendBacks[kind]
		if observedOnly {
			n -= d.SendBacksReported[kind]
		}
		rep += d.SendBacksReported[kind]
		return n
	}
	v.sbDesign, v.sbReview, v.sbNotAcc = pick("design"), pick("review"), pick("not_accepted")
	for st, n := range d.ByStep {
		if observedOnly {
			n -= d.ByStepReported[st]
		}
		if n > 0 {
			v.sbByStep[st] = n
		}
	}
	if observedOnly && d.FirstPassReported {
		v.firstPass = ""
	} else if d.FirstPassReported {
		rep++
	}
	for _, b := range d.Bugs {
		if b.Source == hubstore.EvReported {
			rep++
			if observedOnly {
				continue
			}
		}
		v.bugs++
	}
	v.reported = rep > 0 && !observedOnly
	if !v.landed.IsZero() {
		watchEnd := rowTime(o.WatchEndsAt)
		v.verdict, _ = EffVerdict(now, watchEnd, v.sbDesign+v.sbReview+v.sbNotAcc, v.firstPass, v.red, v.bugs, v.reverts, d.CIKnown)
	}
	return v
}

func (v *effView) sendBacks() int { return v.sbDesign + v.sbReview + v.sbNotAcc }

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	if n := len(s); n%2 == 1 {
		return s[n/2]
	}
	n := len(s)
	return (s[n/2-1] + s[n/2]) / 2
}

// percentile is the nearest-rank p (0..1) of xs.
func percentile(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	i := int(math.Ceil(p*float64(len(s)))) - 1
	if i < 0 {
		i = 0
	}
	return s[i]
}

const unitsPerUSD = 1e10

func usd(units int64) float64 { return float64(units) / unitsPerUSD }

func fptr(f float64) *float64 { return &f }

func finish(f EffFigure, reported int, den int) EffFigure {
	f.Den = den
	f.Greyed = den < EffMinItems
	f.Source, f.ReportedN = hubstore.EvObserved, reported
	if reported > 0 {
		f.Source = hubstore.EvReported
	}
	return f
}

// wrongTile is what went wrong after landing in a window, counted by the day it happened.
type wrongTile struct {
	Red, Bugs, Reverts int
	Reported           int
}

// Figures computes every roll-up for the items that landed in [a, b), and the waste of those given up on in it.
func Figures(items []*effView, a, b time.Time) (landed int, figs map[string]EffFigure) {
	figs = map[string]EffFigure{}
	var in []*effView
	for _, v := range items {
		if !v.landed.IsZero() && !v.landed.Before(a) && v.landed.Before(b) {
			in = append(in, v)
		}
	}
	landed = len(in)
	// clean-pass: decided verdicts only (clean or not clean). A watching or unproven item has no answer yet.
	{
		f, rep, den := EffFigure{Key: FigCleanPass, Unit: UnitPct}, 0, 0
		for _, v := range in {
			if v.verdict == VerdictClean || v.verdict == VerdictNotClean {
				den++
				if v.verdict == VerdictClean {
					f.Num++
				}
				if v.reported {
					rep++
				}
			}
		}
		if den > 0 {
			f.Value = fptr(float64(f.Num) / float64(den))
		}
		figs[f.Key] = finish(f, rep, den)
	}
	{
		f, rep, den := EffFigure{Key: FigFirstPass, Unit: UnitPct}, 0, 0
		for _, v := range in {
			if v.firstPass == "" {
				continue
			}
			den++
			if v.firstPass == "pass" {
				f.Num++
			}
			if v.d.FirstPassReported {
				rep++
			}
		}
		if den > 0 {
			f.Value = fptr(float64(f.Num) / float64(den))
		}
		figs[f.Key] = finish(f, rep, den)
	}
	{
		f, rep := EffFigure{Key: FigSendBacks, Unit: UnitPerItem, Parts: map[string]float64{}}, 0
		var d, r, n int
		for _, v := range in {
			d, r, n = d+v.sbDesign, r+v.sbReview, n+v.sbNotAcc
			if v.reported {
				rep++
			}
		}
		if landed > 0 {
			f.Num = d + r + n
			f.Value = fptr(float64(f.Num) / float64(landed))
			f.Parts = map[string]float64{"design": float64(d) / float64(landed), "review": float64(r) / float64(landed), "not_accepted": float64(n) / float64(landed)}
		}
		figs[f.Key] = finish(f, rep, landed)
	}
	{
		f := EffFigure{Key: FigRedAfter, Unit: UnitCount}
		for _, v := range in {
			if v.red {
				f.Num++
			}
		}
		if landed > 0 {
			f.Value = fptr(float64(f.Num) / float64(landed))
		}
		figs[f.Key] = finish(f, 0, landed)
	}
	{
		f, rep := EffFigure{Key: FigBugs, Unit: UnitPerItem, Parts: map[string]float64{}}, 0
		var bugs, rev int
		for _, v := range in {
			bugs, rev = bugs+v.bugs, rev+v.reverts
			if v.reported && v.bugs > 0 {
				rep++
			}
		}
		if landed > 0 {
			f.Num = bugs + rev
			f.Value = fptr(float64(f.Num) / float64(landed))
			f.Parts = map[string]float64{"bugs": float64(bugs), "reverts": float64(rev)}
		}
		figs[f.Key] = finish(f, rep, landed)
	}
	{
		f := EffFigure{Key: FigTimeLand, Unit: UnitSeconds, Parts: map[string]float64{}}
		var tot, wait, ci, work []float64
		for _, v := range in {
			if v.d.TotalSeconds <= 0 {
				continue
			}
			tot = append(tot, float64(v.d.TotalSeconds))
			wait = append(wait, float64(v.o.WaitHumanSeconds))
			ci = append(ci, float64(v.d.TestSeconds))
			work = append(work, float64(v.o.WorkSeconds))
		}
		if len(tot) > 0 {
			f.Value = fptr(median(tot))
			f.Parts = map[string]float64{"wait": median(wait), "ci": median(ci), "work": median(work), "p90": percentile(tot, .9)}
		}
		figs[f.Key] = finish(f, 0, len(tot))
	}
	{
		f := EffFigure{Key: FigCost, Unit: UnitUSD, Parts: map[string]float64{}}
		var steps, coord, share []float64
		for _, v := range in {
			if !v.costKnown {
				continue
			}
			steps, coord = append(steps, usd(v.stepCost)), append(coord, usd(v.coordCost))
			if all := v.stepCost + v.coordCost; all > 0 {
				share = append(share, float64(v.coordCost)/float64(all))
			}
		}
		if len(steps) > 0 {
			f.Value = fptr(median(steps))
			f.Parts = map[string]float64{"coord": median(coord), "share": median(share)}
		}
		figs[f.Key] = finish(f, 0, len(steps))
	}
	{
		f := EffFigure{Key: FigWaste, Unit: UnitUSD, Parts: map[string]float64{}}
		var waste, all int64
		var n int
		for _, v := range items {
			if !v.abandoned.IsZero() && !v.abandoned.Before(a) && v.abandoned.Before(b) && v.costKnown {
				waste += v.abandonedC
				n++
			}
		}
		for _, v := range in {
			if v.costKnown {
				all += v.stepCost + v.coordCost
			}
		}
		all += waste
		f.Num = n
		if n > 0 || landed > 0 {
			f.Value = fptr(usd(waste))
			share := 0.0
			if all > 0 {
				share = float64(waste) / float64(all)
			}
			f.Parts = map[string]float64{"items": float64(n), "share": share}
		}
		figs[f.Key] = finish(f, 0, landed)
	}
	return landed, figs
}

// TrendOf compares a figure with the one before it, to the same days.
func TrendOf(this, last EffFigure, thisLanded, lastLanded int) EffTrend {
	t := EffTrend{Verdict: "none", Dir: "flat"}
	if this.Value == nil || last.Value == nil {
		return t
	}
	w := last
	t.Was = &w
	if thisLanded < EffMinItems || lastLanded < EffMinItems || this.Greyed || last.Greyed {
		t.Verdict, t.Text = "too_few", "too few to trend"
		return t
	}
	delta := *this.Value - *last.Value
	t.Delta = fptr(delta)
	switch {
	case delta > 0:
		t.Dir = "up"
	case delta < 0:
		t.Dir = "down"
	}
	noise := 0.0
	switch this.Unit {
	case UnitPct, UnitCount, UnitPerItem:
		noise = 1 / float64(this.Den)
	default:
		noise = 0.10 * math.Abs(*last.Value)
	}
	if math.Abs(delta) < noise-1e-9 || delta == 0 {
		t.Verdict = "same"
		return t
	}
	if (delta < 0) == lowerBetter[this.Key] {
		t.Verdict = "better"
	} else {
		t.Verdict = "worse"
	}
	return t
}

// periodStart is the start of the day or Monday-based week holding t, in t's zone.
func periodStart(t time.Time, by string) time.Time {
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	if by == "week" {
		d = d.AddDate(0, 0, -((int(d.Weekday()) + 6) % 7))
	}
	return d
}

func periodNext(t time.Time, by string) time.Time {
	if by == "week" {
		return t.AddDate(0, 0, 7)
	}
	return t.AddDate(0, 0, 1)
}

func periodPrev(t time.Time, by string) time.Time {
	if by == "week" {
		return t.AddDate(0, 0, -7)
	}
	return t.AddDate(0, 0, -1)
}

func periodKey(t time.Time, by string) string {
	if by == "week" {
		y, w := t.ISOWeek()
		return fmt.Sprintf("%d-W%02d", y, w)
	}
	return t.Format("2006-01-02")
}
