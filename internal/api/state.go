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
// DECLINES TO PERSIST. That is not a new rule. `docs/runtime/activity-design.md` says
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
// ── plus the seen row, which is persisted too ───────────
//
// Each card goes out as its stored row with one key added, `seen`: the card's
// row in the seen table, shaped by `Seen.View` exactly as the board's rows carry
// it. That is stored state as much as the card is, and `Unseen` and the open
// questions are worked out from its stored timestamps alone, so nothing true
// only this second rides along. The hub's notifier needs it: without it, "a
// question is waiting" and "a turn finished that you have not seen" can never
// be told from the cache (f-023).
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
	// The seen rows are a decoration: a room whose seen table will not read
	// still says what it holds, as withSeen does for the board.
	seen, _ := s.st.SeenAll()
	// NONE IS AN ANSWER AND MUST LOOK LIKE ONE, which is why this is made and
	// never left nil. A nil slice marshals to `null`, which is
	// indistinguishable from a body that never mentioned cards at all, and
	// those mean opposite things to whoever is reading this: one is "the board
	// here is clear" and the other is "I could not tell you". The hub takes an
	// announcement whole, so the first empties its cache and the second must
	// not.
	cards := make([]stateCard, 0, len(tasks))
	for _, t := range tasks {
		cards = append(cards, stateCard{Task: t, Seen: seen[t.ID].View()})
	}
	// The stored rows, marshalled as themselves, each with its seen row.
	writeJSON(w, http.StatusOK, map[string]any{"cards": cards})
}

// stateCard is a stored card with its stored seen row. The embedded row
// marshals as its own fields, so the payload is the card as stored plus one
// key.
type stateCard struct {
	*store.Task
	Seen *store.SeenView `json:"seen,omitempty"`
}
