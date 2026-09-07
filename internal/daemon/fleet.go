package daemon

import (
	"sort"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// Which of these want me.
//
// THE GAP THIS CLOSES IS NOT IN ATRIUM. Sixteen agents ran for hours and the
// operator found out which ones had finished by asking them, one at a time.
// Everything needed was already written down: `atrium finish` files a recap on
// a card, `atrium ask` puts the question on it, `waiting_reason` says a
// question was asked, and the activity tracker knows whether anything has been
// heard from a session at all. Nothing read any of it together.
//
// So this adds no state and no schema. It is a classification over facts that
// already exist, plus an order to read them in.
//
// NOTHING LIVE IS STORED. The quiet case is decided from the activity tracker,
// which lives in memory and dies with the daemon, exactly as
// `docs/activity-design.md` requires. After a restart atrium has heard nothing
// from anybody, so everything running reads as quiet, which is TRUE: atrium
// does not know what those sessions are doing. It is not a stored claim that
// went stale, it is the absence of one.

// What a card wants from you. Ordered by how much it wants it, which is the
// order the list comes back in.
//
// FOUR OF THESE WERE REPEATEDLY CONFUSED, and telling them apart is the whole
// point: a session that finished and filed a recap, a session that stopped and
// asked a question, a session still working that asked one anyway, and a
// session that has gone quiet with nothing recorded. The last is the one
// nobody knows to look at, because it has never asked for anything.
const (
	// WantBlocked is a session that stopped and asked. It cannot go on until
	// somebody answers, and there are words on the card saying what it needs.
	//
	// First because it is the only one that is both stopped and specific.
	WantBlocked = "blocked"
	// WantPermission is a session held at the permission gate. Also stopped,
	// and answered with a decision rather than with thought.
	WantPermission = "permission"
	// WantQuestion is `atrium ask --continue`: a question from a session that
	// is carrying on regardless. Worth an answer, never worth an interruption.
	WantQuestion = "question"
	// WantFinished is a session that declared its work over. It wants reading,
	// not answering.
	WantFinished = "finished"
	// WantQuiet is a session atrium has heard nothing from and that has said
	// nothing about itself. No recap, no question, no activity.
	//
	// THE ONE NOBODY LOOKS AT, which is why it ranks above the sessions that
	// are visibly working. A session that is fine and a session that died an
	// hour ago look identical from here, and that is the point: the only way
	// to find out is to go and look, and until now nothing said which ones to
	// go and look at.
	WantQuiet = "quiet"
	// WantWorking is a session that is moving and has asked for nothing.
	// Nothing to do about it.
	WantWorking = "working"
)

// wantOrder ranks the buckets. Lower sorts first.
var wantOrder = map[string]int{
	WantBlocked: 0, WantPermission: 1, WantQuestion: 2,
	WantFinished: 3, WantQuiet: 4, WantWorking: 5,
}

// quietAfter is how long without a word before a session counts as quiet.
//
// The same fifteen minutes the activity tracker believes an activity for, and
// for the same reason: shorter writes off a slow build, longer means a session
// can be dead for most of an hour before anything says so.
const quietAfter = 15 * time.Minute

// classify decides what one card wants, and says so in a line a person reads.
//
// Order matters here the way it matters in the permission chain. A card that
// asked and stopped is blocked whatever else is true of it, and a card that
// finished is finished even though it is also, now, completely silent.
func classify(t *store.Task, rep *store.AgentReport, act *Activity, now time.Time) (want, why string) {
	switch t.Status {
	case store.StatusDone:
		// A SESSION THAT FINISHED, not a card that is in `done`.
		//
		// Those are different and the board is full of the second. Work
		// dragged across by hand, adopted cards, anything the sweep tidied:
		// all `done`, none of them a session that just ended and left
		// something to read. Listing them buried nineteen real recaps under
		// twenty five rows of archaeology on the first board this was pointed
		// at.
		//
		// So the claim has to come from the session: a recap it wrote, or the
		// `finished` it filed. Anything else is not part of what is running
		// here and drops out.
		if t.Recapped() {
			return WantFinished, "finished and filed a recap"
		}
		if rep != nil && rep.Kind == store.ReportFinished {
			return WantFinished, "finished and said nothing about what it did"
		}
		return "", ""
	case store.StatusNeedsPermission:
		return WantPermission, "held at the permission gate"
	case store.StatusNeedsInput:
		// A question survives the turn ending: `atrium ask` writes the words,
		// and the asking-tool path writes only the fact. Both are a question.
		if rep != nil && rep.Kind == store.ReportAsked {
			return WantBlocked, "stopped and asked: " + rep.Ask
		}
		if t.WaitingReason == store.WaitingAsked {
			return WantBlocked, "stopped and put a question to you"
		}
		if t.WaitingReason == store.WaitingStarted {
			return WantQuiet, "started and has not been given anything to do"
		}
		// The Stop hook and nothing else. Atrium knows the turn ended and
		// knows nothing about why, which is the case this list exists for.
		return WantQuiet, "stopped and said nothing"
	}

	// Still running. A question asked while working does not move the card, so
	// the timeline is the only place it is written down.
	if rep != nil && rep.Kind == store.ReportAsked && !rep.Blocked {
		return WantQuestion, "still working, and asked: " + rep.Ask
	}
	if act != nil {
		return WantWorking, describeActivity(act)
	}
	// Nothing in the activity tracker means either atrium has never heard from
	// this session or the last thing it heard is past the cutoff. Either way
	// there is no word, so the card's own last activity is all there is.
	silent := now.Sub(t.LastActivityAt)
	if silent >= quietAfter {
		return WantQuiet, "no word for " + shortDuration(silent)
	}
	return WantWorking, "running"
}

// describeActivity says what a runner is doing, in a few words.
func describeActivity(a *Activity) string {
	out := a.What
	if a.What == ActivityTool && a.Tool != "" {
		out = "running " + a.Tool
	}
	if a.Seconds > 0 {
		out += " for " + shortDuration(time.Duration(a.Seconds)*time.Second)
	}
	if a.Subagents > 0 {
		out += ", " + plural(a.Subagents, "subagent")
	}
	return out
}

// plural writes a count with its noun, which is one `if` nobody should write
// twice.
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return itoa(n) + " " + noun + "s"
}

