package daemon

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// Owed answers: a worker stops, and the answer it owes survives. See
// docs/rnd/long-turn-checkin-design.md section 11.
//
// An OPEN ITEM is kept for every worker that reached done, ended, needs-input or needs-permission
// owing its launcher an answer, whether or not the launcher holds its notices. It closes when the
// launcher acts on the worker (a message, an exit, a relaunch, a dismiss) and by itself when its
// reason is gone. READING NEVER CLOSES IT: that is what @ui did before its context was cleared.
//
// Pushes only go up. One to the orchestrator at owedPushAfter, held, never about the orchestrator
// itself, and one listing line to the launcher per context clear. Nothing is ever written to the
// worker, so none of these can start another. Bound: 2 + N pushes for an item.

// NoticeOwed is the source of a held notice about an open item.
const NoticeOwed = "owed"

var (
	// owedPushAfter is how long an item is open before the orchestrator is told.
	owedPushAfter = envDuration("ATRIUM_OWED_PUSH", 10*time.Minute)
	// owedPermAfter is how long a worker sits at needs-permission before it owes anything.
	owedPermAfter = envDuration("ATRIUM_OWED_PERMISSION", 5*time.Minute)
)

// owedStale is how old an ended worker may be and still open an item, so a deploy does not
// open one for every card that finished last month.
const owedStale = 24 * time.Hour

// Why an item is open.
const (
	owedEnded      = "ended"
	owedAsked      = "asked"
	owedStopped    = "stopped"
	owedPermission = "permission"
)

// owes says whether a worker owes its launcher an answer right now: why, its last words, and
// since when. Empty reason when it does not. Pure over the store, so the same test opens an
// item and later says its reason is gone.
func (d *Daemon) owes(t *store.Task, now time.Time) (reason, text string, since time.Time) {
	if t == nil || !d.reportsToLauncher(t) || isParked(t) {
		return "", "", time.Time{}
	}
	switch t.Status {
	case store.StatusNeedsPermission:
		if t.WaitingSince != nil && now.Sub(*t.WaitingSince) >= owedPermAfter {
			return owedPermission, "waiting on a permission dialog", *t.WaitingSince
		}
	case store.StatusNeedsInput, store.StatusDone, store.StatusDead:
		if a := d.st.OwedAskOf(t.ID); a != nil {
			return owedAsked, a.Text, a.At
		}
		if t.Status == store.StatusNeedsInput {
			if ended, ok := d.stoppedSilently(t); ok {
				return owedStopped, t.Recap, ended
			}
			return "", "", time.Time{}
		}
		// A report that went to the orchestrator because there was no launcher IS the item.
		reported := d.st.OwedReportedAt(t.ID)
		if reported.IsZero() || (t.OwedAt != nil && reported.Before(*t.OwedAt)) {
			reported = time.Time{}
		}
		if reported.IsZero() && t.OwesReport() && now.Sub(t.LastActivityAt) < owedStale {
			return owedEnded, t.Recap, t.LastActivityAt
		}
	}
	return "", "", time.Time{}
}

// localOrchestrator is the card on this room tagged atrium:orchestrator, other than `not`.
func (d *Daemon) localOrchestrator(not string) *store.Task {
	all, err := d.st.List()
	if err != nil {
		return nil
	}
	var best *store.Task
	for _, t := range all {
		if t.ID == not || !hasTag(t.Tags, OrchestratorTag) {
			continue
		}
		if best == nil || (best.Status == store.StatusDone && t.Status != store.StatusDone) {
			best = t
		}
	}
	return best
}

