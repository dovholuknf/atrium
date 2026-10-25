package link

import (
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/base/forge"
	"github.com/dovholuknf/atrium/internal/hubstore"
)

var effNow = time.Date(2026, 10, 14, 15, 0, 0, 0, time.UTC) // a Wednesday

// landedItem is an item that landed at `landed`, with a test that passed, in the steps the engine would have written.
func landedItem(id, landed string, evs ...hubstore.WorkEvent) (hubstore.Work, hubstore.WorkOutcome, []hubstore.WorkEvent) {
	w := hubstore.Work{BacklogID: id, Title: "t " + id, CreatedAt: "2026-10-01T00:00:00Z"}
	steps := []hubstore.WorkStepRow{
		{Step: hubstore.StepBuilding, State: hubstore.StepDone, StartedAt: "2026-10-01T00:00:00Z", EndedAt: "2026-10-01T04:00:00Z"},
		{Step: hubstore.StepLanded, State: hubstore.StepDone, StartedAt: landed, EndedAt: landed},
	}
	evs = append([]hubstore.WorkEvent{{WorkID: id, Kind: hubstore.EventTestResult, At: "2026-10-01T03:00:00Z", Note: "pass", Source: hubstore.EvObserved}}, evs...)
	o := DeriveOutcome(EffIn{Work: w, Steps: steps, Events: evs, CIKnown: true, Now: effNow})
	return w, o, evs
}

func report(t *testing.T, observedOnly bool, items ...func() (hubstore.Work, hubstore.WorkOutcome, []hubstore.WorkEvent)) EffReport {
	t.Helper()
	in := EffReportIn{By: "week", Now: effNow, ObservedOnly: observedOnly, Outcomes: map[string]hubstore.WorkOutcome{}, Events: map[string][]hubstore.WorkEvent{}}
	for _, f := range items {
		w, o, e := f()
		in.Works = append(in.Works, w)
		in.Outcomes[w.BacklogID] = o
		in.Events[w.BacklogID] = e
	}
	return BuildEfficacy(in)
}

func item(id, landed string, evs ...hubstore.WorkEvent) func() (hubstore.Work, hubstore.WorkOutcome, []hubstore.WorkEvent) {
	return func() (hubstore.Work, hubstore.WorkOutcome, []hubstore.WorkEvent) {
		return landedItem(id, landed, evs...)
	}
}

func TestAnItemWithNoOutcomeYetIsADashNotZeroPercent(t *testing.T) {
	r := report(t, false)
	for _, k := range EffFigureKeys[:6] {
		if f := r.Periods[len(r.Periods)-1].Figures[k]; f.Value != nil {
			t.Fatalf("%s is %v with nothing landed: a dash is not a zero", k, *f.Value)
		}
	}
	// An item that landed with no test fact has no first-pass figure, and is not a zero either.
	w := hubstore.Work{BacklogID: "x", CreatedAt: "2026-10-01T00:00:00Z"}
	o := DeriveOutcome(EffIn{Work: w, Steps: []hubstore.WorkStepRow{{Step: hubstore.StepLanded, State: hubstore.StepDone, EndedAt: "2026-10-12T10:00:00Z"}}, CIKnown: true, Now: effNow})
	if o.FirstPass != "" || o.Verdict != VerdictWatching {
		t.Fatalf("%+v", o)
	}
	_, figs := Figures([]*effView{ViewOf(w, o, false, effNow)}, time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC), effNow)
	if figs[FigFirstPass].Value != nil || figs[FigCleanPass].Value != nil {
		t.Fatalf("an untested, still-watched item is a dash: %+v %+v", figs[FigFirstPass], figs[FigCleanPass])
	}
}

func TestARevertShowsOnTheItemAndInTheWeekItHappened(t *testing.T) {
	rev := hubstore.WorkEvent{WorkID: "f-a", Kind: hubstore.EventRevert, At: "2026-10-13T09:00:00Z", SHA: "dead", Source: hubstore.EvObserved}
	// f-a landed LAST week and was reverted this week.
	r := report(t, false, item("f-a", "2026-10-06T10:00:00Z", rev))
	if r.Wrong.Reverts != 1 || r.Wrong.Total != 1 {
		t.Fatalf("this week's tile: %+v", r.Wrong)
	}
	if r.Wrong.Was != 0 {
		t.Fatalf("last week did not see it: %+v", r.Wrong)
	}
	o := landedOutcome(t, "f-a", "2026-10-06T10:00:00Z", rev)
	if o.Reverted != 1 || o.Verdict != VerdictNotClean {
		t.Fatalf("the item: %+v", o)
	}
	if m, _ := MarkOf(o, effNow); m.Kind != "revert" {
		t.Fatalf("the mark: %+v", m)
	}
}

