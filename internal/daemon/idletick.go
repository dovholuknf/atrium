package daemon

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// The idle clock: a card that has sat idle for `idle_park_after` is parked, and a
// director writes its handoff first. See docs/rnd/keepalive-policy-design.md section
// 7. Who is subject is idlepark.go. This is the tick and what it does.

// idleHandoffAfter is how long a card is idle before its handoff is taken, at
// the earliest: before the one hour cache goes cold, so the capture turn reads a
// warm cache. The later of this and `idle_park_after` minus idleHandoffLead.
const (
	idleHandoffAfter = 50 * time.Minute
	idleHandoffLead  = 70 * time.Minute
	// idleClockSlack is how far after a handoff's end an event may land and still
	// be the handoff's own: the Stop hook and the store write trail the turn.
	idleClockSlack = 10 * time.Second
	// idleParkGrace is how long a runner asked to leave is given, and idleGoneWait
	// how long the park waits to see it gone before it marks the card.
	idleParkGrace = windDownGrace
)

// idleGoneWait is how long the park waits to see the runner gone before it marks
// the card, and idleLeave is how the runner is asked to go. Variables so a test
// need not wait out a wind-down.
var (
	idleGoneWait = 5 * time.Second
	idleLeave    = func(d *Daemon, r *runner, id string) { windDown(r, idleParkGrace, d.exitKeysFor(id)) }
)

// handoffMark is one card's handoff, and the clock it must not move.
type handoffMark struct {
	// base is idle-since as it stood when the capture began. Everything the
	// capture itself did (its prompt, its turn end) is at or before end, and
	// reads as base.
	base       time.Time
	capturing  bool
	end        time.Time
	file       string
	written    bool
	timedOut   bool
	timeoutWhy string
	// prev is the mark an automatic new context found when it took this one, put
	// back if the cycle fails.
	prev *handoffMark
}

// idleParks is the tick's memory, in memory on purpose: it describes an idle
// stretch of a running process. A restart loses it and costs one more handoff.
type idleParks struct {
	mu sync.Mutex
	by map[string]*handoffMark
	// parking is the cards whose wind-down is under way.
	parking sync.Map
}

func (p *idleParks) get(id string) *handoffMark {
	p.mu.Lock()
	defer p.mu.Unlock()
	if m := p.by[id]; m != nil {
		cp := *m
		return &cp
	}
	return nil
}

func (p *idleParks) put(id string, m *handoffMark) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.by == nil {
		p.by = map[string]*handoffMark{}
	}
	p.by[id] = m
}

func (p *idleParks) drop(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.by, id)
}

// takesHandoff is whether a card writes its handoff before it is parked: a
// director, the orchestrator, or a card opted in. A subagent does not: its brief
// and its branch are its state, and it has reported.
func takesHandoff(t *store.Task) bool {
	return hasTag(t.Tags, DirectorTag) || hasTag(t.Tags, ParkIdleTag) || hasTag(t.Tags, OrchestratorTag)
}

// rawIdleSince is the latest of the last turn end, the last prompt and the last
// human touch. Never earlier than the card's creation.
func (d *Daemon) rawIdleSince(t *store.Task) time.Time {
	since := t.CreatedAt
	if at, err := d.st.TurnEndedAt(t.ID); err == nil && at != nil && at.After(since) {
		since = *at
	}
	if t.PromptedAt != nil && t.PromptedAt.After(since) {
		since = *t.PromptedAt
	}
	if t.HumanAt != nil && t.HumanAt.After(since) {
		since = *t.HumanAt
	}
	return since
}

// idleSince is rawIdleSince with the handoff turn taken out: what the capture
// did is not activity, or the handoff would reset the clock it exists for. The
// mark is dropped once something real lands after it, which restarts the cycle.
func (d *Daemon) idleSince(t *store.Task) time.Time {
	raw := d.rawIdleSince(t)
	m := d.idle.get(t.ID)
	if m == nil {
		return raw
	}
	if m.capturing || !raw.After(m.end.Add(idleClockSlack)) {
		return m.base
	}
	d.idle.drop(t.ID)
	return raw
}

