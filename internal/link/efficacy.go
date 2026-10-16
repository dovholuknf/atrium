package link

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

// EFFICACY, THE PURE PART (item lifecycle L9, docs/rnd/item-lifecycle-design.md section 12). This file reads what an item
// has (its steps, facts, assignments, events and cost) and answers its outcome, one row of `work_outcome`. It reads no
// clock, no git and no store, so a changed rule (the 7 days, the 5 items) is a recompute and not a data fix.
//
//   - OBSERVED BEATS REPORTED, AND THE TAG TRAVELS. An event says which it is. A figure that used a reported input says so,
//     and the observed-only filter drops those inputs from every figure, so the numbers are not flattered by a card's say-so.
//   - A DASH IS NOT A ZERO. An item that has not reached a step, has no test fact, or has no priced rows has no measure for
//     it, and the roll-ups count only the items that have, saying how many that was.
//   - A SEND-BACK IS COUNTED BY THE TRANSITION, not the note: two at different times are two even when the notes repeat. The
//     same one recorded twice is one, because the event's key is its time and sha.
//   - IT SCORES THE PROCESS. A send-back is a fact about a step, not a grade for the card that did it, and nothing here
//     stops anything.

const (
	// EffWatchDays is how long after landing a bug or a red CI still counts against the item, and so how long a clean pass
	// stays unproven.
	EffWatchDays = 7
	// EffMinItems is the fewest landed items a figure is trended on.
	EffMinItems = 5
)

// The verdicts of a landed item.
const (
	VerdictClean    = "clean"
	VerdictWatching = "watching"
	VerdictNotClean = "not_clean"
	// VerdictUnproven is an item whose 7 days are over and that cannot be called clean for want of a test fact, or of a CI
	// that could be read. It is left out of the clean-pass rate, since a dash is not a zero.
	VerdictUnproven = "unproven"
)

// EffBug is a bug filed against an item.
type EffBug struct {
	ID     string `json:"id"`
	At     string `json:"at"`
	Source string `json:"source"`
}

// EffDetail is the JSON an outcome keeps beside its columns: what the roll-ups and the item's strip need that a column cannot say.
type EffDetail struct {
	FiledAt string `json:"filed_at,omitempty"`
	// SendBacks and SendBacksReported are counts by kind: design, review, not_accepted.
	SendBacks         map[string]int `json:"send_backs"`
	SendBacksReported map[string]int `json:"send_backs_reported"`
	// ByStep and ByStepReported are the same counts by the step they were sent back from.
	ByStep         map[string]int `json:"by_step"`
	ByStepReported map[string]int `json:"by_step_reported"`
	// FirstPassAt is when the first test result was seen. FirstPassReported says the result is somebody's say-so.
	FirstPassAt       string `json:"first_pass_at,omitempty"`
	FirstPassReported bool   `json:"first_pass_reported,omitempty"`
	// RedEpisodes counts the reds that name the item, RedOpen says the last has not gone green.
	RedEpisodes int  `json:"red_episodes"`
	RedOpen     bool `json:"red_open,omitempty"`
	// CIKnown is whether CI could be read since the item landed. A clean pass is not given without it.
	CIKnown bool     `json:"ci_known"`
	Bugs    []EffBug `json:"bugs,omitempty"`
	Reverts []string `json:"reverts,omitempty"`
	// RevertAt is the time of each revert, parallel to Reverts.
	RevertAt []string `json:"revert_at,omitempty"`
	// StepSeconds is each step's window, TestSeconds the tested window alone, TotalSeconds filed (or launched) to landed.
	StepSeconds  map[string]int64 `json:"step_seconds,omitempty"`
	TestSeconds  int64            `json:"test_seconds,omitempty"`
	TotalSeconds int64            `json:"total_seconds,omitempty"`
	// LongestWait is the longest single wait on the human, and the step it was on.
	LongestWaitStep    string `json:"longest_wait_step,omitempty"`
	LongestWaitSeconds int64  `json:"longest_wait_seconds,omitempty"`
	CostRows           int    `json:"cost_rows"`
	Reason             string `json:"reason,omitempty"`
}

