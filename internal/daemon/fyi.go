package daemon

import (
	"strings"

	"github.com/dovholuknf/atrium/internal/store"
)

// fyi and needs: the two weights a report or a say may carry.
//
// A message that is only news should wait on the receiver's card and not start a turn.
// The orchestrator received 235 "ok" relays in two days, each one a full turn.
//
// THE SENDER CHOOSES, AND THE RECEIVER DECIDES WHETHER IT HOLDS. An fyi to a card that
// holds its notices (OrchestratorTag or HoldNoticesTag) is held on that card exactly like
// an automatic notice. An fyi to anything else is delivered as it always was.
//
// NEEDS IS THE DEFAULT, AND ANY UNKNOWN WORD IS NEEDS. A model that spells the kind wrong
// must still reach the receiver. The only failure this can cause is one extra turn.

// Kinds a report or a say may carry.
const (
	KindFYI   = "fyi"
	KindNeeds = "needs"
)

// NoticeFYI is the source of a held fyi, as `held notice` readers see it.
const NoticeFYI = "fyi"

// owedKind is the kind a say carries as owing sees it: a reply answers and asks nothing.
func owedKind(kind string, reply bool) string {
	if reply {
		return KindFYI
	}
	return parseKind(kind)
}

// parseKind is the kind a sender asked for. Never an error.
func parseKind(s string) string {
	if strings.EqualFold(strings.TrimSpace(s), KindFYI) {
		return KindFYI
	}
	return KindNeeds
}

// fyiSender is the sender's card when an fyi from it to target should be held, and nil when
// it should go the ordinary way: no sender card, a say to itself, a receiver that does not
// hold, or one nobody can read.
func (d *Daemon) fyiSender(from string, target *store.Task) *store.Task {
	from = strings.TrimSpace(from)
	if from == "" || target == nil || !holdsNotices(target) {
		return nil
	}
	sender, err := d.st.GetByWireName(d.st.Qualify(from))
	if err != nil || sender.ID == target.ID {
		return nil
	}
	if d.sayGate(target) == sayGone {
		return nil
	}
	return sender
}

// holdFyi records an fyi on the receiver's card and in the sender's say record. Nothing is
// typed and nothing is queued. It still counts as a report when the receiver is the
// sender's launcher, because peerSaid is what marks that.
func (d *Daemon) holdFyi(sender, target *store.Task, door, text string) {
	text = truncatePeer(text)
	d.holdNotice(target, sender, NoticeFYI, text)
	rec := sayRecordFor(sender.WireName, target, sayTrace{}, false, door, "", false)
	rec.State, rec.Note = store.SayDelivered, "held on the card as an fyi"
	d.recordSay(rec, text)
	d.peerSaid(sender.WireName, target, text, KindFYI)
}

// heldNoticesFor is how many held notices a card has not read and when the oldest was held,
// for the board's row. A card that does not hold notices has none.
func (d *Daemon) heldNoticesFor(t *store.Task) (int, string) {
	if !holdsNotices(t) {
		return 0, ""
	}
	n, oldest, err := d.st.HeldNoticeStats(t.ID)
	if err != nil || n == 0 {
		return 0, ""
	}
	return n, oldest.UTC().Format(store.TimeFormat)
}
