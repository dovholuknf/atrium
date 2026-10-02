package api

import (
	"fmt"
	"strings"

	"github.com/dovholuknf/atrium/internal/store"
)

// The tags that make a card the operator's to end. Spelled here because
// internal/daemon owns the constants and imports this package.
const (
	exitDirectorTag = "atrium:director"
	exitCeilingTag  = "atrium:context-ceiling"
)

// exitAsk is the body of `POST /v1/tasks/{id}/exit`.
//
// From is the card asking, as a handle, alias or id on THIS room, and it is
// what makes the caller an agent. Empty is the operator: the board's button, a
// person at a shell. Foreign says From is a name on another room, which is
// never this room's card of the same name.
type exitAsk struct {
	From    string `json:"from"`
	Foreign bool   `json:"foreign"`
	Force   bool   `json:"force"`
	Why     string `json:"why"`
}

// guardExit decides whether `from` may ask `target` to leave. Nil is yes. The
// second result says the exit goes ahead on a force, and should be recorded.
//
//   - No From: the operator. Anything, directors included.
//   - A director (atrium:director or atrium:context-ceiling): never an agent's
//     to end, force or not. Only the operator.
//   - The caller itself, or a card the caller launched: yes.
//   - Any other card: refused unless Force, which is recorded on the card.
//
// THE CALLER IS WHO IT SAYS IT IS. The route is as open as the rest of the
// board, so this stops the mistake and not the attacker: a worker handing an
// id it was given back to atrium_exit, which is how a director once went done.
func (s *Server) guardExit(target *store.Task, in exitAsk) (forced bool, err error) {
	from := strings.TrimSpace(in.From)
	if from == "" {
		return false, nil
	}
	if hasExitTag(target.Tags, exitDirectorTag) || hasExitTag(target.Tags, exitCeilingTag) {
		return false, fmt.Errorf("%s is a director. an agent cannot exit one, force or not: only the operator can", target.DisplayTitle())
	}
	if !in.Foreign {
		if me := s.callerCard(from); me != nil {
			if me.ID == target.ID {
				return false, nil
			}
			if target.SpawnedByID == me.ID || (target.SpawnedByID == "" && target.SpawnedBy != "" &&
				strings.EqualFold(target.SpawnedBy, me.WireName)) {
				return false, nil
			}
		}
	}
	if in.Force {
		return true, nil
	}
	return false, fmt.Errorf("%s is not you or a card you launched, so it was not asked to exit. "+
		"to exit yourself, give no card. a card you do not own needs force, which is recorded on it", target.DisplayTitle())
}

// callerCard finds the card a sender name means on this room: handle, alias,
// then id. Nil when it names nothing here.
func (s *Server) callerCard(name string) *store.Task {
	if t, err := s.st.GetByWireName(name); err == nil {
		return t
	}
	if t, err := s.st.GetByAlias(name); err == nil {
		return t
	}
	if t, err := s.st.Get(name); err == nil {
		return t
	}
	return nil
}

func hasExitTag(tags []string, want string) bool {
	for _, t := range tags {
		if strings.EqualFold(strings.TrimSpace(t), want) {
			return true
		}
	}
	return false
}