// EffIn is everything DeriveOutcome reads.
type EffIn struct {
	Work        hubstore.Work
	Steps       []hubstore.WorkStepRow
	Assignments []hubstore.Item
	Events      []hubstore.WorkEvent
	Cost        CostView
	// Backlog is the item's backlog row, nil when the hub holds none.
	Backlog *hubstore.BacklogItem
	// CIKnown is whether CI could be read on this pass.
	CIKnown bool
	Now     time.Time
}

func (d *EffDetail) fill() {
	for _, m := range []*map[string]int{&d.SendBacks, &d.SendBacksReported, &d.ByStep, &d.ByStepReported} {
		if *m == nil {
			*m = map[string]int{}
		}
	}
}

// ParseEffDetail reads an outcome's detail, empty when it is not there.
func ParseEffDetail(o hubstore.WorkOutcome) EffDetail {
	var d EffDetail
	_ = json.Unmarshal([]byte(o.Detail), &d)
	d.fill()
	return d
}

func evTime(e hubstore.WorkEvent) time.Time { return rowTime(e.At) }

func secs(d time.Duration) int64 {
	if d < 0 {
		return 0
	}
	return int64(d / time.Second)
}

func stepRow(steps []hubstore.WorkStepRow, name string) (hubstore.WorkStepRow, bool) {
	for _, s := range steps {
		if s.Step == name {
			return s, true
		}
	}
	return hubstore.WorkStepRow{}, false
}

// LandedAt is when an item landed, from its `landed` step: the end of the step, which is when the hub first read the branch on
// claude/main. Empty when it has not.
func LandedAt(steps []hubstore.WorkStepRow) string {
	s, ok := stepRow(steps, hubstore.StepLanded)
	if !ok || s.State != hubstore.StepDone {
		return ""
	}
	if s.EndedAt != "" {
		return s.EndedAt
	}
	return s.StartedAt
}

// stepWindowOf is a step's window, open to now while it is active.
func stepWindowOf(s hubstore.WorkStepRow, now time.Time) (a, b time.Time, ok bool) {
	a = rowTime(s.StartedAt)
	if a.IsZero() || s.State == hubstore.StepSkipped {
		return a, b, false
	}
	b = rowTime(s.EndedAt)
	if b.IsZero() {
		if s.State == hubstore.StepDone {
			return a, a, true
		}
		b = now
	}
	if b.Before(a) {
		b = a
	}
	return a, b, true
}

// firstPassOf reads the first test result: the earliest observed one when there is one, else the earliest reported, and
// whether it was reported.
func firstPassOf(events []hubstore.WorkEvent) (result, at string, reported bool) {
	var best *hubstore.WorkEvent
	for i := range events {
		e := events[i]
		if e.Kind != hubstore.EventTestResult {
			continue
		}
		if best == nil || (best.Source == hubstore.EvReported && e.Source == hubstore.EvObserved) ||
			(best.Source == e.Source && e.At < best.At) {
			c := e
			best = &c
		}
	}
	if best == nil {
		return "", "", false
	}
	res := strings.ToLower(strings.TrimSpace(best.Note))
	switch {
	case strings.HasPrefix(res, "pass"), res == "ok", res == "green":
		res = "pass"
	case res != "":
		res = "fail"
	default:
		return "", "", false
	}
	return res, best.At, best.Source == hubstore.EvReported
}

