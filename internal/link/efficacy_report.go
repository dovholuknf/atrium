package link

import (
	"sort"
	"strconv"
	"time"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

// THE EFFICACY VIEW'S ANSWER (docs/rnd/item-lifecycle-design.md section 12), built from the outcomes the collect derived. Pure:
// it reads no store, so a test hands it rows and a clock.

// EffReportIn is what a report is built from.
type EffReportIn struct {
	Works        []hubstore.Work
	Outcomes     map[string]hubstore.WorkOutcome
	Events       map[string][]hubstore.WorkEvent
	By           string // day or week
	From, To     time.Time
	ObservedOnly bool
	// Now carries the zone: days and weeks are cut in it.
	Now time.Time
}

// EffWrong is the tile of what went wrong after landing, by the day it happened.
type EffWrong struct {
	Red      int    `json:"red"`
	Bugs     int    `json:"bugs"`
	Reverts  int    `json:"reverts"`
	Total    int    `json:"total"`
	Was      int    `json:"was"`
	Reported int    `json:"reported"`
	Source   string `json:"source"`
}

// EffItemRow is one landed item in the list under the roll-ups.
type EffItemRow struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Dept     string  `json:"dept,omitempty"`
	LandedAt string  `json:"landed_at"`
	Mark     EffMark `json:"mark"`
	// SendBacks is the words for it ("2 send-backs", "first test failed", "0 send-backs").
	SendBacks    string   `json:"send_backs"`
	TotalSeconds int64    `json:"total_seconds"`
	Usd          *float64 `json:"usd"`
	// Reported is that a reported input went into the mark or the count.
	Reported bool `json:"reported"`
}

// EffAbandoned is one item given up on.
type EffAbandoned struct {
	ID     string   `json:"id"`
	Title  string   `json:"title"`
	Reason string   `json:"reason,omitempty"`
	Usd    *float64 `json:"usd"`
}

// EffReport is the whole answer of GET /_hub/work/efficacy.
type EffReport struct {
	By           string `json:"by"`
	ObservedOnly bool   `json:"observed_only"`
	Now          string `json:"now"`
	Zone         string `json:"zone"`
	// Periods is oldest to newest, the last being This.
	Periods []EffPeriod `json:"periods"`
	// Compare is the period before This, cut to the same days when This is partial.
	Compare EffPeriod           `json:"compare"`
	Trends  map[string]EffTrend `json:"trends"`
	Wrong   EffWrong            `json:"wrong"`
	// SendBacksByStep is this period's send-backs by the step they were sent back from.
	SendBacksByStep map[string]int `json:"send_backs_by_step"`
	Time            struct {
		Median       int64  `json:"median"`
		Work         int64  `json:"work"`
		Wait         int64  `json:"wait"`
		CI           int64  `json:"ci"`
		P90Step      string `json:"p90_step"`
		P90          int64  `json:"p90"`
		LongWaitStep string `json:"long_wait_step"`
		LongWait     int64  `json:"long_wait"`
		LongWaitItem string `json:"long_wait_item"`
	} `json:"time"`
	Abandoned      []EffAbandoned `json:"abandoned"`
	AbandonedTotal *float64       `json:"abandoned_total"`
	Landed         []EffItemRow   `json:"landed"`
	// Prices says what the dollars were worked out on: estimated, at list price.
	Estimated bool     `json:"estimated"`
	Notes     []string `json:"notes"`
}

