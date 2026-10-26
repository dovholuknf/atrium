package link

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/base/forge"
	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/hubstore"
)

// THE EFFICACY COLLECTORS (item lifecycle L9, docs/rnd/item-lifecycle-design.md section 12.6). A tick reads what L2 to L5 already
// keep (the facts, the steps, the cost) plus two new reads, reverts on the mirror and CI on main, writes the events once each
// (the event key makes a replay a no-op), and derives every item's outcome again from scratch.
//
//	GET /_hub/work/efficacy?from=&to=&by=day|week&observed_only=1   the roll-ups, the trend and the lists. A READ of what the last
//	                                                                tick derived: nothing here writes
//	GET /_hub/work/<id>  gains `efficacy`: the item's outcome, its events and its mark
//	POST /_hub/work/<id>/event {kind: not_accepted|bug_filed, note, sha?, at?}  reported, from the machine the hub runs on
//
// NOTHING HERE STOPS ANYTHING. A bad week changes a number and an arrow. A read that cannot be made (no CI login, no mirror)
// leaves its events unwritten and the item `unproven`, which says so, and is not read as "nothing went wrong".

// CIPoint is the CI result of one commit on main: green when every finished run of it passed, red when one failed.
type CIPoint struct {
	SHA    string
	At     time.Time
	Result string // red or green
}

// SetCIRuns says how CI on main is read, one answer per commit. Unset, the hub's own forge door is used.
func (p *Proxy) SetCIRuns(f func(context.Context) ([]CIPoint, error)) {
	p.mu.Lock()
	p.ciRuns = f
	p.mu.Unlock()
}

// EffGit is what the lifecycle's git reach adds for efficacy: the repository's name, and the reverts on it.
type EffGit interface {
	RepoName() string
	RevertScan(ctx context.Context) ([]gitsync.Revert, error)
}

