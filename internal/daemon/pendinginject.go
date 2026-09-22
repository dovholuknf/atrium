package daemon

import (
	"log"
	"sync"
	"time"
)

// A peer message that could not be typed in right away, kept trying.
//
// The gate (see injectPeer) types a peer message into a terminal only when the
// operator's line is empty and the keyboard has been quiet. When it is not, the
// message is queued for the ordinary hook delivery AND handed here, so atrium
// keeps trying to put it on screen the way clint reads his sessions rather than
// only ever behind a tool call.
//
// NEVER LOST AND NEVER DROPPED. The durable copy is the message row in the
// store, which the permission and Stop hooks drain whatever this does. This
// retries the ON-SCREEN delivery on a widening backoff, so a doer's words land
// in the terminal the moment the operator's line clears, and until then they
// wait rather than tangling into what he is typing.
//
// THE BACKOFF IS THE RATE LIMITER. Each time the gate is still shut, one
// warning goes to the board and the next retry is scheduled further out. The
// intervals grow, so the warnings thin out on their own without any separate
// throttle. A keystroke on that terminal means the operator is back, so the
// backoff resets to the front and the message gets an early retry again: the
// long intervals only accrue during genuine absence.
//
// IN MEMORY, like the activity it drives. The retry schedule dies with the
// daemon, and that is right: the message itself is durable in the store and the
// hooks still deliver it, so a restart costs the on-screen retry and nothing
// that was said. The board signal it raises is a fact about now, which
// docs/activity-design.md says is never written down.

// backoffSteps is how long to wait before each retry, widening so the warnings
// that ride them thin out. Past the last it holds at four hours, so a message is
// never abandoned, only asked about less and less often.
//
// THE FRONT IS SECONDS, NOT MINUTES. The gate opens after `peerGateIdle` of
// quiet on an empty line, so a first retry a minute out left a message sitting
// for most of that minute on a terminal that was already free. A keystroke
// resets to the front, so the first retry after the operator stops typing lands
// just past the gate's own idle window.
var backoffSteps = []time.Duration{
	2 * time.Second,
	5 * time.Second,
	10 * time.Second,
	30 * time.Second,
	1 * time.Minute,
	2 * time.Minute,
	5 * time.Minute,
	10 * time.Minute,
	30 * time.Minute,
	1 * time.Hour,
	2 * time.Hour,
	4 * time.Hour,
}

// pendingMsg is one deferred peer message: the store row that is the source of
// truth, who sent it, and the bytes to type.
type pendingMsg struct {
	msgID  string
	from   string
	text   string // the clean text, for the timeline record
	banner string
	body   string // text plus any bracketed-paste markers, what actually types
}

// heldTell is everything waiting to be typed into one card's terminal, and
// where it is in the backoff.
type heldTell struct {
	entries []pendingMsg
	step    int
	timer   *time.Timer
	since   time.Time
}

// pendingInjector retries the on-screen delivery of deferred peer messages.
type pendingInjector struct {
	d  *Daemon
	mu sync.Mutex
	by map[string]*heldTell
}

func newPendingInjector(d *Daemon) *pendingInjector {
	return &pendingInjector{d: d, by: map[string]*heldTell{}}
}

// deferPeerInjection hands a just-queued peer message to the injector to retry
// on screen. The banner and body are rebuilt the same way tellByTyping does, so
// a retry types exactly what an immediate injection would have.
func (d *Daemon) deferPeerInjection(taskID, msgID, from, text string) {
	if d.pending == nil {
		return
	}
	// A card that refuses peer typing is never going to take a PEER'S text on
	// screen, so there is nothing to retry. The queue and the hooks still deliver
	// it. Same refusal tellByTyping makes before it tries the first time. The
	// operator's own text (empty from) is not a peer's and is always retried.
	if from != "" {
		if t, err := d.st.Get(taskID); err != nil || !t.PeerTyping {
			return
		}
	}
	body := text
	if d.bracketedPasteFor(taskID, false) {
		body = "\x1b[200~" + text + "\x1b[201~"
	}
	banner := ""
	if from != "" {
		banner = peerBanner(from)
	}
	d.pending.hold(taskID, pendingMsg{
		msgID:  msgID,
		from:   from,
		text:   text,
		banner: banner,
		body:   body,
	})
}

