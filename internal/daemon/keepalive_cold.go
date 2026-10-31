package daemon

import (
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"github.com/dovholuknf/atrium/internal/small"
	"github.com/dovholuknf/atrium/internal/store"
)

// THE COLD HANDOFF. When atrium is not going to warm an idle card's cache again, the cache goes cold an hour
// after its last reply and the next turn rewrites the whole context. Just before that, while the cache is still
// warm, the card is handed the ordinary new-context cycle: the handoff is written on the warm cache and the next
// turn starts small. No second handoff path: the cycle is newcontext.go's, started as a button press starts it.
//
// Keep-alive owns the moment, because it already knows each card's expiry: a card it will refresh is left alone,
// and one it will not (its break-even stop, a stop of any other kind, a switch that is off, a model or cache it
// does not cover, a suspended room) is handed off inside the margin before expiry.

// coldHandoffBy is the `by` on the event a cold handoff leaves on the card.
const coldHandoffBy = "cache-cold"

// coldHandoffReason is the reason written on that event.
const coldHandoffReason = "cache going cold"

// coldHandoffOff reports whether a card's own override switches the cold handoff off.
func coldHandoffOff(t *store.Task) bool {
	return strings.EqualFold(strings.TrimSpace(t.Overrides[api.OverrideColdHandoff]), "off")
}

// coldCheck starts the new-context cycle on a card whose cache atrium is no longer warming and which is about to
// go cold. warming says keep-alive will refresh it, or has just held a refresh back for a reason that passes.
func (k *keepalive) coldCheck(t *store.Task, card *store.KeepaliveCard, warming bool) {
	if warming || k.cold == nil || card == nil || !k.st.ColdHandoffOn() || coldHandoffOff(t) {
		return
	}
	if k.holding != nil && k.holding(t.ID) {
		return
	}
	if k.deployHeld != nil && k.deployHeld(t.ID) {
		return
	}
	if h, err := k.st.Harness(t.Runner); err != nil || !shared.IsClaude(h) {
		return
	}
	if small.IsParked(t) || strings.TrimSpace(t.ResumeID) == "" || strings.TrimSpace(t.Worktree) == "" {
		return
	}
	if t.Status != store.StatusNeedsInput {
		return
	}
	if pend, _ := k.st.PendingForTask(t.ID); len(pend) > 0 {
		return
	}
	path := k.transcript(t.Worktree, t.ResumeID)
	if path == "" {
		return
	}
	r, err := k.replies.readLastReply(path)
	if err != nil || r.TTL != time.Hour || r.Context < keepaliveMinContext {
		return
	}
	_, _, _, _, _, expiry, _ := k.stretchState(t.ID, card, r)
	now := k.now()
	if now.Before(refreshDueAt(expiry)) || !now.Before(expiry) {
		return
	}
	if k.coldDone(t.ID, r.At) || k.coldSpent(t.ID) {
		return
	}
	err = k.cold(t)
	if errors.Is(err, errNewContextBusy) || isSharedErr(err) {
		return
	}
	k.mu.Lock()
	k.cooled[t.ID] = r.At
	k.mu.Unlock()
	if err == nil {
		if serr := k.st.SetSetting(coldSpentKey(t.ID), k.now().Format(time.RFC3339Nano)); serr != nil {
			log.Printf("[atrium] could not record the cold handoff as spent on %s: %v", t.ID, serr)
		}
	}
	ev := map[string]any{
		"by": coldHandoffBy, "reason": coldHandoffReason, "started": err == nil,
		"reply_at": r.At.Format(time.RFC3339Nano), "expires_at": expiry.Format(time.RFC3339Nano),
		"context": r.Context,
	}
	if err != nil {
		ev["error"] = err.Error()
		log.Printf("[atrium] cold handoff not started on %s: %v", t.DisplayTitle(), err)
	} else {
		log.Printf("[atrium] cold handoff started on %s: cache expires %s", t.DisplayTitle(), expiry.Local().Format("15:04"))
	}
	if err := k.st.AppendEvent(t.ID, store.EventNotified, ev); err != nil {
		log.Printf("[atrium] could not record the cold handoff on %s: %v", t.ID, err)
	}
	k.broadcast("keepalive", map[string]any{"task_id": t.ID, "title": t.DisplayTitle(), "cold_handoff": err == nil})
}

// coldSpentKey is the setting that holds a card's cold handoff as used. One handoff per human turn: only a turn the
// operator or a peer started puts it back, never atrium's own continuation after the clear, nor a keep-alive ping.
func coldSpentKey(taskID string) string { return "cold_handoff_spent:" + taskID }

// coldSpent says the card was cold-handed-off and no turn of a person's has come since. A read failure answers
// spent, so a store that cannot be read cannot start a loop.
func (k *keepalive) coldSpent(taskID string) bool {
	v, err := k.st.Setting(coldSpentKey(taskID))
	return err != nil || v != ""
}

// coldSpentAt is when the card was cold-handed-off, while no turn of a person's has come since.
func (k *keepalive) coldSpentAt(taskID string) (time.Time, bool) {
	v, err := k.st.Setting(coldSpentKey(taskID))
	if err != nil || v == "" {
		return time.Time{}, false
	}
	at, err := time.Parse(time.RFC3339Nano, v)
	return at, err == nil
}

// whyColdHandedOff is the skip reason of a card handed off cold: its cache is let go until a person's turn.
const whyColdHandedOff = "handed off cold"

// coldRearm lets the card be handed off again, on a turn a person or a peer started.
func (k *keepalive) coldRearm(taskID string) {
	if k == nil {
		return
	}
	if v, err := k.st.Setting(coldSpentKey(taskID)); err != nil || v == "" {
		return
	}
	if err := k.st.SetSetting(coldSpentKey(taskID), ""); err != nil {
		log.Printf("[atrium] could not re-arm the cold handoff on %s: %v", taskID, err)
	}
}

// coldDone says a handoff was already made for the reply the card's last turn ended on. A new turn moves the
// reply on, which is what lets the next cold event hand off again. The card's events say it too, so a restart of
// the room does not forget.
func (k *keepalive) coldDone(taskID string, replyAt time.Time) bool {
	k.mu.Lock()
	at, ok := k.cooled[taskID]
	k.mu.Unlock()
	if ok && !replyAt.After(at) {
		return true
	}
	evs, err := k.st.Events(taskID, 50)
	if err != nil {
		return false
	}
	for _, e := range evs {
		if e.Kind != store.EventNotified {
			continue
		}
		var p struct {
			By      string `json:"by"`
			ReplyAt string `json:"reply_at"`
		}
		if json.Unmarshal(e.Payload, &p) != nil || p.By != coldHandoffBy {
			continue
		}
		if at, err := time.Parse(time.RFC3339Nano, p.ReplyAt); err == nil && !replyAt.After(at) {
			return true
		}
	}
	return false
}

// coldHandoff is the daemon's side of the seam: the cycle, with the guards a lent or moving card needs.
func (d *Daemon) coldHandoff(t *store.Task) error {
	if d.guests.get(t.ID) != nil || d.frozenForMove(t.ID) || d.nctx.get(t.ID) != nil {
		return errNewContextBusy
	}
	return d.StartNewContext(t.ID)
}
