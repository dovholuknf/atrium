package daemon

import (
	"github.com/dovholuknf/atrium/internal/store"
)

// Who idle parking applies to, and the orchestrator's rule. See
// docs/keepalive-policy-design.md section 7 and r-007 in docs/backlog-2.md.
//
// This is the question only. The reaper tick that asks it, and the clock, are
// the next stage.

// ParkIdleTag opts one of the operator's own cards into idle parking. Agent cards
// (`origin:agent`) are subject without it.
const ParkIdleTag = "atrium:park-idle"

// OrchestratorTag marks the orchestrator card. It is its OWN rule and not the
// opt-in above: the orchestrator is where everything filters back, so it parks
// only when literally nothing else is running. Nothing wears it until the
// operator or the orchestrator puts it on a card.
const OrchestratorTag = "atrium:orchestrator"

// fixtureCards is the set of cards a fixture started. A fixture is a terminal
// that comes up with the daemon, not work, so it is never parked on a clock and
// never counts as something running.
func (d *Daemon) fixtureCards() map[string]bool {
	out := map[string]bool{}
	fixtures, err := d.st.Fixtures()
	if err != nil {
		return out
	}
	for _, f := range fixtures {
		if f.TaskID != "" {
			out[f.TaskID] = true
		}
	}
	return out
}

// idleParkSubject reports whether a card is one the idle clock may park:
// an agent's card, or one of the operator's that opted in, or the orchestrator.
// Never a fixture, a throwaway, or a card with a lent session in use. A shell is
// not a card's runner (it lives in the supervisor's other map), so it never gets
// here.
func (d *Daemon) idleParkSubject(t *store.Task) bool {
	if t == nil || t.Throwaway || d.fixtureCards()[t.ID] || d.guests.get(t.ID) != nil {
		return false
	}
	return hasTag(t.Tags, OriginAgentTag) || hasTag(t.Tags, ParkIdleTag) || hasTag(t.Tags, OrchestratorTag)
}

// othersLive reports whether any card other than `except` has a live runner.
// Parked cards have no runner and so do not count, and neither do fixtures: they
// are terminals and not work. A runner whose card cannot be read DOES count,
// since "nothing else is running" is the safe side to be wrong on.
func (d *Daemon) othersLive(except string) bool {
	fixtures := d.fixtureCards()
	d.sup.mu.Lock()
	ids := make([]string, 0, len(d.sup.runners))
	for id := range d.sup.runners {
		ids = append(ids, id)
	}
	d.sup.mu.Unlock()
	for _, id := range ids {
		if id == except || fixtures[id] {
			continue
		}
		if t, err := d.st.Get(id); err == nil && isParked(t) {
			continue
		}
		return true
	}
	return false
}

// orchestratorMayPark is the orchestrator's extra condition: with the tag on, it
// is free to park only when no other card has a live runner. A card without the
// tag has no such condition.
func (d *Daemon) orchestratorMayPark(t *store.Task) bool {
	if t == nil || !hasTag(t.Tags, OrchestratorTag) {
		return true
	}
	return !d.othersLive(t.ID)
}

// mayIdlePark is the whole "who" half of the rule: the card is subject, and if it
// is the orchestrator nothing else is running.
func (d *Daemon) mayIdlePark(t *store.Task) bool {
	return d.idleParkSubject(t) && d.orchestratorMayPark(t)
}