func landedOutcome(t *testing.T, id, landed string, evs ...hubstore.WorkEvent) hubstore.WorkOutcome {
	t.Helper()
	_, o, _ := landedItem(id, landed, evs...)
	return o
}

func TestASendBackIsCountedByTheTransitionNotTheNote(t *testing.T) {
	a := hubstore.WorkEvent{WorkID: "f-a", Kind: hubstore.EventSendBack, Step: "designed", At: "2026-10-02T01:00:00Z", SHA: "aa", Note: "cover the halt", Source: hubstore.EvObserved}
	b := a
	b.At = "2026-10-02T05:00:00Z" // the same note again, later: a second send-back
	o := landedOutcome(t, "f-a", "2026-10-12T10:00:00Z", a, b)
	if d := ParseEffDetail(o); d.SendBacks["design"] != 2 || d.ByStep["designed"] != 2 {
		t.Fatalf("%+v", d)
	}
}

func TestFactEventsTakeOnlyTheFirstTestAndTheirSource(t *testing.T) {
	facts := []hubstore.WorkFact{
		{Seq: 1, Kind: hubstore.FactTest, Source: hubstore.SourceReported, At: "2026-10-01T01:00:00Z", AtSHA: "a1", Payload: `{"result":"fail"}`},
		{Seq: 2, Kind: hubstore.FactTest, Source: hubstore.SourceSeen, At: "2026-10-01T02:00:00Z", AtSHA: "a2", Payload: `{"result":"pass"}`},
		{Seq: 3, Kind: hubstore.FactReview, Source: hubstore.SourceSeen, At: "2026-10-01T03:00:00Z", AtSHA: "a2", Payload: `{"verdict":"hold","findings":2}`},
		{Seq: 4, Kind: hubstore.FactSendBack, Source: hubstore.SourceSeen, At: "2026-10-01T04:00:00Z", AtSHA: "d1", Payload: `{"note":"n"}`},
	}
	ev := FactEvents("f-a", facts)
	if len(ev) != 3 || ev[0].Kind != hubstore.EventTestResult || ev[0].Note != "fail" || ev[0].Source != hubstore.EvReported {
		t.Fatalf("%+v", ev)
	}
	if ev[1].Kind != hubstore.EventReviewVerdct || ev[1].Note != "hold 2 findings" {
		t.Fatalf("%+v", ev[1])
	}
	if ev[2].Kind != hubstore.EventSendBack || ev[2].Step != hubstore.StepDesigned {
		t.Fatalf("%+v", ev[2])
	}
}

func TestAWeekWithFewerThanFiveLandedItemsSaysTooFewToTrend(t *testing.T) {
	var items []func() (hubstore.Work, hubstore.WorkOutcome, []hubstore.WorkEvent)
	for i, id := range []string{"a", "b", "c", "d"} {
		items = append(items, item(id, "2026-10-12T1"+string(rune('0'+i))+":00:00Z"))
	}
	r := report(t, false, items...)
	this := r.Periods[len(r.Periods)-1]
	if !this.TooFew || this.Landed != 4 {
		t.Fatalf("%+v", this)
	}
	if tr := r.Trends[FigFirstPass]; tr.Verdict == "better" || tr.Verdict == "worse" {
		t.Fatalf("a trend on 4 items: %+v", tr)
	}
	// Five is enough to be trended, once last week has five too.
	for _, id := range []string{"e", "f", "g", "h", "i"} {
		items = append(items, item(id, "2026-10-06T10:00:00Z"))
	}
	items = append(items, item("j", "2026-10-13T10:00:00Z"))
	r = report(t, false, items...)
	if r.Periods[len(r.Periods)-1].TooFew {
		t.Fatalf("five landed is not too few: %+v", r.Periods[len(r.Periods)-1])
	}
}

