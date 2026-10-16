package link

import (
	"regexp"
	"sort"
	"strings"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

// THE STEP ENGINE, THE PURE PART (item lifecycle L2). Derive reads everything an item has and answers its eight steps and
// the item's own state. It reads no clock, no git and no store, so a bug in it is fixed by running it again, and a test
// feeds it a table. What it needs from git is already a DocView when it is called. See lifecycle_engine.go for the part
// that gathers it, and docs/rnd/item-lifecycle-design.md sections 2.4, 3 and 4.1.
//
// WHAT THIS STAGE SEES: designed, approved and building. The other five stay pending until their stages land (L3 to L5),
// and the item is `finished` once everything this stage can see is done.

// DocView is what git says of the design doc, read by the engine before it calls Derive.
type DocView struct {
	// Known is whether git could be read at all. False leaves a done step done: an unreadable hub is no evidence the doc moved.
	Known bool
	// Latest is the newest commit that changed the design doc, at or after the designer's. It is what an approval pins.
	Latest string
	// Since is how many commits changed the doc after the approved one, 0 with no approval.
	Since int
}

// DeriveIn is everything Derive reads.
type DeriveIn struct {
	Work        hubstore.Work
	Cards       []hubstore.WorkCard
	Assignments []hubstore.Item
	Facts       []hubstore.WorkFact
	Doc         DocView
	// Stored are the rows L3 wrote for landed, deployed and pushed, which are read from git and not derived here. They are
	// taken as they stand, so the item's current step goes on past building.
	Stored []hubstore.WorkStepRow
	// Evidence is what git says of the branch for tested and reviewed. The zero value is "git could not be read".
	Evidence Evidence
}

// Evidence is what the hub read of the item's branch for the tested and reviewed steps (item lifecycle L4).
type Evidence struct {
	// Known is that the branch was read, so Tip is the tip. False leaves a done step done: an unreadable hub is no evidence
	// the branch moved.
	Known bool
	Tip   string
	// Since is, per commit a test was recorded at, how many commits of work the branch has gained after it. -1 is "no longer
	// on the branch".
	Since map[string]int
	// Cover is what the verdict trailers say of the item's own commits, and CoverKnown that it could be read.
	Cover      CoverView
	CoverKnown bool
}

// CoverView is deployready's coverage, in the words the engine needs.
type CoverView struct {
	Total, Covered, Held int
	TipCovered           bool
}

// Lifecycle is what Derive answers.
type Lifecycle struct {
	Steps []hubstore.WorkStepRow `json:"steps"`
	// State is the item's own: blocked, running, queued or finished. Derived, never stored on its own.
	State string `json:"state"`
	// Current is the first step that is not done or skipped, empty when there is none.
	Current string `json:"current"`
	// Waiting is who the item waits on right now: `human` for an approval (or a question), else empty.
	Waiting string `json:"waiting,omitempty"`
	// Ready is tested and reviewed at the tip with the test SEEN by atrium. A reported test never makes it true, and NotReady
	// says what is missing.
	Ready    bool   `json:"ready"`
	NotReady string `json:"not_ready,omitempty"`
}

var shaInNote = regexp.MustCompile(`^[0-9a-f]{7,64}$`)

// NoteSHA is the commit an assignment's finishing note names, or "". The room puts the sha of an atrium_done in the note.
func NoteSHA(note string) string {
	n := strings.ToLower(strings.TrimSpace(note))
	if shaInNote.MatchString(n) {
		return n
	}
	return ""
}

func cardKey(room, card string) string {
	return strings.ToLower(strings.TrimSpace(room)) + "/" + strings.TrimSpace(card)
}

// StepOfCard is the step a card is for. A card attached with no step is a builder: that is what every launch was before
// the step was named.
func StepOfCard(c hubstore.WorkCard) string {
	if hubstore.ValidStep(c.Step) {
		return c.Step
	}
	return hubstore.StepBuilding
}

// Adhoc is whether an item is one no backlog item named, which has no design to approve.
func Adhoc(w hubstore.Work) bool { return strings.HasPrefix(w.BacklogID, hubstore.AdhocPrefix) }

// stepWork is the cards and assignments that belong to one step.
type stepWork struct {
	cards   []string
	assigns []hubstore.Item
}

func (in DeriveIn) of(step string) stepWork {
	var w stepWork
	on := map[string]bool{}
	for _, c := range in.Cards {
		if StepOfCard(c) == step {
			k := cardKey(c.Room, c.Card)
			on[k] = true
			w.cards = append(w.cards, k)
		}
	}
	for _, a := range in.Assignments {
		if on[cardKey(a.Room, a.Card)] {
			w.assigns = append(w.assigns, a)
		}
	}
	sort.SliceStable(w.assigns, func(i, j int) bool { return w.assigns[i].CreatedAt < w.assigns[j].CreatedAt })
	return w
}

func (w stepWork) any(states ...string) bool {
	for _, a := range w.assigns {
		for _, s := range states {
			if a.State == s {
				return true
			}
		}
	}
	return false
}

func (w stepWork) all(state string) bool {
	for _, a := range w.assigns {
		if a.State != state {
			return false
		}
	}
	return len(w.assigns) > 0
}

func (w stepWork) started() string {
	first := ""
	for _, a := range w.assigns {
		if first == "" || a.CreatedAt < first {
			first = a.CreatedAt
		}
	}
	return first
}

// lastFinished is the end of the latest finished assignment, and the sha its note names.
func (w stepWork) lastFinished() (at, sha string) {
	for _, a := range w.assigns {
		if a.State == hubstore.ItemStateFinished && a.UpdatedAt >= at {
			at, sha = a.UpdatedAt, NoteSHA(a.Note)
		}
	}
	return at, sha
}

func latest(facts []hubstore.WorkFact, kind string) (hubstore.WorkFact, bool) {
	for i := len(facts) - 1; i >= 0; i-- {
		if facts[i].Kind == kind {
			return facts[i], true
		}
	}
	return hubstore.WorkFact{}, false
}

func has(facts []hubstore.WorkFact, kind string) bool {
	_, ok := latest(facts, kind)
	return ok
}

// Derive is the engine. The facts are in the order they were written.
func Derive(in DeriveIn) Lifecycle {
	skipWhy := ""
	switch {
	case has(in.Facts, hubstore.FactBuildDirectly):
		skipWhy = "filed to build directly"
	case Adhoc(in.Work):
		skipWhy = "no backlog item named this work, so there is no design to approve"
	case noDesignNeeded(in):
		skipWhy = "built with no design: a fix small enough not to need one"
	}

	byStep := map[string]hubstore.WorkStepRow{}
	put := func(s hubstore.WorkStepRow) {
		if s.Cards == "" {
			s.Cards = "[]"
		}
		byStep[s.Step] = s
	}

	// ── designed ──
	des := in.of(hubstore.StepDesigned)
	designed := hubstore.WorkStepRow{Step: hubstore.StepDesigned, State: hubstore.StepPending, Cards: hubstore.CardsJSON(des.cards), StartedAt: des.started()}
	design, haveDesign := latest(in.Facts, hubstore.FactDesign)
	sentBack := map[string]bool{}
	for _, f := range in.Facts {
		if f.Kind == hubstore.FactSendBack {
			sentBack[f.AtSHA] = true
		}
	}
	designSent := haveDesign && sentBack[design.AtSHA]
	switch {
	case skipWhy != "":
		designed.State, designed.Reason = hubstore.StepSkipped, skipWhy
	case haveDesign && !designSent:
		designed.State, designed.SHA, designed.EndedAt = hubstore.StepDone, design.AtSHA, design.At
		if designed.StartedAt == "" {
			designed.StartedAt = design.At
		}
	case designSent && !des.any(hubstore.ItemStateRunning, hubstore.ItemStateQueued, hubstore.ItemStateBlocked):
		designed.State, designed.Party, designed.SHA = hubstore.StepActive, hubstore.PartyCard, design.AtSHA
		designed.Reason = "sent back at " + short(design.AtSHA) + ", waiting for the designer's next commit"
	case des.any(hubstore.ItemStateBlocked):
		designed.State, designed.Party, designed.Reason = hubstore.StepBlocked, hubstore.PartyCard, "the designer is blocked"
	case des.any(hubstore.ItemStateRunning, hubstore.ItemStateQueued):
		designed.State, designed.Party = hubstore.StepActive, hubstore.PartyCard
	case des.all(hubstore.ItemStateFinished):
		// Finished, and no design fact: the commit it named does not touch a design doc the hub can read (yet).
		designed.State, designed.Party = hubstore.StepActive, hubstore.PartyCard
		if at, sha := des.lastFinished(); sha == "" {
			designed.EndedAt, designed.Reason = at, "the designer finished without naming a commit"
		} else {
			designed.EndedAt, designed.SHA = at, sha
			designed.Reason = "the designer's commit " + short(sha) + " does not touch a design doc the hub can read"
		}
	}
	put(designed)

	// ── approved ──
	approved := hubstore.WorkStepRow{Step: hubstore.StepApproved, State: hubstore.StepPending, Cards: "[]"}
	switch {
	case skipWhy != "":
		approved.State, approved.Reason = hubstore.StepSkipped, skipWhy
	case designed.State != hubstore.StepDone:
		approved.Reason = "waits for the design"
	default:
		approved.StartedAt = designed.EndedAt
		var ap hubstore.WorkFact
		found := false
		for _, f := range in.Facts {
			if f.Kind == hubstore.FactApprove && f.Note("design") == design.AtSHA {
				ap, found = f, true
			}
		}
		switch {
		case !found:
			approved.State, approved.Party, approved.SHA = hubstore.StepActive, hubstore.PartyHuman, firstNonEmpty(in.Doc.Latest, design.AtSHA)
			approved.Reason = "waiting on you: approve the design at " + short(approved.SHA) + ", or send it back with a note"
		case in.Doc.Known && in.Doc.Since > 0:
			approved.State, approved.SHA, approved.EndedAt = hubstore.StepStale, ap.AtSHA, ap.At
			approved.Reason = "approved at " + short(ap.AtSHA) + ", and the design doc has changed since, " + plural(in.Doc.Since, "commit") +
				" on, at " + short(firstNonEmpty(in.Doc.Latest, ap.AtSHA))
		default:
			approved.State, approved.SHA, approved.EndedAt = hubstore.StepDone, ap.AtSHA, ap.At
		}
	}
	put(approved)

	// ── building ──
	bld := in.of(hubstore.StepBuilding)
	building := hubstore.WorkStepRow{Step: hubstore.StepBuilding, State: hubstore.StepPending, Cards: hubstore.CardsJSON(bld.cards), StartedAt: bld.started()}
	switch {
	case bld.any(hubstore.ItemStateBlocked):
		building.State, building.Party, building.Reason = hubstore.StepBlocked, hubstore.PartyCard, "a builder is blocked"
	case bld.any(hubstore.ItemStateRunning, hubstore.ItemStateQueued):
		building.State, building.Party = hubstore.StepActive, hubstore.PartyCard
	case bld.all(hubstore.ItemStateFinished):
		building.State = hubstore.StepDone
		building.EndedAt, building.SHA = bld.lastFinished()
	}
	if len(bld.assigns) > 0 && building.State != hubstore.StepDone && skipWhy == "" && approved.State != hubstore.StepDone {
		building.Reason = "started before the design was approved"
	}
	put(building)

	// ── tested and reviewed (L4) ──
	landed := false
	for _, s := range in.Stored {
		if s.Step == hubstore.StepLanded && s.State == hubstore.StepDone {
			landed = true
		}
	}
	stored := map[string]hubstore.WorkStepRow{}
	for _, s := range in.Stored {
		stored[s.Step] = s
	}
	tested, dirtyTest := deriveTested(in)
	reviewed := deriveReviewed(in)
	// A LANDED ITEM STAYS WHAT IT WAS. Once the branch is on claude/main its own range is empty and a test recorded at its old
	// tip is "not on the branch", which would unsay a test and a review that were true when it landed.
	if landed {
		for _, s := range []*hubstore.WorkStepRow{&tested, &reviewed} {
			if old, ok := stored[s.Step]; ok && (old.State == hubstore.StepDone || old.State == hubstore.StepStale) && s.State != hubstore.StepDone {
				*s = old
			}
		}
	}
	put(tested)
	put(reviewed)

	// The steps L3 reads from git are taken as stored, and the ones no stage has built yet stay pending, so the bar has
	// eight places and the item's current step is honest.
	for _, s := range in.Stored {
		if !engineStep(s.Step) && hubstore.ValidStep(s.Step) {
			put(s)
		}
	}
	out := completeSteps(steps(byStep))
	out.State, out.Waiting = itemState(out, in.Assignments)
	out.Ready, out.NotReady = readiness(tested, reviewed, dirtyTest)
	return out
}

// noDesignNeeded is an item that was built (its builders all finished, or it landed) with no designer ever on it and no
// design noticed. A bug or small fix is filed and built straight away, and waiting at `designed` forever after it has landed
// is wrong. An item a designer has been on, or whose design was sent back, is never skipped by this.
func noDesignNeeded(in DeriveIn) bool {
	if has(in.Facts, hubstore.FactDesign) || has(in.Facts, hubstore.FactSendBack) || len(in.of(hubstore.StepDesigned).cards) > 0 {
		return false
	}
	if in.of(hubstore.StepBuilding).all(hubstore.ItemStateFinished) {
		return true
	}
	for _, s := range in.Stored {
		if s.Step == hubstore.StepLanded && s.State == hubstore.StepDone {
			return true
		}
	}
	return false
}

// deriveTested is the `tested` step: the newest test at the branch tip, else the newest test there is, shown with how many
// commits ago it was. The bool is that the deciding test was run with uncommitted changes.
func deriveTested(in DeriveIn) (hubstore.WorkStepRow, bool) {
	row := hubstore.WorkStepRow{Step: hubstore.StepTested, State: hubstore.StepPending, Cards: "[]", Reason: "no test has been recorded"}
	var tests []hubstore.WorkFact
	for _, f := range in.Facts {
		if f.Kind == hubstore.FactTest {
			tests = append(tests, f)
		}
	}
	if len(tests) == 0 {
		return row, false
	}
	tip, known := in.Evidence.Tip, in.Evidence.Known && in.Evidence.Tip != ""
	// AT THE TIP, A TEST ATRIUM SAW BEATS ONE A CARD REPORTED, and either beats an older one. A pass beats a fail only by being
	// newer: fixing and re-running is the normal way a test goes green, and the older red stays in the facts.
	pick := tests[len(tests)-1]
	if known {
		best := -1
		for i, f := range tests {
			if f.AtSHA != tip {
				continue
			}
			if best < 0 || f.Source == hubstore.SourceSeen || tests[best].Source != hubstore.SourceSeen {
				best = i
			}
		}
		if best >= 0 {
			pick = tests[best]
		}
	}
	row.StartedAt, row.EndedAt, row.SHA = tests[0].At, pick.At, pick.AtSHA
	row.Cards = hubstore.CardsJSON(cardsOf(pick))
	row.Tag = hubstore.TagReported
	if pick.Source == hubstore.SourceSeen {
		row.Tag = hubstore.TagObserved
	}
	cmd := pick.Note("cmd")
	at := short(pick.AtSHA)
	passed := pick.Note("result") == hubstore.ResultPass
	n, have := in.Evidence.Since[pick.AtSHA]
	switch {
	case !passed:
		row.State, row.Party = hubstore.StepActive, hubstore.PartyCard
		row.Reason = "the test failed at " + at + withCmd(cmd)
		return row, pick.Dirty
	case known && pick.AtSHA != tip && have && n < 0:
		row.State = hubstore.StepStale
		row.Reason = "tested at " + at + ", which is not on the branch any more"
	case known && pick.AtSHA != tip && have && n > 0:
		row.State = hubstore.StepStale
		row.Reason = "tested at " + at + ", " + plural(n, "commit") + " ago"
	default:
		// At the tip, or the branch could not be read (then a done step stays done).
		row.State = hubstore.StepDone
		row.Reason = "tested at " + at + withCmd(cmd)
	}
	if pick.Dirty {
		row.Reason += " plus uncommitted changes"
	}
	if pick.Source == hubstore.SourceReported {
		row.Reason += ", reported by the card"
	}
	return row, pick.Dirty
}

func withCmd(cmd string) string {
	if cmd == "" {
		return ""
	}
	return " (" + cmd + ")"
}

func cardsOf(f hubstore.WorkFact) []string {
	if f.Card == "" {
		return nil
	}
	return []string{f.Card}
}

// deriveReviewed is the `reviewed` step: the verdict trailers on the item's own commits, read by deployready's matcher, or a
// review atrium took itself. Both are OBSERVED, and no card writes one.
func deriveReviewed(in DeriveIn) hubstore.WorkStepRow {
	row := hubstore.WorkStepRow{Step: hubstore.StepReviewed, State: hubstore.StepPending, Cards: "[]",
		Reason: "no verdict covers this branch yet", Tag: hubstore.TagObserved}
	var rev hubstore.WorkFact
	haveRev := false
	for _, f := range in.Facts {
		if f.Kind == hubstore.FactReview {
			rev, haveRev = f, true
		}
	}
	ev := in.Evidence
	known := ev.Known && ev.Tip != ""
	cv := ev.Cover
	trailers := known && ev.CoverKnown && cv.Total > 0
	verdict := rev.Note("verdict")
	switch {
	case trailers && cv.Held > 0:
		row.State, row.Party, row.SHA = hubstore.StepActive, hubstore.PartyCard, ev.Tip
		row.Reason = "held by a review verdict on " + itoaN(cv.Held) + " of " + plural(cv.Total, "commit")
	case haveRev && verdict == hubstore.VerdictHold && (!known || rev.AtSHA == ev.Tip):
		row.State, row.Party, row.SHA, row.EndedAt = hubstore.StepActive, hubstore.PartyCard, rev.AtSHA, rev.At
		row.Reason = "held by the review at " + short(rev.AtSHA)
	case trailers && cv.Covered == cv.Total:
		row.State, row.SHA = hubstore.StepDone, ev.Tip
		row.Reason = "a verdict covers " + plural(cv.Total, "commit") + " of the branch"
	case haveRev && verdict == hubstore.VerdictOK && (!known || rev.AtSHA == ev.Tip):
		row.State, row.SHA, row.EndedAt = hubstore.StepDone, rev.AtSHA, rev.At
		row.Reason = "reviewed at " + short(rev.AtSHA)
	case trailers && cv.Covered > 0:
		row.State, row.SHA = hubstore.StepStale, ev.Tip
		row.Reason = "covers " + itoaN(cv.Covered) + " of " + plural(cv.Total, "commit")
	case haveRev && verdict == hubstore.VerdictOK:
		row.State, row.SHA, row.EndedAt = hubstore.StepStale, rev.AtSHA, rev.At
		if n, ok := ev.Since[rev.AtSHA]; ok && n > 0 {
			row.Reason = "reviewed at " + short(rev.AtSHA) + ", " + plural(n, "commit") + " ago"
		} else {
			row.Reason = "reviewed at " + short(rev.AtSHA) + ", and the branch has moved since"
		}
	case known && ev.CoverKnown && cv.Total == 0:
		row.Reason = "the branch has no commits of its own to review"
	}
	if haveRev {
		row.Cards = hubstore.CardsJSON(cardsOf(rev))
	}
	return row
}

// readiness is whether the item is ready in the change record's sense: tested AND reviewed at the tip, with the test SEEN by
// atrium on a clean tree. A card's own "tests pass" is shown on the timeline and does not count, so a reported test says why.
func readiness(tested, reviewed hubstore.WorkStepRow, dirty bool) (bool, string) {
	switch {
	case tested.State != hubstore.StepDone:
		return false, "not tested at the tip: " + tested.Reason
	case tested.Tag != hubstore.TagObserved:
		return false, "the test is reported by a card, not seen by atrium. only a test atrium saw counts"
	case dirty:
		return false, "the test ran with uncommitted changes, so the commit itself is not tested"
	case reviewed.State != hubstore.StepDone:
		return false, "not reviewed at the tip: " + reviewed.Reason
	}
	return true, ""
}

// itemState is the item's own state, from its steps and its assignments. Blocked is anything waiting on a person or on a
// worker's question. Running is a card working. Queued is work nobody has picked up yet, and finished is everything this
// stage can see being done.
func itemState(l Lifecycle, as []hubstore.Item) (state, waiting string) {
	blocked := false
	for _, s := range l.Steps {
		switch {
		case s.State == hubstore.StepActive && s.Party == hubstore.PartyHuman:
			return hubstore.ItemStateBlocked, hubstore.PartyHuman
		case s.State == hubstore.StepBlocked:
			blocked = true
		}
	}
	if blocked {
		return hubstore.ItemStateBlocked, ""
	}
	queued := false
	for _, a := range as {
		switch a.State {
		case hubstore.ItemStateRunning:
			return hubstore.ItemStateRunning, ""
		case hubstore.ItemStateQueued:
			queued = true
		}
	}
	if queued {
		return hubstore.ItemStateQueued, ""
	}
	switch l.Current {
	case hubstore.StepDesigned, hubstore.StepApproved, hubstore.StepBuilding:
		return hubstore.ItemStateQueued, ""
	}
	return hubstore.ItemStateFinished, ""
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func plural(n int, w string) string {
	if n == 1 {
		return "1 " + w
	}
	return itoaN(n) + " " + w + "s"
}

// engineSteps are the five steps Derive writes. The other three are L3's, read from git.
var engineSteps = []string{hubstore.StepDesigned, hubstore.StepApproved, hubstore.StepBuilding, hubstore.StepTested, hubstore.StepReviewed}

func engineStep(s string) bool {
	for _, x := range engineSteps {
		if x == s {
			return true
		}
	}
	return false
}

// steps is the map as a list in step order, with the places no row fills left out.
func steps(by map[string]hubstore.WorkStepRow) []hubstore.WorkStepRow {
	var out []hubstore.WorkStepRow
	for _, step := range hubstore.Steps {
		if s, ok := by[step]; ok {
			out = append(out, s)
		}
	}
	return out
}

// completeSteps puts rows in step order, fills every missing step as pending, and says which is current.
func completeSteps(rows []hubstore.WorkStepRow) Lifecycle {
	by := map[string]hubstore.WorkStepRow{}
	for _, s := range rows {
		by[s.Step] = s
	}
	out := Lifecycle{Steps: []hubstore.WorkStepRow{}}
	for _, step := range hubstore.Steps {
		s, ok := by[step]
		if !ok {
			s = hubstore.WorkStepRow{Step: step, State: hubstore.StepPending}
		}
		if s.Cards == "" {
			s.Cards = "[]"
		}
		out.Steps = append(out.Steps, s)
	}
	for _, s := range out.Steps {
		if s.State != hubstore.StepDone && s.State != hubstore.StepSkipped {
			out.Current = s.Step
			break
		}
	}
	return out
}