// openOwed keeps an item for a worker. Idempotent: a worker with an open item gets none.
func (d *Daemon) openOwed(t *store.Task, reason, text string, since time.Time) {
	if it, _ := d.st.OwedItemOf(t.ID); it != nil {
		return
	}
	it := store.OwedItem{Worker: t.ID, WorkerWire: t.WireName, Reason: reason, Status: t.Status,
		Text: firstWords(text), Since: since}
	launcher := d.launcherOf(t)
	switch {
	case launcher != nil:
		it.Host, it.Launcher = launcher.ID, launcher.ID
	default:
		// NOBODY KEPT BY NOBODY: the local orchestrator keeps it, else the worker's own row.
		if name, room, ok := d.remoteLauncher(t); ok {
			it.RemoteLauncher = name + "@" + room
		}
		it.Orphan = true
		it.Host = t.ID
		if o := d.localOrchestrator(t.ID); o != nil {
			it.Host = o.ID
		}
	}
	if err := d.st.PutOwedItem(it); err != nil {
		log.Printf("[atrium] could not keep an owed item for %s: %v", t.DisplayTitle(), err)
		return
	}
	line := d.owedLine(it, time.Now())
	if host, err := d.st.Get(it.Host); err == nil && host.ID != t.ID {
		d.holdNotice(host, t, NoticeOwed, "owes an answer: "+line)
		// THE TYPED NUDGE for a launcher that does not hold its notices. Only a permission wait has
		// no notice of its own: a report, an ask, a silent stop and an ending each already say it.
		if launcher != nil && reason == owedPermission && !holdsNotices(launcher) {
			if _, err := d.deliverPeerAs(store.DeliveryNotice, launcher, t.WireName, truncatePeer(t.WireName+" is "+line)); err != nil {
				log.Printf("[atrium] could not nudge %s about %s: %v", launcher.DisplayTitle(), t.DisplayTitle(), err)
			}
		}
	}
	if err := d.st.LogWorkAtrium(t.ID, "owes an answer ("+reason+"), kept on its launcher's card"); err != nil {
		log.Printf("[atrium] could not log the owed item on %s: %v", t.DisplayTitle(), err)
	}
	d.publishTask(it.Host)
}

