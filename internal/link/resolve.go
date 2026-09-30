package link

import (
	"fmt"
	"sort"
	"strings"
)

// Turning a name somebody typed into a card. See
// docs/rnd/handle-addressed-http-design.md.
//
// ONE RULE, TWO CALLERS. The control tools (resolvePeer) and the hub's HTTP
// routing (placeCard) both find a card by the same order, so a name that works
// in atrium_say works in curl, and one that fails fails the same way:
//
//  1. a wire name or an id, exactly
//  2. an alias, case and a leading `@` ignored: live before done, then newest.
//     A dead card keeps its alias as a record and no longer answers.
//
// ACROSS ROOMS NOTHING KEEPS TWO ALIASES APART, so one live match wins, no live
// match and one done match wins, and anything else is ambiguous. A script is
// told both rather than reaching whichever room answered first.

// candidate is one card that answers to a name, and its room.
type candidate struct {
	Room string
	Card ctlCard
}

// errNoCard is a name nothing answered to, with what would have worked.
type errNoCard struct {
	who       string
	rooms     []string
	quiet     []string
	wouldWork []string
}

func (e *errNoCard) Error() string {
	msg := fmt.Sprintf("no card called %q", e.who)
	if len(e.rooms) > 0 {
		msg += " on room " + strings.Join(e.rooms, ", room ")
	}
	if len(e.quiet) > 0 {
		msg += ". not answering: " + strings.Join(e.quiet, ", ")
	}
	if len(e.wouldWork) == 0 {
		return msg + ", and nothing else is running either"
	}
	return msg + ". these would have worked: " + strings.Join(e.wouldWork, ", ")
}

// errAmbiguous is a name more than one card answers to.
type errAmbiguous struct {
	who        string
	candidates []string
}

func (e *errAmbiguous) Error() string {
	return fmt.Sprintf("%q is on more than one room. name the room: %s", e.who, strings.Join(e.candidates, ", "))
}

func cardLive(status string) bool { return status != "done" && status != "dead" }

// matchCard finds `who` in one room's list.
func matchCard(cards []ctlCard, who string) (ctlCard, bool) {
	who = strings.TrimSpace(who)
	if who == "" {
		return ctlCard{}, false
	}
	for _, t := range cards {
		if t.Wire == who || t.ID == who {
			return t, true
		}
	}
	// A wire name without its atrium's prefix, `rnd-director` for
	// `sparta/rnd-director`, the way the room itself qualifies a bare one.
	for _, t := range cards {
		if i := strings.LastIndex(t.Wire, "/"); i >= 0 && t.Wire[i+1:] == who {
			return t, true
		}
	}
	a := strings.ToLower(strings.TrimPrefix(who, "@"))
	if a == "" {
		return ctlCard{}, false
	}
	var best *ctlCard
	for i := range cards {
		t := &cards[i]
		if t.Alias != a || t.Status == "dead" {
			continue
		}
		if best == nil || aliasBeats(t.Status, t.Created, best.Status, best.Created) {
			best = t
		}
	}
	if best == nil {
		return ctlCard{}, false
	}
	return *best, true
}

// wouldWork is every live handle in a list, `wire (@alias)`, with `@room` after
// the wire name when room is given.
func wouldWork(room string, cards []ctlCard) []string {
	var out []string
	for _, t := range cards {
		if !cardLive(t.Status) {
			continue
		}
		// A card with no session has no wire name. Its alias still reaches it,
		// and one with neither is nobody a caller could have meant.
		name, alias := t.Wire, t.Alias
		if name == "" {
			name, alias = alias, ""
		}
		if name == "" {
			continue
		}
		if room != "" {
			name += "@" + room
		}
		if alias != "" {
			name += " (@" + alias + ")"
		}
		out = append(out, name)
	}
	return out
}

// spelled is a candidate as a caller should retype it: `alias@room` when it has
// an alias, the wire name otherwise, then its tagged id.
func (c candidate) spelled() string {
	name := c.Card.Wire
	if c.Card.Alias != "" {
		name = c.Card.Alias
	}
	return name + "@" + c.Room + " (" + tagFor(c.Room, c.Card.ID) + ")"
}

// resolveAcross finds `who` on every room in `lists`. `quiet` are rooms that did
// not answer, named in the error when nothing matched.
func resolveAcross(lists map[string][]ctlCard, quiet []string, who string) (candidate, error) {
	rooms := make([]string, 0, len(lists))
	for room := range lists {
		rooms = append(rooms, room)
	}
	sort.Strings(rooms)
	var live, done []candidate
	for _, room := range rooms {
		c, ok := matchCard(lists[room], who)
		if !ok {
			continue
		}
		if cardLive(c.Status) {
			live = append(live, candidate{Room: room, Card: c})
		} else {
			done = append(done, candidate{Room: room, Card: c})
		}
	}
	pick := live
	if len(live) == 0 {
		pick = done
	}
	switch len(pick) {
	case 1:
		return pick[0], nil
	case 0:
		e := &errNoCard{who: who, rooms: rooms, quiet: quiet}
		for _, room := range rooms {
			tag := room
			if len(rooms)+len(quiet) < 2 {
				tag = ""
			}
			e.wouldWork = append(e.wouldWork, wouldWork(tag, lists[room])...)
		}
		return candidate{}, e
	}
	e := &errAmbiguous{who: who}
	for _, c := range pick {
		e.candidates = append(e.candidates, c.spelled())
	}
	return candidate{}, e
}
