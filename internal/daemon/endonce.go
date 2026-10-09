package daemon

import (
	"net/http"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// ONE MESSAGE PER END EVENT. A worker that ends with atrium_done or atrium_blocked has told its launcher, and a say
// of the same meaning in the same turn would tell it twice. Whichever comes second is not delivered:
//
//   - a say, then the end tool: the end tool is still recorded, and its notice is not sent (endEchoesSay).
//   - the end tool, then a say: the say is dropped with a note saying why (endSayDropped).
//
// "The same turn" is since the card's last turn end. A card with no turn end yet gets sameTurnFallback.
const sameTurnFallback = 10 * time.Minute

type endMark struct {
	status string
	at     time.Time
}

// markEnded notes that a card ended with atrium_done or atrium_blocked just now.
func (d *Daemon) markEnded(taskID, status string) {
	d.endedMarks.Store(taskID, endMark{status: status, at: time.Now()})
}

// sinceTurn is the moment "this turn" began for a card.
func (d *Daemon) sinceTurn(taskID string) time.Time {
	if at, err := d.st.TurnEndedAt(taskID); err == nil && at != nil {
		return *at
	}
	return time.Now().Add(-sameTurnFallback)
}

// endEchoesSay says whether a launched worker already said its launcher the same end, done or blocked, this turn.
func (d *Daemon) endEchoesSay(task *store.Task, status string) bool {
	if task == nil || !agentLaunched(task) {
		return false
	}
	since := d.sinceTurn(task.ID)
	says, err := d.st.SaysFor(task.ID, 10)
	if err != nil {
		return false
	}
	for _, s := range says {
		if s.FromTask != task.ID || store.SayEndStatus(s.Preview) != status {
			continue
		}
		if at, err := time.Parse(store.TimeFormat, s.SentAt); err == nil && at.After(since) {
			return true
		}
	}
	return false
}

// endSayDropped says whether a say is a launched worker repeating an end it already made with atrium_done or
// atrium_blocked this turn.
func (d *Daemon) endSayDropped(from, text string) bool {
	status := store.SayEndStatus(text)
	if status == "" {
		return false
	}
	sender, err := d.st.GetByWireName(d.st.Qualify(strings.TrimSpace(from)))
	if err != nil || !agentLaunched(sender) {
		return false
	}
	v, ok := d.endedMarks.Load(sender.ID)
	if !ok {
		return false
	}
	m := v.(endMark)
	return m.status == status && m.at.After(d.sinceTurn(sender.ID))
}

// writeEndSayDropped answers a say that endSayDropped refused.
func writeEndSayDropped(w http.ResponseWriter) {
	writeJSONCode(w, http.StatusOK, map[string]any{
		"ok": true, "delivered": "dropped", "typed": false, "queued": false,
		"note": "not sent: you already ended with atrium_done or atrium_blocked, so your launcher has it. " +
			"Nothing more is needed. Stop.",
	})
}
