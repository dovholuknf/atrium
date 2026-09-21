package api

import (
	"net/http"

	"github.com/dovholuknf/atrium/internal/store"
)

// What this room is holding, as the hub is allowed to remember it.
//
// ── why this is not `/v1/tasks` ─────────────────────────
//
// THE HUB CACHES EXACTLY WHAT THE ROOM PERSISTS, AND NEVER WHAT THE ROOM
// DECLINES TO PERSIST. That is not a new rule. `docs/activity-design.md` says
// what a runner is doing right now is never written down, because it would be
// a lie the moment the daemon restarted, and a hub writing it down would break
// that rule at one remove.
//
// `/v1/tasks` answers with a VIEW, which is the card plus everything true only
// this second: whether it is supervised, whether it has a shell, what tool it
// is running, its telemetry, how long it has been idle. All of that is correct
// for a board drawing a live room and all of it is wrong in a cache.
//
// The obvious answer is to send the view and strip the live fields, and the
// obvious answer is wrong: it is a field list, kept in one place and read in
// another, and the day somebody adds a live field to the view is the day the
// hub starts remembering it. This endpoint answers with the stored row itself,
// so the two halves cannot drift. What is persisted is what is sent, by
// construction rather than by maintenance.
//
// ── and why it is not the board's business ──────────────
//
// Nothing in the board calls this. It exists for the link, which asks its own
// room through the same handler it serves to the hub, so there is no second
// path to the data and no second set of rules about what may leave.

// roomState answers with every card this room holds, as stored.
func (s *Server) roomState(w http.ResponseWriter, r *http.Request) {
	// ARCHIVED CARDS ARE NOT HERE, the same as everywhere else that asks what
	// is on the board. An archived card is a row somebody put away, and a hub
	// showing a shut room's board should show what that board showed.
	tasks, err := s.st.List()
	if err != nil {
		s.fail(w, err)
		return
	}
	// NONE IS AN ANSWER AND MUST LOOK LIKE ONE. A nil slice marshals to `null`,
	// which is indistinguishable from a body that never mentioned cards at all,
	// and those mean opposite things to whoever is reading this: one is "the
	// board here is clear" and the other is "I could not tell you". The hub
	// takes an announcement whole, so the first empties its cache and the
	// second must not.
	if tasks == nil {
		tasks = []*store.Task{}
	}
	// The stored rows, marshalled as themselves. No view, no wrapper per card,
	// nothing computed.
	writeJSON(w, http.StatusOK, map[string]any{"cards": tasks})
}