// hold registers a peer message that the gate would not take right now, and
// starts it retrying.
//
// A card with no runner is left to the queue and the hooks: there is no
// terminal to type into, so there is nothing to retry. The message is still
// safe in the store.
func (pi *pendingInjector) hold(taskID string, m pendingMsg) {
	run := pi.d.sup.get(taskID)
	if run == nil {
		return
	}
	pi.mu.Lock()
	ht := pi.by[taskID]
	first := ht == nil
	if first {
		ht = &heldTell{since: time.Now()}
		pi.by[taskID] = ht
	}
	ht.entries = append(ht.entries, m)
	if first {
		// A keystroke on this terminal re-arms the backoff to the front, so an
		// operator who is back gets an early retry. Cleared in drop.
		reset := func() { pi.reset(taskID) }
		run.onKey.Store(&reset)
		ht.timer = time.AfterFunc(backoffSteps[0], func() { pi.attempt(taskID) })
	}
	pi.mu.Unlock()
	// The live board signal: this card is holding a message. Named by the first
	// sender, aged from when the first message was held. See activityTracker.
	who := m.from
	if who == "" {
		who = "you"
	}
	pi.d.act.setHeld(taskID, who)
	pi.d.publishTask(taskID)
}

// attempt is one retry: reconcile against the store, try to drain what is still
// waiting into an open gate, and either finish or warn and reschedule.
func (pi *pendingInjector) attempt(taskID string) {
	run := pi.d.sup.get(taskID)
	if run == nil {
		// The terminal went away. The hooks still deliver from the queue, so the
		// message is not lost, but there is nothing here to type into.
		pi.drop(taskID)
		return
	}

	// Drop anything the hooks already delivered, so a message that reached the
	// session another way is never typed on top.
	if pending, err := pi.d.st.PendingMessages(taskID); err == nil {
		live := map[string]bool{}
		for _, msg := range pending {
			live[msg.ID] = true
		}
		pi.mu.Lock()
		if ht := pi.by[taskID]; ht != nil {
			kept := ht.entries[:0]
			for _, e := range ht.entries {
				if live[e.msgID] {
					kept = append(kept, e)
				}
			}
			ht.entries = kept
		}
		pi.mu.Unlock()
	}

	pi.mu.Lock()
	ht := pi.by[taskID]
	if ht == nil {
		pi.mu.Unlock()
		return
	}
	if len(ht.entries) == 0 {
		pi.mu.Unlock()
		pi.drop(taskID)
		return
	}
	entries := append([]pendingMsg(nil), ht.entries...)
	step := ht.step
	pi.mu.Unlock()

	// A card that has since refused peer typing gives up the retry for PEER text.
	// The queue and the hooks still deliver it. The operator's own text keeps
	// retrying.
	t, err := pi.d.st.Get(taskID)
	if err != nil {
		pi.drop(taskID)
		return
	}
	if !t.PeerTyping {
		kept := entries[:0]
		for _, e := range entries {
			if e.from == "" {
				kept = append(kept, e)
			}
		}
		entries = kept
		if len(entries) == 0 {
			pi.drop(taskID)
			return
		}
	}
	// A dialog the runner put up itself must not be answered by a peer message's
	// Enter, exactly as tellByTyping refuses one. This is not the operator's line
	// being dirty, so it is a silent wait: reschedule at the same interval and do
	// not warn about a line the operator has not touched.
	if pi.d.act.dialogOpen(taskID) {
		pi.mu.Lock()
		if ht := pi.by[taskID]; ht != nil && ht.timer != nil {
			ht.timer.Reset(backoffSteps[step])
		}
		pi.mu.Unlock()
		return
	}

	// Drain oldest first while the gate stays open. injectPeer submits each on
	// its own line, so several waiting messages arrive as several turns. The
	// gate only shuts here if the operator starts typing between two of them,
	// which leaves the rest for the next tick.
	delivered := map[string]bool{}
	for _, e := range entries {
		wrote, err := run.injectPeer(e.banner, e.body)
		if err != nil {
			log.Printf("[atrium] retrying a held message into %s failed: %v", taskID, err)
			break
		}
		if !wrote {
			break
		}
		if err := pi.d.st.MarkDelivered(taskID, "terminal", []string{e.msgID}); err != nil {
			log.Printf("[atrium] typed a held message into %s but could not mark it delivered: %v", taskID, err)
		}
		pi.d.notePeerTyped(taskID, e.from, e.text, "typed and sent after waiting for your line to clear")
		delivered[e.msgID] = true
	}

	pi.mu.Lock()
	ht = pi.by[taskID]
	if ht == nil {
		pi.mu.Unlock()
		return
	}
	if len(delivered) > 0 {
		kept := ht.entries[:0]
		for _, e := range ht.entries {
			if !delivered[e.msgID] {
				kept = append(kept, e)
			}
		}
		ht.entries = kept
	}
	if len(ht.entries) == 0 {
		pi.mu.Unlock()
		pi.drop(taskID)
		pi.d.publishTask(taskID)
		return
	}
	// Still blocked. Widen the backoff, warn once for this tick, and reschedule.
	from := ht.entries[0].from
	waited := time.Since(ht.since)
	final := ht.step >= len(backoffSteps)-1
	if !final {
		ht.step++
	}
	next := backoffSteps[ht.step]
	if ht.timer != nil {
		ht.timer.Reset(next)
	}
	pi.mu.Unlock()

	// Not in the first minute. The front of the backoff is seconds apart, and a
	// message held that briefly is the gate working, not something to announce.
	if waited >= time.Minute {
		pi.warn(taskID, from, waited, final)
	}
}

