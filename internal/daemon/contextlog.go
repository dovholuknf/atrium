package daemon

import (
	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"log"
	"strings"
	"sync"

	"github.com/dovholuknf/atrium/internal/store"
)

// What reached a card in its CURRENT turn, so a new-context cycle can give it back.
//
// A say that lands in the turn the cycle then cuts into (typed at the line, or carried by the hook that opened the
// turn) is in the model's context, and the model is then told to write its handoff and stop. Nothing makes it act on
// the say, and /clear wipes it. The sender was told `queued` and later `delivered`, and the say never happened.
//
// So the daemon remembers, per card, what it delivered and in which turn, and a cycle that begins takes the ones
// delivered in the latest turn. When the cycle ends with the wake typed they are queued again, once each, behind it.
// A say the model already acted on arrives a second time, labelled as a repeat. That is the cheaper mistake.
//
// IN MEMORY, like the activity it reads: a restart ends the cycle too. See newcontext_journal.go.

// deliveredCap is how many deliveries a card remembers. Older ones are in turns long over.
const deliveredCap = 32

// carriedNote leads a say handed back after the wake.
const carriedNote = "[repeated: this reached you just before your context was cleared, and your handoff may not " +
	"mention it. Act on it if you have not already.]\n"

type deliveredSay = shared.DeliveredSay

type contextLog struct {
	mu sync.Mutex
	by map[string][]deliveredSay
}

func newContextLog() *contextLog { return &contextLog{by: map[string][]deliveredSay{}} }

// noteInContext records a delivery to a card's session by whatever route.
func (d *Daemon) noteInContext(taskID, from, text, kind string) {
	if d.inCtx == nil || strings.TrimSpace(text) == "" {
		return
	}
	// A delivery between turns starts the next one. Mid-turn it joins the one going.
	turn := d.act.turnsBegun(taskID)
	if !d.act.midTurn(taskID) {
		turn++
	}
	d.inCtx.mu.Lock()
	defer d.inCtx.mu.Unlock()
	l := append(d.inCtx.by[taskID], deliveredSay{From: from, Text: text, Kind: kind, Turn: turn})
	if len(l) > deliveredCap {
		l = l[len(l)-deliveredCap:]
	}
	d.inCtx.by[taskID] = l
}

// takeInContext returns, and forgets, what was delivered in the card's latest turn or after it.
func (d *Daemon) takeInContext(taskID string) []deliveredSay {
	if d.inCtx == nil {
		return nil
	}
	latest := d.act.turnsBegun(taskID)
	d.inCtx.mu.Lock()
	defer d.inCtx.mu.Unlock()
	var out []deliveredSay
	for _, s := range d.inCtx.by[taskID] {
		if s.Turn >= latest {
			out = append(out, s)
		}
	}
	delete(d.inCtx.by, taskID)
	return out
}

// requeueCarried queues again what a cycle took, to be delivered by the ordinary paths now the wake is typed.
func (d *Daemon) requeueCarried(taskID string, carried []deliveredSay) {
	for _, s := range carried {
		text := s.Text
		if !strings.HasPrefix(text, carriedNote) && !strings.HasPrefix(text, inCaseYouMissedIt) {
			text = carriedNote + text
		}
		// A repeat is never a new ask: it owes no report, whoever sent it. The launcher's
		// original say opened any debt there is, and that stands or was settled already.
		// Recorded as a launcher's fyi is, so no nudge and no silent-stop notice follow.
		var m *store.Message
		var err error
		if s.From != "" {
			m, err = d.st.QueuePeerKind(taskID, text, s.From, store.PromptFYI, true)
		} else {
			m, err = d.st.QueueAfterTurn(taskID, text, "")
		}
		if err != nil {
			log.Printf("[atrium] could not hand back a say to %s after its new context: %v", taskID, err)
			continue
		}
		d.deferPeerInjection(taskID, m.ID, s.From, text, store.PromptFYI, true)
	}
}
