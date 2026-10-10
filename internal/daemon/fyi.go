package daemon

import (
	"log"
	"strings"

	"github.com/dovholuknf/atrium/internal/store"
)

// fyi and needs: the two weights a report or a say may carry.
//
// A message that is only news should wait on the receiver's card and not start a turn.
// The orchestrator received 235 "ok" relays in two days, each one a full turn.
//
// THE SENDER CHOOSES, AND NOTHING IS PARKED UNSEEN. An fyi to a card that holds its notices
// waits for the end of that card's turn and is then typed. See fyiWaitsForTurn.
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

// promptKind is the kind a say writes on the prompt it records: store.PromptFYI for an fyi
// that asks for no reply, which makes no report owed, and empty for anything else. A
// launcher's "stop, nothing else to do" sent as an fyi leaves its worker owing nothing, so
// no nudge and no silent-stop notice follow. See store.promptOwes.
func promptKind(kind string, reply bool) string {
	if !reply && parseKind(kind) == KindFYI {
		return store.PromptFYI
	}
	return ""
}

// parseKind is the kind a sender asked for. Never an error.
func parseKind(s string) string {
	if strings.EqualFold(strings.TrimSpace(s), KindFYI) {
		return KindFYI
	}
	return KindNeeds
}

// fyiWaitsForTurn reports whether an fyi from `from` to target waits for the end of target's turn.
//
// AN FYI IS NEVER PARKED UNSEEN. A card that holds its notices (OrchestratorTag or HoldNoticesTag) used to keep
// an fyi on the card and type nothing, and nothing read it. Now the fyi is delivered like `when: "done"`: typed
// at once when the card is idle, and at the end of its current turn when it is not. Only automatic notices that
// are not says stay held. The sender's own answer is `queued` with `when: "done"`, and `held` is kept for the
// hub or a room not answering.
func fyiWaitsForTurn(from string, target *store.Task) bool {
	if strings.TrimSpace(from) == "" || target == nil {
		return false
	}
	return holdsNotices(target)
}

// heldFyisRedeliveredKey is the setting that says the fyis parked before this change were delivered.
const heldFyisRedeliveredKey = "held_fyis_redelivered"

// redeliverHeldFyis delivers, once, the fyi says that were parked on a holding card before fyis stopped being
// held. Each is queued to wait for the card's next turn end. They stay on the card's held list as well, which is
// only a record now.
func (d *Daemon) redeliverHeldFyis() {
	if v, err := d.st.Setting(heldFyisRedeliveredKey); err != nil || v != "" {
		return
	}
	cards, err := d.st.List()
	if err != nil {
		log.Printf("[atrium] could not list cards to redeliver held fyis: %v", err)
		return
	}
	n := 0
	for _, t := range cards {
		if !holdsNotices(t) {
			continue
		}
		held, err := d.st.UnreadHeldNotices(t.ID)
		if err != nil {
			continue
		}
		for _, h := range held {
			if h.Source != NoticeFYI || strings.TrimSpace(h.Text) == "" {
				continue
			}
			from := h.About
			if from == "" {
				from = "atrium"
			}
			if _, _, err := d.deliverPeerCaused(t, from, h.Text, store.PromptFYI, "", true); err != nil {
				log.Printf("[atrium] could not redeliver a held fyi to %s: %v", t.DisplayTitle(), err)
				continue
			}
			n++
		}
	}
	if err := d.st.SetSetting(heldFyisRedeliveredKey, "1"); err != nil {
		log.Printf("[atrium] could not record the held fyi redelivery: %v", err)
	}
	if n > 0 {
		log.Printf("[atrium] delivered %d fyi(s) that were parked on a card", n)
	}
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