// idleParkEligible is the card's part of the rule: everything but the clock.
func (d *Daemon) idleParkEligible(t *store.Task, wakes map[string]bool) bool {
	if isParked(t) || !d.mayIdlePark(t) {
		return false
	}
	if t.Status != store.StatusNeedsInput && t.Status != store.StatusDone {
		return false
	}
	id := t.ID
	if d.holdingMessages(id) || d.deployHeld(id) || d.act.midTurn(id) || d.act.dialogOpen(id) || d.act.onSubagents(id) {
		return false
	}
	// Work the last Stop said is still running: shells and the like. Subagents
	// are onSubagents above. A session waiting on either is not idle.
	if n, _ := d.act.backgroundWork(id); n > 0 {
		return false
	}
	if perms, err := d.st.PendingForTask(id); err != nil || len(perms) > 0 {
		return false
	}
	if s, err := d.st.GetSeen(id); err == nil && s.Asked() && !s.Answered() {
		return false
	}
	if msgs, err := d.st.PendingMessages(id); err != nil || len(msgs) > 0 {
		return false
	}
	if wakes[id] || d.hasOutstandingLiveWorker(id) {
		return false
	}
	return true
}

// hasOutstandingLiveWorker is a worker with a live runner. A PARKED worker does
// not hold its launcher up here, though it does for the silent-stop notice: it
// has stopped, so the launcher has nothing to wait beside. Workers park first.
func (d *Daemon) hasOutstandingLiveWorker(launcherID string) bool {
	ids, err := d.st.WorkerIDs(launcherID)
	if err != nil {
		return true
	}
	for _, id := range ids {
		if d.sup.get(id) != nil {
			return true
		}
	}
	return false
}

// idleHandoffAt is how long a card is idle before its handoff is taken.
func idleHandoffAt(after time.Duration) time.Duration {
	at := after - idleHandoffLead
	if at < idleHandoffAfter {
		at = idleHandoffAfter
	}
	return at
}

// parkIdle is the reaper's tick for idle parking. It never blocks: a handoff and
// a wind-down each run on their own goroutine, and a card either is or is not
// eligible at every tick, so a tick lost costs nothing.
func (d *Daemon) parkIdle(now time.Time) {
	after, on := d.st.IdleParkAfter()
	if !on {
		return
	}
	wakes := map[string]bool{}
	if ws, err := d.st.RestartWakes(); err == nil {
		for _, w := range ws {
			wakes[w.TaskID] = true
		}
	}
	d.sup.mu.Lock()
	ids := make([]string, 0, len(d.sup.runners))
	for id := range d.sup.runners {
		ids = append(ids, id)
	}
	d.sup.mu.Unlock()

	for _, id := range ids {
		t, err := d.st.Get(id)
		if err != nil || !d.idleParkEligible(t, wakes) {
			continue
		}
		if m := d.idle.get(id); m != nil && m.capturing {
			continue
		}
		idle := now.Sub(d.idleSince(t))
		needs := takesHandoff(t)
		m := d.idle.get(id)
		if needs && (m == nil || (!m.written && !m.timedOut)) && idle >= idleHandoffAt(after) {
			d.startIdleHandoff(t, idle)
			continue
		}
		if idle < after || (needs && (m == nil || (!m.written && !m.timedOut))) {
			continue
		}
		d.idlePark(t, idle, m)
	}
}