func firstWords(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// owedLine is one item as the listing line and the held notice say it.
func (d *Daemon) owedLine(it store.OwedItem, now time.Time) string {
	mins := int(now.Sub(it.Since) / time.Minute)
	switch it.Reason {
	case owedAsked:
		return fmt.Sprintf("%s (asked you a question %dm ago)", it.WorkerWire, mins)
	case owedEnded:
		return fmt.Sprintf("%s (%s, no report)", it.WorkerWire, it.Status)
	}
	return fmt.Sprintf("%s (%s %dm)", it.WorkerWire, it.Status, mins)
}

// closeOwed closes a worker's item, and tells the board the host's count dropped.
func (d *Daemon) closeOwed(workerID, how string) {
	it, _ := d.st.OwedItemOf(workerID)
	if it == nil {
		return
	}
	if closed, err := d.st.CloseOwedItem(workerID); err != nil {
		log.Printf("[atrium] could not close the owed item for %s: %v", workerID, err)
		return
	} else if !closed {
		return
	}
	log.Printf("[atrium] owed item for %s closed: %s", it.WorkerWire, how)
	d.publishTask(it.Host)
}

// owedPass is one tick: open what is owed, close what is not, push what has waited.
func (d *Daemon) owedPass(now time.Time) {
	items, err := d.st.OpenOwedItems()
	if err != nil {
		log.Printf("[atrium] reading owed items: %v", err)
		return
	}
	for _, it := range items {
		t, err := d.st.Get(it.Worker)
		if err != nil || t.ArchivedAt != nil {
			d.closeOwed(it.Worker, "the worker is gone")
			continue
		}
		// RE-CHECKED BEFORE EVERY PUSH: an item whose reason is gone is closed, never pushed.
		if reason, _, _ := d.owes(t, now); reason == "" {
			d.closeOwed(it.Worker, "its reason is gone")
			continue
		}
		if it.Pushed.IsZero() && now.Sub(it.Since) >= owedPushAfter {
			d.pushOwed(t, it, now)
		}
	}
	tasks, err := d.st.List(store.StatusNeedsInput, store.StatusNeedsPermission, store.StatusDone, store.StatusDead)
	if err != nil {
		return
	}
	for _, t := range tasks {
		if reason, text, since := d.owes(t, now); reason != "" && d.mayReopen(t, reason, since) {
			d.openOwed(t, reason, text, since)
		}
	}
}

// mayReopen says whether a closed item's reason is a NEW one. A status change moves a card's last
// activity, so an exited or dismissed item must not come back because the card went dead: an ended
// item reopens only on a launcher prompt newer than the close, any other reason on a new
// occurrence of it (a new question, a new permission wait).
func (d *Daemon) mayReopen(t *store.Task, reason string, since time.Time) bool {
	c := d.st.OwedClosedOf(t.ID)
	if c == nil {
		return true
	}
	if reason != c.Reason {
		return since.After(c.At)
	}
	if reason == owedEnded {
		// A PROMPT AFTER THE WORKER LAST ENDED is new work. Compared with when it ended and not with
		// the close: a typed say writes the prompt before it closes the item, so the close is
		// always the later of the two. A status change moves last activity, not owed_at, so an
		// exit followed by the card going dead stays closed.
		return t.OwedAt != nil && t.OwedAt.After(c.Since)
	}
	return since.After(c.At)
}

// pushOwed tells the orchestrator once, held. The order: a card here tagged atrium:orchestrator,
// else the one the hub reaches, through the relay outbox like notifyRemoteLauncher, else nothing
// and the board's chip is all there is. NEVER THE ORCHESTRATOR ABOUT ITSELF.
func (d *Daemon) pushOwed(worker *store.Task, it store.OwedItem, now time.Time) {
	it.Pushed = now
	if err := d.st.PutOwedItem(it); err != nil {
		log.Printf("[atrium] could not record the push for %s: %v", it.WorkerWire, err)
		return
	}
	if hasTag(worker.Tags, OrchestratorTag) {
		return
	}
	mins := int(now.Sub(it.Since) / time.Minute)
	var text string
	switch l, err := d.st.Get(it.Launcher); {
	case it.Launcher != "" && err == nil:
		if hasTag(l.Tags, OrchestratorTag) {
			return
		}
		text = fmt.Sprintf("%s has not answered %s for %d minutes. %s is waiting at %s. Its last words: %s",
			l.WireName, it.WorkerWire, mins, it.WorkerWire, it.Status, orWord(it.Text, "none"))
	case it.RemoteLauncher != "":
		text = fmt.Sprintf("%s has not answered %s for %d minutes. %s is waiting at %s. Its last words: %s",
			it.RemoteLauncher, it.WorkerWire, mins, it.WorkerWire, it.Status, orWord(it.Text, "none"))
	default:
		text = fmt.Sprintf("%s has no launcher, and has been waiting at %s for %d minutes. Its last words: %s",
			it.WorkerWire, it.Status, mins, orWord(it.Text, "none"))
	}
	text = truncatePeer(text)
	if o := d.localOrchestrator(worker.ID); o != nil {
		d.holdNotice(o, worker, NoticeOwed, text)
		return
	}
	d.pushRemoteOrchestrator(worker, text)
}

// remoteOrchestratorKey caches the hub's orchestrator, so a push is held when the hub is away.
const remoteOrchestratorKey = "orchestrator_remote"

// pushRemoteOrchestrator holds one notice in the relay outbox for the orchestrator the hub
// reaches. Found by asking the hub for its peers, and remembered, so an away hub still holds it.
func (d *Daemon) pushRemoteOrchestrator(worker *store.Task, text string) {
	name, room := d.remoteOrchestrator()
	if name == "" {
		return
	}
	if _, err := d.holdRelay(worker, worker.WireName, name, room, "", text, "", "", store.RelaySourceNotice); err != nil {
		log.Printf("[atrium] could not hold an owed notice to %s@%s: %v", name, room, err)
	}
}

func (d *Daemon) remoteOrchestrator() (name, room string) {
	if rl := d.relay(); rl != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		list, _, err := rl.Peers(ctx, true, false)
		cancel()
		if err == nil {
			// BY TAG ONLY. A card on any room may be told the name `orchestrator`, and would
			// otherwise be sent every orphan report. A hub that sends no tags gets the chip only.
			for _, p := range list {
				if hasTag(p.Tags, OrchestratorTag) {
					_ = d.st.SetSetting(remoteOrchestratorKey, p.Handle+"@"+p.Room)
					return p.Handle, p.Room
				}
			}
			// The hub answered and lists none: whatever was remembered is stale.
			_ = d.st.SetSetting(remoteOrchestratorKey, "")
			return "", ""
		}
	}
	// THE HUB IS AWAY: the one last found by tag, so the notice is held.
	v, _ := d.st.Setting(remoteOrchestratorKey)
	if n, r, err := SplitAddress(v); err == nil {
		return n, r
	}
	return "", ""
}