func TestAReportedInputIsTaggedAndObservedOnlyDropsIt(t *testing.T) {
	bug := hubstore.WorkEvent{WorkID: "f-a", Kind: hubstore.EventBugFiled, At: "2026-10-13T09:00:00Z", Note: "b-1", Source: hubstore.EvReported}
	r := report(t, false, item("f-a", "2026-10-12T10:00:00Z", bug))
	f := r.Periods[len(r.Periods)-1].Figures[FigBugs]
	if f.Source != hubstore.EvReported || f.ReportedN != 1 || f.Value == nil || *f.Value != 1 {
		t.Fatalf("%+v", f)
	}
	r = report(t, true, item("f-a", "2026-10-12T10:00:00Z", bug))
	f = r.Periods[len(r.Periods)-1].Figures[FigBugs]
	if f.Value == nil || *f.Value != 0 || f.Source != hubstore.EvObserved || r.Wrong.Bugs != 0 {
		t.Fatalf("observed only still counts a reported bug: %+v %+v", f, r.Wrong)
	}
}

func TestTheCleanPassWaitsOutSevenDays(t *testing.T) {
	o := landedOutcome(t, "f-a", "2026-10-12T10:00:00Z")
	m, _ := MarkOf(o, effNow)
	if o.Verdict != VerdictWatching || m.Text != "watching, 5 days left" {
		t.Fatalf("%s %+v", o.Verdict, m)
	}
	o = landedOutcome(t, "f-b", "2026-10-01T10:00:00Z")
	if o.Verdict != VerdictClean || o.CleanAt == "" {
		t.Fatalf("%+v", o)
	}
	if v, _ := EffVerdict(effNow, effNow.Add(-time.Hour), 0, "pass", false, 0, 0, false); v != VerdictUnproven {
		t.Fatalf("clean without a readable CI is %s", v)
	}
}

func TestTrendIsGoodOrBadAndIgnoresTheNoiseFloor(t *testing.T) {
	mk := func(k, u string, v float64, den int) EffFigure {
		return EffFigure{Key: k, Unit: u, Value: &v, Den: den}
	}
	if tr := TrendOf(mk(FigSendBacks, UnitPerItem, 0.5, 10), mk(FigSendBacks, UnitPerItem, 0.9, 10), 10, 10); tr.Verdict != "better" || tr.Dir != "down" {
		t.Fatalf("fewer send-backs is better: %+v", tr)
	}
	if tr := TrendOf(mk(FigCleanPass, UnitPct, 5.0/7, 7), mk(FigCleanPass, UnitPct, 4.0/7, 7), 7, 7); tr.Verdict != "better" {
		t.Fatalf("%+v", tr)
	}
	if tr := TrendOf(mk(FigCost, UnitUSD, 3.1, 8), mk(FigCost, UnitUSD, 3.0, 8), 8, 8); tr.Verdict != "same" {
		t.Fatalf("a 3 percent move in a median is noise: %+v", tr)
	}
	if tr := TrendOf(mk(FigCleanPass, UnitPct, 1, 3), mk(FigCleanPass, UnitPct, 0.5, 3), 3, 3); tr.Verdict != "too_few" {
		t.Fatalf("%+v", tr)
	}
}

func TestCIEventsNameTheItemOnlyWhenItsShaIsInTheFailingRange(t *testing.T) {
	pts := []CIPoint{
		{SHA: "g1", At: effNow.Add(-5 * time.Hour), Result: "green"},
		{SHA: "r1", At: effNow.Add(-4 * time.Hour), Result: "red"},
		{SHA: "r2", At: effNow.Add(-3 * time.Hour), Result: "red"},
		{SHA: "g2", At: effNow.Add(-2 * time.Hour), Result: "green"},
	}
	has := map[string]bool{"r1": true, "r2": true, "g2": true} // the item landed between g1 and r1
	contains := func(build, sha string) bool { return has[build] }
	ev := CIEvents("f-a", "item", effNow.Add(-6*time.Hour), pts, contains)
	if len(ev) != 2 || ev[0].Kind != hubstore.EventCIRed || ev[0].SHA != "r1" || ev[1].Kind != hubstore.EventCIGreen || ev[1].SHA != "g2" {
		t.Fatalf("one red episode, ended by the next green: %+v", ev)
	}
	// An item that was already in the last green is not in the range.
	has["g1"] = true
	if ev := CIEvents("f-a", "item", effNow.Add(-6*time.Hour), pts, contains); len(ev) != 0 {
		t.Fatalf("%+v", ev)
	}
	o := DeriveOutcome(EffIn{Work: hubstore.Work{BacklogID: "f-a"}, Steps: []hubstore.WorkStepRow{{Step: hubstore.StepLanded, State: hubstore.StepDone, EndedAt: effNow.Add(-6 * time.Hour).Format(time.RFC3339)}},
		Events: []hubstore.WorkEvent{{WorkID: "f-a", Kind: hubstore.EventCIRed, At: effNow.Add(-4 * time.Hour).Format(time.RFC3339), Source: hubstore.EvObserved},
			{WorkID: "f-a", Kind: hubstore.EventCIGreen, At: effNow.Add(-2 * time.Hour).Format(time.RFC3339), Source: hubstore.EvObserved}}, CIKnown: true, Now: effNow})
	if o.CIRedSeconds != 7200 {
		t.Fatalf("red for %d s", o.CIRedSeconds)
	}
}