// DeriveOutcome is the whole outcome of one item.
func DeriveOutcome(in EffIn) hubstore.WorkOutcome {
	d := EffDetail{CIKnown: in.CIKnown, CostRows: in.Cost.Rows}
	d.fill()
	o := hubstore.WorkOutcome{WorkID: in.Work.BacklogID, ComputedAt: in.Now.UTC().Format(time.RFC3339)}
	landedAt := LandedAt(in.Steps)
	landed := rowTime(landedAt)
	o.LandedAt = landedAt
	d.FiledAt = in.Work.CreatedAt

	// ── send-backs, test, CI, bugs, reverts: the events, counted by what they are ──
	var redAt []time.Time
	var greenAt []time.Time
	watchEnd := landed.Add(EffWatchDays * 24 * time.Hour)
	if !landed.IsZero() {
		o.WatchEndsAt = watchEnd.UTC().Format(time.RFC3339)
	}
	for _, e := range in.Events {
		at := evTime(e)
		reported := e.Source == hubstore.EvReported
		count := func(kind string) {
			d.SendBacks[kind]++
			d.ByStep[e.Step]++
			if reported {
				d.SendBacksReported[kind]++
				d.ByStepReported[e.Step]++
			}
		}
		switch e.Kind {
		case hubstore.EventSendBack:
			count("design")
		case hubstore.EventNotAccepted:
			count("not_accepted")
		case hubstore.EventReviewVerdct:
			if n := strings.ToLower(strings.TrimSpace(e.Note)); strings.HasPrefix(n, "changes") || strings.HasPrefix(n, hubstore.VerdictHold) {
				count("review")
			}
		case hubstore.EventCIRed:
			if !landed.IsZero() && !at.Before(landed) && !at.After(watchEnd) {
				redAt = append(redAt, at)
			}
		case hubstore.EventCIGreen:
			greenAt = append(greenAt, at)
		case hubstore.EventBugFiled:
			if !landed.IsZero() && !at.Before(landed) && !at.After(watchEnd) {
				d.Bugs = append(d.Bugs, EffBug{ID: e.Note, At: e.At, Source: e.Source})
			}
		case hubstore.EventRevert:
			if !landed.IsZero() && !at.Before(landed) {
				d.Reverts = append(d.Reverts, e.SHA)
				d.RevertAt = append(d.RevertAt, e.At)
			}
		}
	}
	sort.Slice(redAt, func(i, j int) bool { return redAt[i].Before(redAt[j]) })
	d.RedEpisodes = len(redAt)
	for _, r := range redAt {
		end := in.Now
		for _, g := range greenAt {
			if g.After(r) && g.Before(end) {
				end = g
			}
		}
		if end.Equal(in.Now) {
			d.RedOpen = true
		}
		o.CIRedSeconds += secs(end.Sub(r))
	}
	o.FirstPass, _, o.FirstPassSource = "", "", ""
	if res, at, rep := firstPassOf(in.Events); res != "" {
		o.FirstPass, d.FirstPassAt, d.FirstPassReported = res, at, rep
		o.FirstPassSource = hubstore.EvObserved
		if rep {
			o.FirstPassSource = hubstore.EvReported
		}
	}
	o.EscapedBugs, o.Reverted = len(d.Bugs), len(d.Reverts)

	// ── time: the window of each step, the wait on the human, and filed to landed ──
	d.StepSeconds = map[string]int64{}
	start := rowTime(in.Work.CreatedAt)
	var wait int64
	for _, s := range in.Steps {
		a, b, ok := stepWindowOf(s, in.Now)
		if !ok {
			continue
		}
		d.StepSeconds[s.Step] = secs(b.Sub(a))
		if start.IsZero() || (a.Before(start) && !a.IsZero()) {
			start = a
		}
		if s.Step == hubstore.StepTested {
			d.TestSeconds = secs(b.Sub(a))
		}
		if s.Party == hubstore.PartyHuman || (s.Step == hubstore.StepApproved && s.State != hubstore.StepSkipped) {
			w := secs(b.Sub(a))
			wait += w
			if w > d.LongestWaitSeconds {
				d.LongestWaitSeconds, d.LongestWaitStep = w, s.Step
			}
		}
	}
	// A question waiting on the human right now: the history of earlier ones is not kept, which is said and not guessed at.
	for _, a := range in.Assignments {
		if a.State == hubstore.ItemStateBlocked && a.OwnerKind == "human" {
			if t := rowTime(a.UpdatedAt); !t.IsZero() {
				w := secs(in.Now.Sub(t))
				wait += w
				if w > d.LongestWaitSeconds {
					d.LongestWaitSeconds, d.LongestWaitStep = w, "blocked"
				}
			}
		}
	}
	if !landed.IsZero() && !start.IsZero() {
		d.TotalSeconds = secs(landed.Sub(start))
		if wait > d.TotalSeconds {
			wait = d.TotalSeconds
		}
	}
	o.WaitHumanSeconds = wait
	if work := d.TotalSeconds - wait - d.TestSeconds; work > 0 && !landed.IsZero() {
		o.WorkSeconds = work
	}

	// ── cost, from the real tokens each time ──
	o.StepCost, o.CoordCost = in.Cost.Headline.Units, in.Cost.Coordination.Units
	o.CostKnown = in.Cost.Rows > 0 && in.Cost.Headline.Known && in.Cost.Coordination.Known

	// ── the verdict ──
	if !landed.IsZero() {
		sb := d.SendBacks["design"] + d.SendBacks["review"] + d.SendBacks["not_accepted"]
		o.Verdict, d.Reason = EffVerdict(in.Now, watchEnd, sb, o.FirstPass, d.RedEpisodes > 0, len(d.Bugs), len(d.Reverts), in.CIKnown)
		if o.Verdict == VerdictClean {
			o.CleanAt = watchEnd.UTC().Format(time.RFC3339)
		}
	}

	// ── abandoned ──
	if at, why, ok := abandonedAt(in); ok {
		o.AbandonedAt = at
		o.AbandonedCost = o.StepCost + o.CoordCost
		if d.Reason == "" {
			d.Reason = why
		}
	}
	b, _ := json.Marshal(d)
	o.Detail = string(b)
	return o
}