// PointsOf turns CI runs into one point per commit, oldest first. A commit with any failed run is red. Runs still going and
// runs cancelled or skipped say nothing.
func PointsOf(runs []forge.CIRunInfo) []CIPoint {
	by := map[string]*CIPoint{}
	for _, r := range runs {
		var res string
		switch strings.ToLower(r.Conclusion) {
		case "success":
			res = "green"
		case "failure", "timed_out", "startup_failure":
			res = "red"
		default:
			continue
		}
		at, err := time.Parse(time.RFC3339, r.Created)
		if err != nil || r.Head == "" {
			continue
		}
		pt := by[r.Head]
		if pt == nil {
			by[r.Head] = &CIPoint{SHA: strings.ToLower(r.Head), At: at.UTC(), Result: res}
			continue
		}
		if at.Before(pt.At) {
			pt.At = at.UTC()
		}
		if res == "red" {
			pt.Result = "red"
		}
	}
	out := make([]CIPoint, 0, len(by))
	for _, p := range by {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

// CIEvents are the ci_red and ci_green events one item's landing earned. A red commit names the item when the item's sha is in
// its failing range: the red commit holds it and the last green one before it did not. Only the first red of a run counts, since
// the rest are the same breakage, and the first green after it ends it.
func CIEvents(id, sha string, landed time.Time, points []CIPoint, contains func(build, sha string) bool) []hubstore.WorkEvent {
	var out []hubstore.WorkEvent
	lastGreen, prevRed, counted := "", false, false
	for _, pt := range points {
		switch pt.Result {
		case "green":
			if counted {
				out = append(out, hubstore.WorkEvent{WorkID: id, Kind: hubstore.EventCIGreen, At: pt.At.Format(hubstore.TimeFormat), SHA: pt.SHA,
					Source: hubstore.EvObserved})
			}
			lastGreen, prevRed, counted = pt.SHA, false, false
		case "red":
			if prevRed && counted {
				continue
			}
			ok := contains(pt.SHA, sha) && (lastGreen == "" || !contains(lastGreen, sha))
			if ok && (lastGreen != "" || !pt.At.Before(landed)) {
				out = append(out, hubstore.WorkEvent{WorkID: id, Kind: hubstore.EventCIRed, Step: hubstore.StepLanded, At: pt.At.Format(hubstore.TimeFormat),
					SHA: pt.SHA, Source: hubstore.EvObserved})
				counted = true
			}
			prevRed = true
		}
	}
	return out
}

// defaultCIRuns reads the runs on main through the hub's forge door, for the repository the merge flow names.
func (p *Proxy) defaultCIRuns(ctx context.Context) ([]CIPoint, error) {
	p.mu.Lock()
	g := p.landGit
	p.mu.Unlock()
	eg, ok := g.(EffGit)
	if !ok || eg.RepoName() == "" {
		return nil, nil
	}
	ref, err := gitsync.ParseName(eg.RepoName())
	f := p.forgeSide()
	if err != nil || f == nil {
		return nil, err
	}
	ans, err := p.forgeCI(ctx, f, forge.HubCIAsk{Host: ref.Host, Org: ref.Owner, Repo: ref.Repo, Action: forge.CIRuns, Branch: "main", Limit: forge.MaxRuns})
	if err != nil {
		return nil, err
	}
	return PointsOf(ans.Runs), nil
}

// CollectEfficacy writes the events and derives every outcome. now is the clock, a parameter so a test sets it.
func (p *Proxy) CollectEfficacy(ctx context.Context, now time.Time) error {
	st := p.itemStore()
	if st == nil {
		return nil
	}
	p.mu.Lock()
	g, ciRuns := p.landGit, p.ciRuns
	p.mu.Unlock()
	if ciRuns == nil {
		ciRuns = p.defaultCIRuns
	}
	works, err := st.WorkList(0)
	if err != nil {
		return err
	}
	backlog, err := st.BacklogForEfficacy()
	if err != nil {
		return err
	}
	known := map[string]bool{}
	byBacklog := map[string]*hubstore.BacklogItem{}
	for _, w := range works {
		known[w.BacklogID] = true
	}
	for i := range backlog {
		byBacklog[backlog[i].ID] = &backlog[i]
	}
	// One read each, shared by every item.
	points, ciErr := ciRuns(ctx)
	ciKnown := ciErr == nil && points != nil
	var reverts []gitsync.Revert
	if eg, ok := g.(EffGit); ok {
		reverts, _ = eg.RevertScan(ctx)
	}
	for _, e := range BugsNaming(backlog, known) {
		if _, err := st.AddWorkEvent(e); err != nil {
			return err
		}
	}
	tb, _ := st.Prices()
	usage, _ := st.AllWorkUsage()
	for _, w := range works {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		id := w.BacklogID
		r, err := p.refreshWork(ctx, st, id, false)
		if err != nil {
			continue
		}
		steps := r.Lifecycle.Steps
		for _, e := range FactEvents(id, r.Facts) {
			if _, err := st.AddWorkEvent(e); err != nil {
				return err
			}
		}
		if landedAt := LandedAt(steps); landedAt != "" {
			shas := map[string]bool{}
			for _, name := range []string{hubstore.StepLanded, hubstore.StepPushed, hubstore.StepBuilding} {
				if s, ok := stepRow(steps, name); ok && s.SHA != "" {
					shas[strings.ToLower(s.SHA)] = true
				}
			}
			for _, rv := range reverts {
				if shas[rv.Reverts] {
					if _, err := st.AddWorkEvent(hubstore.WorkEvent{WorkID: id, Kind: hubstore.EventRevert, At: rv.At.Format(hubstore.TimeFormat),
						SHA: rv.SHA, Note: "reverts " + rv.Reverts, Source: hubstore.EvObserved}); err != nil {
						return err
					}
				}
			}
			// CI is named by the commit that carries the item on origin's main, which has its own sha once re-signed.
			carrier := ""
			if s, ok := stepRow(steps, hubstore.StepPushed); ok && s.State == hubstore.StepDone {
				carrier = s.SHA
			}
			if carrier != "" && ciKnown && g != nil {
				contains := func(build, sha string) bool {
					c, _, _ := g.Contains(ctx, build, sha)
					return c
				}
				for _, e := range CIEvents(id, carrier, rowTime(landedAt), points, contains) {
					if _, err := st.AddWorkEvent(e); err != nil {
						return err
					}
				}
			}
		}
	}
	evs, err := st.WorkEvents("")
	if err != nil {
		return err
	}
	for _, w := range works {
		id := w.BacklogID
		r, err := p.refreshWork(ctx, st, id, false)
		if err != nil {
			continue
		}
		cost := ItemCost(tb, r.Cards, r.Lifecycle.Steps, usage[id], now)
		in := EffIn{Work: w, Steps: r.Lifecycle.Steps, Assignments: r.Assignments, Events: evs[id], Cost: cost, Backlog: byBacklog[id],
			CIKnown: ciKnown || LandedAt(r.Lifecycle.Steps) == "", Now: now}
		o := DeriveOutcome(in)
		if ev, ok := AbandonedEvent(o, ParseEffDetail(o).Reason); ok {
			if _, err := st.AddWorkEvent(ev); err != nil {
				return err
			}
		}
		if err := st.PutWorkOutcome(o); err != nil {
			return err
		}
	}
	return nil
}

// effTick is how often the collect runs.
const effTick = 2 * time.Minute

// RunEfficacy collects every effTick until ctx ends, the first a moment after the hub is up.
func (p *Proxy) RunEfficacy(ctx context.Context) {
	t := time.NewTimer(20 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = p.CollectEfficacy(ctx, time.Now())
			t.Reset(effTick)
		}
	}
}

// serveWorkEfficacy answers GET /_hub/work/efficacy from the store.
func (p *Proxy) serveWorkEfficacy(w http.ResponseWriter, r *http.Request, st *hubstore.Store) {
	q := r.URL.Query()
	loc := time.Local
	now := time.Now().In(loc)
	parse := func(s string) time.Time {
		if t, err := time.ParseInLocation("2006-01-02", s, loc); err == nil {
			return t
		}
		return time.Time{}
	}
	in := EffReportIn{By: q.Get("by"), From: parse(q.Get("from")), To: parse(q.Get("to")), Now: now,
		ObservedOnly: q.Get("observed_only") == "1" || q.Get("observed_only") == "true"}
	if t := in.To; !t.IsZero() && t.After(now) {
		in.To = now
	}
	works, err := st.WorkList(0)
	if err != nil {
		wfail(w, err)
		return
	}
	outs, err := st.WorkOutcomes()
	if err != nil {
		wfail(w, err)
		return
	}
	evs, err := st.WorkEvents("")
	if err != nil {
		wfail(w, err)
		return
	}
	in.Works, in.Outcomes, in.Events = works, outs, evs
	crJSON(w, http.StatusOK, BuildEfficacy(in))
}

// EffItem is the `efficacy` object of an item's detail.
type EffItem struct {
	Outcome hubstore.WorkOutcome `json:"outcome"`
	Detail  EffDetail            `json:"detail"`
	Events  []hubstore.WorkEvent `json:"events"`
	Mark    *EffMark             `json:"mark,omitempty"`
	// Sources says which of its figures lean on a reported input.
	Reported bool `json:"reported"`
}

// efficacyOf is an item's efficacy, read from the store, nil for an item the collect has not derived yet.
func (p *Proxy) efficacyOf(st *hubstore.Store, id string) *EffItem {
	outs, err := st.WorkOutcomes()
	if err != nil {
		return nil
	}
	o, ok := outs[id]
	if !ok {
		return nil
	}
	evs, _ := st.WorkEvents(id)
	e := &EffItem{Outcome: o, Detail: ParseEffDetail(o), Events: evs[id]}
	if e.Events == nil {
		e.Events = []hubstore.WorkEvent{}
	}
	if m, ok := MarkOf(o, time.Now()); ok {
		e.Mark = &m
	}
	e.Reported = ViewOf(hubstore.Work{BacklogID: id}, o, false, time.Now()).reported
	return e
}

// serveWorkEvent answers POST /_hub/work/<id>/event: a reported not_accepted or bug_filed. The operator's machine only, and
// always reported, since atrium saw nothing.
func (p *Proxy) serveWorkEvent(w http.ResponseWriter, r *http.Request, st *hubstore.Store, id string) {
	var in struct {
		Kind string `json:"kind"`
		Note string `json:"note"`
		SHA  string `json:"sha"`
		At   string `json:"at"`
		Step string `json:"step"`
	}
	if err := crDecode(r, &in); err != nil {
		crFail(w, http.StatusBadRequest, "could not read that: "+err.Error())
		return
	}
	if in.Kind != hubstore.EventNotAccepted && in.Kind != hubstore.EventBugFiled {
		crFail(w, http.StatusBadRequest, "kind is not_accepted or bug_filed: the rest atrium sees for itself")
		return
	}
	if _, err := st.WorkByID(id); err != nil {
		wfail(w, err)
		return
	}
	at := strings.TrimSpace(in.At)
	if at == "" {
		at = time.Now().UTC().Format(hubstore.TimeFormat)
	}
	if rowTime(at).IsZero() {
		crFail(w, http.StatusBadRequest, "at is an RFC 3339 time")
		return
	}
	step := in.Step
	if in.Kind == hubstore.EventNotAccepted {
		step = hubstore.StepBuilding
	}
	wrote, err := st.AddWorkEvent(hubstore.WorkEvent{WorkID: id, Kind: in.Kind, Step: step, At: at, SHA: in.SHA, Note: in.Note, Source: hubstore.EvReported})
	if err != nil {
		crFail(w, http.StatusBadRequest, err.Error())
		return
	}
	crJSON(w, http.StatusOK, map[string]any{"ok": true, "recorded": wrote})
}