func TestPointsOfGroupsRunsByCommitAndIgnoresUnfinishedOnes(t *testing.T) {
	runs := []forge.CIRunInfo{
		{Head: "a", Conclusion: "success", Created: "2026-10-14T01:00:00Z"},
		{Head: "a", Conclusion: "failure", Created: "2026-10-14T01:05:00Z"},
		{Head: "b", Conclusion: "", Created: "2026-10-14T02:00:00Z"},
		{Head: "c", Conclusion: "cancelled", Created: "2026-10-14T03:00:00Z"},
		{Head: "d", Conclusion: "success", Created: "2026-10-14T04:00:00Z"},
	}
	pts := PointsOf(runs)
	if len(pts) != 2 || pts[0].Result != "red" || pts[1].SHA != "d" {
		t.Fatalf("%+v", pts)
	}
}

func TestBugsNamingAnItemAreObservedByTagAndReportedByFixes(t *testing.T) {
	bl := []hubstore.BacklogItem{
		{ID: "b-1", Title: "crash", Body: "atrium:item:f-a", CreatedAt: "2026-10-13T00:00:00Z"},
		{ID: "b-2", Title: "other", Body: "fixes f-b", CreatedAt: "2026-10-13T00:00:00Z"},
		{ID: "f-3", Title: "not a bug", Body: "fixes f-a", CreatedAt: "2026-10-13T00:00:00Z"},
	}
	ev := BugsNaming(bl, map[string]bool{"f-a": true, "f-b": true})
	if len(ev) != 2 || ev[0].Source != hubstore.EvObserved || ev[1].Source != hubstore.EvReported {
		t.Fatalf("%+v", ev)
	}
}

func TestNotAcceptedIsReadFromAnOrchestratorMessageNamingTheItem(t *testing.T) {
	if why, ok := NotAcceptedNote("Not accepted: f-a misses the halt", "f-a"); !ok || why != "f-a misses the halt" {
		t.Fatalf("%q %v", why, ok)
	}
	if _, ok := NotAcceptedNote("not accepted: other", "f-a"); ok {
		t.Fatal("it names another item")
	}
}

func TestAbandonedCostIsTheStepsAndCoordination(t *testing.T) {
	w := hubstore.Work{BacklogID: "f-x", CreatedAt: "2026-10-01T00:00:00Z", UpdatedAt: "2026-10-01T00:00:00Z"}
	o := DeriveOutcome(EffIn{Work: w, Backlog: &hubstore.BacklogItem{ID: "f-x", Status: "dropped", ChangedAt: "2026-10-13T00:00:00Z"},
		Cost: CostView{Rows: 1, Headline: CostLine{Units: 5e9, Known: true}, Coordination: CostLine{Units: 1e9, Known: true}}, Now: effNow})
	if o.AbandonedAt == "" || o.AbandonedCost != 6e9 || !o.CostKnown {
		t.Fatalf("%+v", o)
	}
	r := BuildEfficacy(EffReportIn{By: "week", Now: effNow, Works: []hubstore.Work{w}, Outcomes: map[string]hubstore.WorkOutcome{"f-x": o}})
	if r.AbandonedTotal == nil || *r.AbandonedTotal != 0.6 || len(r.Abandoned) != 1 {
		t.Fatalf("%+v", r.Abandoned)
	}
}