// startIdleHandoff asks a card to write its handoff, on its own goroutine.
func (d *Daemon) startIdleHandoff(t *store.Task, idle time.Duration) {
	if d.sup.get(t.ID) == nil {
		return
	}
	// One capture at a time in a directory, as for the operator's own: the typing
	// of two would interleave. Tried again at the next tick.
	if other := d.nctx.sameDirBusy(d, t); other != "" {
		return
	}
	file := HandoffName(t)
	gen, ok := d.nctx.begin(t.ID, file, d.conversationOf(t))
	if !ok {
		return
	}
	d.nctx.captureOnly(t.ID, gen)
	mark := &handoffMark{base: d.rawIdleSince(t), capturing: true, file: file}
	d.idle.put(t.ID, mark)
	log.Printf("[atrium] %s has been idle %s, taking its handoff before it is parked",
		t.DisplayTitle(), idle.Round(time.Minute))
	d.publishTask(t.ID)
	go func() {
		step, err := d.ncCapture(t.ID, gen, file)
		done := *mark
		done.capturing = false
		done.end = time.Now()
		if err != nil {
			done.timedOut = true
			done.timeoutWhy = step + ": " + err.Error()
			log.Printf("[atrium] handoff for %s did not finish, it will be parked anyway: %s",
				t.DisplayTitle(), done.timeoutWhy)
		} else {
			done.written = true
		}
		if d.nctx.mine(t.ID, gen) {
			d.idle.put(t.ID, &done)
		} else {
			// Dismissed from the board: the operator is looking at it.
			d.idle.drop(t.ID)
		}
		d.nctx.finish(t.ID, gen)
		d.releaseHeld(t.ID)
	}()
}

// idlePark parks one card: snapshot the status, wind the runner down with its
// exit keys, then mark it parked with the status it had. The snapshot is first
// because the wind-down files a card `dead`, and that is the one thing a park
// must not leave behind. Off the tick, since the wind-down takes seconds.
func (d *Daemon) idlePark(t *store.Task, idle time.Duration, m *handoffMark) {
	r := d.sup.get(t.ID)
	if r == nil {
		return
	}
	// One wind-down at a time: the next tick can come before this one is done.
	if _, dup := d.idle.parking.LoadOrStore(t.ID, true); dup {
		return
	}
	was := t.Status
	extra := map[string]any{"by": "idle", "idle_for": int64(idle / time.Second)}
	handoff := ""
	if m != nil {
		switch {
		case m.written:
			handoff = m.file
			extra["handoff"] = m.file
		case m.timedOut:
			extra["handoff"] = "timed out"
			extra["handoff_why"] = m.timeoutWhy
		}
	}
	log.Printf("[atrium] %s has been idle %s, parking it", t.DisplayTitle(), idle.Round(time.Minute))
	go func() {
		id := t.ID
		if handoff != "" {
			val := handoff + "|" + strconv.FormatInt(int64(idle/time.Second), 10)
			if err := d.st.SetSetting(store.SettingParkHandoffPrefix+id, val); err != nil {
				log.Printf("[atrium] could not record the handoff for %s: %v", id, err)
			}
		}
		idleLeave(d, r, id)
		deadline := time.Now().Add(idleGoneWait)
		for d.sup.get(id) != nil && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
		if err := d.parkCard(id, was, extra); err != nil {
			log.Printf("[atrium] could not park %s: %v", id, err)
		}
		if m != nil && m.timedOut {
			if err := d.st.SetWhy(id, "parked without a handoff: "+m.timeoutWhy); err != nil {
				log.Printf("[atrium] could not note the missing handoff on %s: %v", id, err)
			}
			d.publishTask(id)
		}
		d.idle.drop(id)
		d.idle.parking.Delete(id)
	}()
}

// queueHandoffWake is called by unpark: a card that took a handoff before it was
// parked is told to read it, ahead of whatever woke it. Queued, never typed, like
// every message for a card that was not spoken to by its operator.
func (d *Daemon) queueHandoffWake(taskID string) {
	key := store.SettingParkHandoffPrefix + taskID
	v, err := d.st.Setting(key)
	if err != nil || strings.TrimSpace(v) == "" {
		return
	}
	file, secs, _ := strings.Cut(v, "|")
	idle := ""
	if n, err := strconv.Atoi(secs); err == nil && n > 0 {
		idle = " after " + (time.Duration(n) * time.Second).Round(time.Minute).String() + " idle"
	}
	text := fmt.Sprintf("You were parked%s. Read %s in the current directory, then act on what follows.", idle, file)
	if _, err := d.st.QueueMessage(taskID, text); err != nil {
		log.Printf("[atrium] could not queue the handoff wake for %s: %v", taskID, err)
		return
	}
	if err := d.st.SetSetting(key, ""); err != nil {
		log.Printf("[atrium] could not clear the handoff record for %s: %v", taskID, err)
	}
}