// EffVerdict says whether a landed item earned the clean pass: no send-back at any step, a first-pass test pass, and no CI
// red, bug or revert, for the 7 days. Anything already wrong is `not_clean` at once, since time cannot undo it. A clean item
// is `watching` until the 7 days end, and `unproven` when they end with no test fact or no readable CI.
func EffVerdict(now, watchEnd time.Time, sendBacks int, firstPass string, red bool, bugs, reverts int, ciKnown bool) (verdict, reason string) {
	switch {
	case reverts > 0:
		return VerdictNotClean, "reverted"
	case red:
		return VerdictNotClean, "CI red after landing"
	case bugs > 0:
		return VerdictNotClean, "bug filed after landing"
	case firstPass == "fail":
		return VerdictNotClean, "first test failed"
	case sendBacks > 0:
		return VerdictNotClean, "sent back"
	case now.Before(watchEnd):
		return VerdictWatching, ""
	case firstPass == "":
		return VerdictUnproven, "no test result was seen"
	case !ciKnown:
		return VerdictUnproven, "CI could not be read"
	}
	return VerdictClean, ""
}

// abandonedAt says whether an item that did not land has been given up on, when, and why: dropped or left incomplete in the
// backlog, or sitting stale or blocked for more than 7 days with no card running.
func abandonedAt(in EffIn) (at, why string, ok bool) {
	if LandedAt(in.Steps) != "" {
		return "", "", false
	}
	if b := in.Backlog; b != nil && (b.Status == "dropped" || b.Status == "incomplete") && rowTime(b.ChangedAt).Year() > 1 {
		return b.ChangedAt, "backlog " + b.Status, true
	}
	var stuck bool
	for _, s := range in.Steps {
		if s.State == hubstore.StepStale || s.State == hubstore.StepBlocked {
			stuck = true
		}
	}
	if !stuck {
		return "", "", false
	}
	last := rowTime(in.Work.UpdatedAt)
	for _, a := range in.Assignments {
		if a.State == hubstore.ItemStateRunning || a.State == hubstore.ItemStateQueued {
			return "", "", false
		}
		if t := rowTime(a.UpdatedAt); t.After(last) {
			last = t
		}
	}
	for _, s := range in.Steps {
		for _, x := range []string{s.StartedAt, s.EndedAt} {
			if t := rowTime(x); t.After(last) {
				last = t
			}
		}
	}
	if last.IsZero() {
		return "", "", false
	}
	end := last.Add(EffWatchDays * 24 * time.Hour)
	if end.After(in.Now) {
		return "", "", false
	}
	return end.UTC().Format(hubstore.TimeFormat), "no card running for more than 7 days", true
}