// reset re-arms the backoff to the front, because a keystroke means the
// operator is at this terminal now and the message should get an early retry
// rather than languish at a multi-hour interval.
//
// Called off the operator keystroke path (runner.onKey), so it is short and
// takes only this lock. See noteOperatorTyped.
func (pi *pendingInjector) reset(taskID string) {
	pi.mu.Lock()
	defer pi.mu.Unlock()
	ht := pi.by[taskID]
	if ht == nil || ht.timer == nil {
		return
	}
	ht.step = 0
	ht.timer.Reset(backoffSteps[0])
}

// drop forgets a card's held messages, because they all landed, were delivered
// another way, or the terminal went away. It disarms the keystroke reset and
// clears the board signal.
func (pi *pendingInjector) drop(taskID string) {
	pi.mu.Lock()
	ht := pi.by[taskID]
	if ht != nil {
		if ht.timer != nil {
			ht.timer.Stop()
		}
		delete(pi.by, taskID)
	}
	pi.mu.Unlock()
	if run := pi.d.sup.get(taskID); run != nil {
		run.onKey.Store(nil)
	}
	pi.d.act.clearHeld(taskID)
	pi.d.publishTask(taskID)
}

// deliveredElsewhere reconciles a card's held set after the hooks drained its
// queue another way, so the board signal clears the moment the last pending
// message is gone.
//
// WITHOUT THIS THE CHIP LATCHES ON. The permission and Stop hooks empty the
// durable queue and mark the rows delivered, but the injector only learns of it
// on its next backoff `attempt`, and that interval widens out to a day. So the
// held badge stayed lit for hours after the message had already landed. Told
// the ids just marked delivered, this forgets them and drops the card when
// nothing is left to type, on the delivery itself rather than on the next tick.
func (pi *pendingInjector) deliveredElsewhere(taskID string, ids []string) {
	gone := make(map[string]bool, len(ids))
	for _, id := range ids {
		gone[id] = true
	}
	pi.mu.Lock()
	ht := pi.by[taskID]
	if ht == nil {
		pi.mu.Unlock()
		return
	}
	kept := ht.entries[:0]
	for _, e := range ht.entries {
		if !gone[e.msgID] {
			kept = append(kept, e)
		}
	}
	ht.entries = kept
	empty := len(ht.entries) == 0
	pi.mu.Unlock()
	if empty {
		pi.drop(taskID)
	}
}

// stopAll halts every backoff timer, for shutdown. The held messages stay in
// the store and the next daemon's hooks deliver them, so nothing is lost by
// giving up the on-screen retry here.
func (pi *pendingInjector) stopAll() {
	pi.mu.Lock()
	defer pi.mu.Unlock()
	for id, ht := range pi.by {
		if ht.timer != nil {
			ht.timer.Stop()
		}
		delete(pi.by, id)
	}
}

// warn tells the board a held message is still waiting and why, once per tick.
// The board turns it into an alert. The growing backoff is what keeps this from
// repeating too often, so there is no separate throttle here.
func (pi *pendingInjector) warn(taskID, from string, waited time.Duration, final bool) {
	pi.d.ap.Broadcast("peer-waiting", map[string]any{
		"task_id": taskID,
		"from":    from,
		"seconds": int64(waited.Seconds()),
		"final":   final,
	})
	// Refresh the held badge so its age advances even between board polls.
	pi.d.publishTask(taskID)
}
