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

func cardKey(room, card string) string { return strings.ToLower(strings.TrimSpace(room)) + "/" + strings.TrimSpace(card) }

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

	// The steps L3 reads from git are taken as stored, and the ones no stage has built yet stay pending, so the bar has
	// eight places and the item's current step is honest.
	for _, s := range in.Stored {
		if !engineStep(s.Step) && hubstore.ValidStep(s.Step) {
			put(s)
		}
	}
	out := completeSteps(steps(byStep))
	out.State, out.Waiting = itemState(out, in.Assignments)
	return out
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