// BuildEfficacy answers the roll-ups for the query.
func BuildEfficacy(in EffReportIn) EffReport {
	by := in.By
	if by != "day" {
		by = "week"
	}
	now := in.Now
	to := in.To
	if to.IsZero() {
		to = now
	}
	to = to.In(now.Location())
	rep := EffReport{By: by, ObservedOnly: in.ObservedOnly, Now: now.UTC().Format(time.RFC3339), Zone: now.Location().String(), Trends: map[string]EffTrend{},
		SendBacksByStep: map[string]int{}, Abandoned: []EffAbandoned{}, Landed: []EffItemRow{}, Estimated: true, Periods: []EffPeriod{}}
	works := map[string]hubstore.Work{}
	for _, w := range in.Works {
		works[w.BacklogID] = w
	}
	var views []*effView
	for id, o := range in.Outcomes {
		w, ok := works[id]
		if !ok {
			w = hubstore.Work{BacklogID: id, Title: id}
		}
		views = append(views, ViewOf(w, o, in.ObservedOnly, now))
	}
	sort.Slice(views, func(i, j int) bool { return views[i].w.BacklogID < views[j].w.BacklogID })

	thisStart := periodStart(to, by)
	first := periodPrev(thisStart, by)
	for i := 0; i < 6; i++ {
		first = periodPrev(first, by)
	}
	if !in.From.IsZero() {
		first = periodStart(in.From.In(now.Location()), by)
		if limit := thisStart; first.After(limit) {
			first = limit
		}
	}
	// Whole periods from first to thisStart, then This itself.
	for s := first; !s.After(thisStart); s = periodNext(s, by) {
		end := periodNext(s, by)
		cut := end
		partial := end.After(now)
		if partial {
			cut = now.Add(time.Nanosecond)
		}
		landed, figs := Figures(views, s, cut)
		rep.Periods = append(rep.Periods, EffPeriod{Key: periodKey(s, by), Start: s.UTC().Format(time.RFC3339), End: end.UTC().Format(time.RFC3339),
			Partial: partial, Landed: landed, TooFew: landed < EffMinItems, Figures: figs})
		if len(rep.Periods) >= 60 {
			break
		}
	}
	this := rep.Periods[len(rep.Periods)-1]
	thisEnd := periodNext(thisStart, by)
	cutEnd := thisEnd
	if this.Partial {
		cutEnd = now.Add(time.Nanosecond)
	}
	prevStart := periodPrev(thisStart, by)
	prevEnd := thisStart
	if this.Partial {
		prevEnd = prevStart.Add(now.Sub(thisStart) + time.Nanosecond)
	}
	pl, pf := Figures(views, prevStart, prevEnd)
	rep.Compare = EffPeriod{Key: periodKey(prevStart, by), Start: prevStart.UTC().Format(time.RFC3339), End: prevEnd.UTC().Format(time.RFC3339),
		Landed: pl, TooFew: pl < EffMinItems, Figures: pf}
	for _, k := range EffFigureKeys {
		rep.Trends[k] = TrendOf(this.Figures[k], pf[k], this.Landed, pl)
	}

	// What went wrong after landing, counted by the day it happened.
	wrong := func(a, b time.Time) (w EffWrong) {
		for _, v := range views {
			if v.landed.IsZero() {
				continue
			}
			for _, e := range in.Events[v.w.BacklogID] {
				at := rowTime(e.At)
				if at.Before(a) || !at.Before(b) || at.Before(v.landed) {
					continue
				}
				if e.Source == hubstore.EvReported && in.ObservedOnly {
					continue
				}
				switch e.Kind {
				case hubstore.EventCIRed:
					w.Red++
				case hubstore.EventBugFiled:
					w.Bugs++
				case hubstore.EventRevert:
					w.Reverts++
				default:
					continue
				}
				if e.Source == hubstore.EvReported {
					w.Reported++
				}
			}
		}
		w.Total = w.Red + w.Bugs + w.Reverts
		w.Source = hubstore.EvObserved
		if w.Reported > 0 {
			w.Source = hubstore.EvReported
		}
		return w
	}
	rep.Wrong = wrong(thisStart, cutEnd)
	rep.Wrong.Was = wrong(prevStart, prevEnd).Total

	// The panels and the list are about This.
	var inThis []*effView
	byStep := map[string][]float64{}
	for _, v := range views {
		if !v.abandoned.IsZero() && !v.abandoned.Before(thisStart) && v.abandoned.Before(cutEnd) {
			a := EffAbandoned{ID: v.w.BacklogID, Title: v.w.Title, Reason: v.d.Reason}
			if v.costKnown {
				a.Usd = fptr(usd(v.abandonedC))
			}
			rep.Abandoned = append(rep.Abandoned, a)
		}
		if v.landed.IsZero() || v.landed.Before(thisStart) || !v.landed.Before(cutEnd) {
			continue
		}
		inThis = append(inThis, v)
		for st, n := range v.sbByStep {
			rep.SendBacksByStep[st] += n
		}
		for st, sec := range v.d.StepSeconds {
			byStep[st] = append(byStep[st], float64(sec))
		}
		if v.d.LongestWaitSeconds > rep.Time.LongWait {
			rep.Time.LongWait, rep.Time.LongWaitStep, rep.Time.LongWaitItem = v.d.LongestWaitSeconds, v.d.LongestWaitStep, v.w.BacklogID
		}
	}
	var wasteSum float64
	var wasteKnown bool
	for _, a := range rep.Abandoned {
		if a.Usd != nil {
			wasteSum, wasteKnown = wasteSum+*a.Usd, true
		}
	}
	if wasteKnown {
		rep.AbandonedTotal = fptr(wasteSum)
	}
	sort.Slice(rep.Abandoned, func(i, j int) bool { return rep.Abandoned[i].ID < rep.Abandoned[j].ID })
	if tl := this.Figures[FigTimeLand]; tl.Value != nil {
		rep.Time.Median = int64(*tl.Value)
		rep.Time.Wait, rep.Time.CI, rep.Time.Work = int64(tl.Parts["wait"]), int64(tl.Parts["ci"]), int64(tl.Parts["work"])
	}
	for _, st := range hubstore.Steps {
		if p := int64(percentile(byStep[st], .9)); p > rep.Time.P90 {
			rep.Time.P90, rep.Time.P90Step = p, st
		}
	}
	for _, v := range inThis {
		o := v.o
		if in.ObservedOnly {
			o.Verdict, o.FirstPass = v.verdict, v.firstPass
		}
		mark, _ := MarkOf(o, now)
		row := EffItemRow{ID: v.w.BacklogID, Title: v.w.Title, Dept: v.w.Dept, LandedAt: o.LandedAt, Mark: mark, TotalSeconds: v.d.TotalSeconds,
			Reported: v.reported}
		switch n := v.sendBacks(); {
		case v.firstPass == "fail":
			row.SendBacks = "first test failed"
		case n == 1 && v.sbDesign == 0 && v.sbReview == 1:
			row.SendBacks = "1 review send-back"
		case n == 1:
			row.SendBacks = "1 send-back"
		default:
			row.SendBacks = strconv.Itoa(n) + " send-backs"
		}
		if v.costKnown {
			row.Usd = fptr(usd(v.stepCost))
		}
		rep.Landed = append(rep.Landed, row)
	}
	sort.SliceStable(rep.Landed, func(i, j int) bool {
		a, b := rep.Landed[i], rep.Landed[j]
		if a.Mark.Rank != b.Mark.Rank {
			return a.Mark.Rank < b.Mark.Rank
		}
		return a.ID < b.ID
	})
	rep.Notes = []string{"A bug nobody linked is not counted, so reported bug links are a floor, which is why the observed-only filter exists.",
		"Dollars are an estimate at list price from the real tokens. A step the item never reached shows a dash, not zero."}
	return rep
}