// ── the events a collect writes, from what the engine and the backlog already hold ──

// FactEvents are the events the item's facts say, which are the same every time they are read, so writing them twice writes
// them once. The fact kinds `test`, `review` and `not_accepted` are L4's and the accept verb's: until they are written here
// there are none, and the figures say dash.
func FactEvents(id string, facts []hubstore.WorkFact) []hubstore.WorkEvent {
	var out []hubstore.WorkEvent
	src := func(f hubstore.WorkFact) string {
		if f.Source == hubstore.SourceReported {
			return hubstore.EvReported
		}
		return hubstore.EvObserved
	}
	firstTest := true
	sorted := append([]hubstore.WorkFact(nil), facts...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Seq < sorted[j].Seq })
	for _, f := range sorted {
		switch f.Kind {
		case hubstore.FactSendBack:
			out = append(out, hubstore.WorkEvent{WorkID: id, Kind: hubstore.EventSendBack, Step: hubstore.StepDesigned, At: f.At,
				SHA: f.AtSHA, Note: f.Note("note"), Source: hubstore.EvObserved})
		case hubstore.FactTest:
			// Only the first test counts: fixing and running it again never rewrites history.
			if !firstTest {
				continue
			}
			firstTest = false
			res := effFirst(f.Note("result"), f.Note("outcome"), f.Note("status"))
			if res == "" {
				continue
			}
			out = append(out, hubstore.WorkEvent{WorkID: id, Kind: hubstore.EventTestResult, Step: hubstore.StepTested, At: f.At,
				SHA: f.AtSHA, Note: res, Source: src(f)})
		case hubstore.FactReview:
			v := effFirst(f.Note("verdict"), f.Note("result"))
			if v == "" {
				continue
			}
			note := v
			var pl struct {
				Findings int `json:"findings"`
			}
			if json.Unmarshal([]byte(f.Payload), &pl) == nil && pl.Findings > 0 {
				note += fmt.Sprintf(" %d findings", pl.Findings)
			}
			out = append(out, hubstore.WorkEvent{WorkID: id, Kind: hubstore.EventReviewVerdct, Step: hubstore.StepReviewed, At: f.At,
				SHA: f.AtSHA, Note: note, Source: src(f)})
		case hubstore.EventNotAccepted:
			out = append(out, hubstore.WorkEvent{WorkID: id, Kind: hubstore.EventNotAccepted, Step: hubstore.StepBuilding, At: f.At,
				SHA: f.AtSHA, Note: f.Note("note"), Source: src(f)})
		}
	}
	return out
}

// NotAcceptedNote says whether an orchestrator's message to a card is a "not accepted" for an item, and the reason after it.
// This is the reported way to arrive at measure 3, until the accept verb records it: the message names the item and starts
// with the words.
func NotAcceptedNote(msg, id string) (reason string, ok bool) {
	m := strings.TrimSpace(msg)
	if !strings.HasPrefix(strings.ToLower(m), "not accepted") || id == "" || !strings.Contains(m, id) {
		return "", false
	}
	return strings.Trim(strings.TrimSpace(m[len("not accepted"):]), ":-. "), true
}

var fixesRe = regexp.MustCompile(`(?i)\bfixes[ :]+([A-Za-z0-9][A-Za-z0-9_.-]*)`)

// IsBug says whether a backlog row is a bug: its id starts `b-`, or its department is `bugs`, or its body says `kind: bug`.
func IsBug(b hubstore.BacklogItem) bool {
	return strings.HasPrefix(strings.ToLower(b.ID), "b-") || strings.EqualFold(b.Dept, "bugs") ||
		regexp.MustCompile(`(?im)^\s*kind:\s*bug\s*$`).MatchString(b.Body)
}