// owedFor is how many open items a card keeps, the oldest's age start, and whether any is an
// orphan's, for the board's row.
func (d *Daemon) owedFor(t *store.Task) (int, string, bool) {
	items, err := d.st.OpenOwedItems()
	if err != nil {
		return 0, "", false
	}
	n, orphan := 0, false
	oldest := time.Time{}
	for _, it := range items {
		if it.Host != t.ID {
			continue
		}
		n++
		orphan = orphan || (it.Orphan && it.Host == it.Worker)
		if oldest.IsZero() || it.Since.Before(oldest) {
			oldest = it.Since
		}
	}
	if n == 0 {
		return 0, "", false
	}
	return n, oldest.UTC().Format(store.TimeFormat), orphan
}

// listOwed queues the one line for a launcher whose context just cleared: every open item it
// keeps, in one message, carried by its first tool call (step 2 of the permission chain). Nothing
// when it keeps none. An orphan on the worker's own row is not the worker's to be told about.
func (d *Daemon) listOwed(host *store.Task) {
	if host == nil {
		return
	}
	items, err := d.st.OpenOwedItems()
	if err != nil {
		return
	}
	var parts []string
	now := time.Now()
	for _, it := range items {
		if it.Host == host.ID && it.Worker != host.ID {
			parts = append(parts, d.owedLine(it, now))
		}
	}
	if len(parts) == 0 {
		return
	}
	noun := "items"
	if len(parts) == 1 {
		noun = "item"
	}
	line := fmt.Sprintf("%d open %s from your workers: %s. atrium_task notices to read them.",
		len(parts), noun, strings.Join(parts, ", "))
	if _, err := d.st.QueueFromPeer(host.ID, truncatePeer(line), "atrium"); err != nil {
		log.Printf("[atrium] could not list the owed items for %s: %v", host.DisplayTitle(), err)
	}
}

// owedSaid is what a message between sessions does to owing, called from peerSaid. A worker's
// needs say to its launcher is a question the launcher owes an answer to; the launcher's message
// to the worker answers it, and closes the item. An fyi opens nothing.
func (d *Daemon) owedSaid(sender, target *store.Task, kind, text string) {
	if sender == nil || target == nil {
		return
	}
	if sender.Launched() && (sender.SpawnedByID == target.ID || d.st.Qualify(sender.SpawnedBy) == target.WireName) {
		if kind == KindNeeds {
			if err := d.st.SetOwedAsk(sender.ID, firstWords(text)); err != nil {
				log.Printf("[atrium] could not record %s's question: %v", sender.DisplayTitle(), err)
			}
		}
		return
	}
	if target.Launched() && (target.SpawnedByID == sender.ID || d.st.Qualify(target.SpawnedBy) == sender.WireName) {
		_ = d.st.ClearOwedAsk(target.ID)
		d.closeOwed(target.ID, "its launcher messaged it")
	}
}

// orphanReport makes a report that has no launcher to go to visible: held on the local
// orchestrator's card, else held in the relay outbox for the hub's. Once per report.
func (d *Daemon) orphanReport(worker *store.Task, body string) {
	if hasTag(worker.Tags, OrchestratorTag) {
		return
	}
	text := truncatePeer(worker.WireName + " reported, and has no launcher to hear it: " + body)
	// THIS NOTICE IS THE ITEM: the worker is not also "ended with no report".
	if err := d.st.SetOwedReported(worker.ID); err != nil {
		log.Printf("[atrium] could not mark %s's report as told: %v", worker.DisplayTitle(), err)
	}
	if o := d.localOrchestrator(worker.ID); o != nil {
		d.holdNotice(o, worker, NoticeNoLauncher, text)
		return
	}
	d.pushRemoteOrchestrator(worker, text)
}

// NoticeNoLauncher is the source of a held notice about a report that reached nobody.
const NoticeNoLauncher = "report-no-launcher"