// shortDuration writes an age the way a person says it: `40s`, `12m`, `3h`.
//
// Never more than two units and never fractional. This is read at a glance in
// a list of sixteen, and "1h37m12.4s" is not.
func shortDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return itoa(int(d.Seconds())) + "s"
	case d < time.Hour:
		return itoa(int(d.Minutes())) + "m"
	case d < 24*time.Hour:
		h := int(d.Hours())
		m := int(d.Minutes()) - h*60
		if m == 0 {
			return itoa(h) + "h"
		}
		return itoa(h) + "h" + itoa(m) + "m"
	default:
		return itoa(int(d.Hours()/24)) + "d"
	}
}

// itoa without dragging strconv through every call site here.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// defaultFinishedWithin is how far back finished work is worth being told
// about.
//
// A dispatcher asking which sessions want them is asking about the shift they
// are on. Work that ended yesterday has either been read or has stopped being
// news, and a list that carries it is a list nobody reads to the bottom of.
// Everything that ever ran here is a different question and `history` answers
// it.
const defaultFinishedWithin = 12 * time.Hour

// roster is the whole board with the default window on finished work.
func (d *Daemon) roster(exclude string, finished bool) ([]Peer, error) {
	return d.rosterWithin(exclude, finished, defaultFinishedWithin)
}

// roster lists sessions with what each one wants, most wanting first.
//
// `finished` decides whether cards that are over are included, and `within`
// bounds how recently they had to finish. They are not addressable, so the peer
// list leaves them out: telling a model it can message a session that has ended
// wastes a turn and produces a message nobody reads. A dispatcher asking which
// sessions want them needs exactly the opposite, because "it finished and there
// is a recap to read" is the answer they were polling for by hand.
func (d *Daemon) rosterWithin(exclude string, finished bool, within time.Duration) ([]Peer, error) {
	tasks, err := d.st.List()
	if err != nil {
		return nil, err
	}
	waiting, err := d.st.UndeliveredCounts()
	if err != nil {
		// Not fatal. The list is still worth having without the counts.
		waiting = map[string]int{}
	}
	reports, err := d.st.LatestAgentReports()
	if err != nil {
		// Also not fatal, and it costs only the words on a question. A card
		// that asked still reads as blocked, from its waiting reason.
		reports = map[string]store.AgentReport{}
	}
	exclude = d.st.Qualify(strings.TrimSpace(exclude))
	now := time.Now()

	out := make([]Peer, 0, len(tasks))
	for _, t := range tasks {
		if t.WireName == "" || t.WireName == exclude {
			continue
		}
		switch t.Status {
		case store.StatusDead, store.StatusBacklog:
			continue
		case store.StatusDone:
			if !finished {
				continue
			}
		}
		var rep *store.AgentReport
		if r, ok := reports[t.ID]; ok {
			rep = &r
		}
		var act *Activity
		if a := d.act.get(t.ID); a != nil {
			act = a
		}
		want, why := classify(t, rep, act, now)
		if want == "" {
			// A card in `done` that no session ever claimed. Not part of what
			// is running here.
			continue
		}
		p := Peer{
			Handle: t.WireName, Title: t.DisplayTitle(), Status: t.Status,
			Runner: t.Runner, Worktree: t.Worktree, Why: t.Why,
			Waiting: waiting[t.ID], Want: want, Note: why, AskPeer: t.AskPeer,
		}
		if rep != nil && rep.Kind == store.ReportAsked {
			p.Ask, p.Blocked, p.Since = rep.Ask, rep.Blocked, rep.At
		}
		if want == WantFinished {
			p.Recap = t.Recap
			switch {
			case t.RecapAt != nil:
				p.Since = *t.RecapAt
			case rep != nil:
				p.Since = rep.At
			default:
				p.Since = t.LastActivityAt
			}
			// Older than the window is work that has been read or has stopped
			// being news. Bounded here rather than in the query, because how
			// long a session has wanted somebody is the same calculation
			// either way and there is one place it is done.
			if now.Sub(p.Since) > within {
				continue
			}
		}
		if p.Since.IsZero() {
			if t.WaitingSince != nil {
				p.Since = *t.WaitingSince
			} else {
				p.Since = t.LastActivityAt
			}
		}
		p.Seconds = int64(now.Sub(p.Since).Seconds())
		if p.Seconds < 0 {
			p.Seconds = 0
		}
		out = append(out, p)
	}

	// Most wanting first, and within a bucket the one that has been like that
	// longest. Waiting longest is a fact, not a judgement, which is the same
	// rule the Stack view follows.
	sort.SliceStable(out, func(i, j int) bool {
		if wantOrder[out[i].Want] != wantOrder[out[j].Want] {
			return wantOrder[out[i].Want] < wantOrder[out[j].Want]
		}
		return out[i].Seconds > out[j].Seconds
	})
	return out, nil
}