// BugsNaming are the bug_filed events for every item the backlog's bugs name. A bug that names an item with the tag
// `atrium:item:<id>` is observed, since filing through `--item` writes it. One that says `fixes <id>` is reported: somebody linked it by hand.
func BugsNaming(backlog []hubstore.BacklogItem, known map[string]bool) []hubstore.WorkEvent {
	var out []hubstore.WorkEvent
	for _, b := range backlog {
		if !IsBug(b) {
			continue
		}
		text := b.Title + "\n" + b.Body
		named := map[string]string{}
		for id := range known {
			if strings.Contains(text, "atrium:item:"+id) {
				named[id] = hubstore.EvObserved
			}
		}
		for _, m := range fixesRe.FindAllStringSubmatch(text, -1) {
			if known[m[1]] && named[m[1]] == "" {
				named[m[1]] = hubstore.EvReported
			}
		}
		for id, src := range named {
			if id == b.ID {
				continue
			}
			out = append(out, hubstore.WorkEvent{WorkID: id, Kind: hubstore.EventBugFiled, At: b.CreatedAt, Note: b.ID, Source: src})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].WorkID+out[i].Note < out[j].WorkID+out[j].Note })
	return out
}

// AbandonedEvent is the abandoned event an outcome's derivation implies, for the history.
func AbandonedEvent(o hubstore.WorkOutcome, reason string) (hubstore.WorkEvent, bool) {
	if o.AbandonedAt == "" {
		return hubstore.WorkEvent{}, false
	}
	return hubstore.WorkEvent{WorkID: o.WorkID, Kind: hubstore.EventAbandoned, At: o.AbandonedAt, Note: reason, Source: hubstore.EvObserved}, true
}

// ── what an item says of itself ──

// EffMark is the one word an item carries: the worst thing that happened to it, or that it was clean.
type EffMark struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
	// Tone is good, watch or bad, which the board draws in teal, the neutral chip and orange.
	Tone string `json:"tone"`
	// Rank orders worst first.
	Rank int `json:"rank"`
}

func shortDur(sec int64) string {
	m := (sec + 30) / 60
	switch {
	case m < 1:
		return "under 1m"
	case m < 60:
		return fmt.Sprintf("%dm", m)
	case m < 1440:
		return fmt.Sprintf("%dh %02dm", m/60, m%60)
	}
	return fmt.Sprintf("%dd %dh", m/1440, (m%1440)/60)
}

// MarkOf is an item's mark, from its outcome. An item that has not landed has none.
func MarkOf(o hubstore.WorkOutcome, now time.Time) (EffMark, bool) {
	if o.LandedAt == "" {
		return EffMark{}, false
	}
	d := ParseEffDetail(o)
	landed := rowTime(o.LandedAt)
	switch {
	case o.Reverted > 0:
		return EffMark{"revert", "reverted after landing", "bad", 0}, true
	case d.RedEpisodes > 0:
		t := "CI red after landing, " + shortDur(o.CIRedSeconds)
		if d.RedOpen {
			t = "CI red after landing, still red"
		}
		return EffMark{"ci_red", t, "bad", 1}, true
	case o.EscapedBugs > 0:
		age := "0d"
		if len(d.Bugs) > 0 {
			age = fmt.Sprintf("%dd", int(rowTime(d.Bugs[0].At).Sub(landed).Hours()/24))
		}
		return EffMark{"bug", "bug filed " + age + " after landing", "bad", 2}, true
	case o.FirstPass == "fail":
		return EffMark{"first_test", "first test failed", "bad", 3}, true
	case o.Verdict == VerdictNotClean:
		return EffMark{"sent_back", "sent back", "bad", 4}, true
	case o.Verdict == VerdictWatching:
		left := int((rowTime(o.WatchEndsAt).Sub(now) + 24*time.Hour - 1) / (24 * time.Hour))
		if left < 1 {
			left = 1
		}
		return EffMark{"watching", fmt.Sprintf("watching, %d %s left", left, effPlural(left, "day", "days")), "watch", 5}, true
	case o.Verdict == VerdictUnproven:
		return EffMark{"unproven", "unproven: " + d.Reason, "watch", 6}, true
	}
	return EffMark{"clean", "clean pass", "good", 7}, true
}

func effPlural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func effFirst(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
