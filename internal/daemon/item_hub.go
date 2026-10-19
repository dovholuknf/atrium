package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// ITEMS, STAGE 2A: THE ROOM'S SIDE. The hub owns items (internal/hubstore) and the room keeps a mirror and an outbox
// (store/item.go). This file is what moves an item: a launch creates it, an ending blocks or finishes it, a turn that
// starts from an unblock runs it again, and every change is pushed to the hub when the hub answers.
// See docs/changes/items-stage2a.md.

// ItemOwnerTag carries the owner an item launch asked for, as `atrium:item-owner:<spec>`. A tag because a tag already
// travels with a launch through every door, hub and room, and a new field would need plumbing through all of them.
const ItemOwnerTag = "atrium:item-owner:"

// itemOwnerSpec is the owner a launch's tags ask for, "" for the creating session.
func itemOwnerSpec(tags []string) string {
	for _, t := range tags {
		if strings.HasPrefix(t, ItemOwnerTag) {
			return strings.TrimSpace(strings.TrimPrefix(t, ItemOwnerTag))
		}
	}
	return ""
}

var errTesterOwner = errors.New("the tester has several holders, so it cannot own an item. name orchestrator, merger, " +
	"director or human")

// resolveItemOwner is hubstore.ResolveOwner for the room, which must not import the hub's store. A test in
// internal/cli keeps the two equal. Empty is the creating session, and with no creating session (the operator's own
// launch) the human made it, so the human owns it.
func resolveItemOwner(spec, creator string) (kind, owner string, err error) {
	spec = strings.ToLower(strings.TrimSpace(spec))
	creator = strings.TrimSpace(creator)
	switch {
	case spec == "":
		if creator == "" || creator == store.HumanLauncher {
			return store.ItemOwnerHuman, "", nil
		}
		return store.ItemOwnerSession, creator, nil
	case spec == "human":
		return store.ItemOwnerHuman, "", nil
	}
	switch role := strings.TrimPrefix(spec, "role:"); role {
	case "tester":
		return "", "", errTesterOwner
	case "orchestrator", "merger", "director":
		return store.ItemOwnerRole, role, nil
	}
	return "", "", errors.New("an owner is empty (the creating session), human, orchestrator, merger or director")
}

// checkItemLaunch refuses an item launch whose owner cannot be, BEFORE a card exists. Nothing else is its business.
func checkItemLaunch(req LaunchRequest) error {
	if !req.Item && !hasTag(req.Tags, ItemTag) && workOfTags(req.Tags) == "" {
		return nil
	}
	_, _, err := resolveItemOwner(itemOwnerSpec(req.Tags), req.SpawnedBy)
	return err
}

func newItemID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "it-" + hex.EncodeToString(b)
}

// createItem records the item a fresh item launch is: running, owned as asked, with its first push owed.
func (d *Daemon) createItem(task *store.Task, ownerSpec string) (*store.ItemRow, error) {
	kind, owner, err := resolveItemOwner(ownerSpec, task.SpawnedBy)
	if err != nil {
		return nil, err
	}
	gen := 1
	if w, err := d.st.WorkItem(task.ID); err == nil && w.Generation > 0 {
		gen = w.Generation
	}
	it, err := d.st.ItemCreate(store.ItemRow{
		ID: newItemID(), TaskID: task.ID, Generation: gen, State: store.ItemRunning,
		OwnerKind: kind, Owner: owner, CreatedBy: task.SpawnedBy,
		// THE ATTACHMENT, from the card's own tag. It rides the push through the outbox, so a room that was down when the
		// card was attached says so when it is back, and the hub treats a replay as nothing.
		WorkID: workOfTags(task.Tags), Step: stepOfTags(task.Tags),
	})
	if err != nil {
		return nil, err
	}
	d.kickItems()
	return it, nil
}

// itemNoticeKey is one notice per item, revision and state: a replay of the same change is the same key.
func itemNoticeKey(id string, revision int, state string) string {
	return id + "/" + itoa(revision) + "/" + state
}

// ── the outbox ──────────────────────────────────────────────────────────────

type itemPush struct{ mu sync.Mutex }

// kickItems pushes what is owed, off the caller's path.
func (d *Daemon) kickItems() {
	go d.flushItems()
	go d.flushTaskOwed()
}

// flushItems pushes what the hub has not taken, oldest first, through the link this room already holds. One pass at a
// time. A hub that is not answering stops the pass (every other row would fail the same way) and the rows stay with
// their attempts. A replay is a no-op on the hub, so a push that was taken and not recorded is sent again safely.
func (d *Daemon) flushItems() {
	d.itemPush.mu.Lock()
	defer d.itemPush.mu.Unlock()
	rows, err := d.st.ItemsOwed(200)
	if err != nil {
		log.Printf("[atrium] could not read the items owed to the hub: %v", err)
		return
	}
	if len(rows) == 0 {
		return
	}
	ha, _ := d.relay().(HubAsker)
	if ha == nil {
		return
	}
	for _, r := range rows {
		ctx, cancel := context.WithTimeout(context.Background(), relayWait)
		res, err := ha.Hub(ctx, "", http.MethodPost, "/_hub/items/sync", json.RawMessage(r.Body))
		cancel()
		switch {
		case err != nil:
			_ = d.st.ItemPushFailed(r.ID, r.Revision, r.State, err.Error())
			return
		case !res.OK:
			// REFUSED, not unreachable: this row stays and the next one is still tried.
			_ = d.st.ItemPushFailed(r.ID, r.Revision, r.State, res.Error)
		default:
			if err := d.st.ItemPushed(r.ID, r.Revision, r.State); err != nil {
				log.Printf("[atrium] the hub took item %s revision %d but it could not be cleared: %v", r.ID, r.Revision, err)
			}
		}
	}
}

// ── moving an item ──────────────────────────────────────────────────────────

// moveItem is one transition, published and pushed when it changed something.
func (d *Daemon) moveItem(id, state, note string) (*store.ItemRow, bool) {
	it, changed, err := d.st.ItemTransition(id, state, note)
	if err != nil {
		log.Printf("[atrium] could not move item %s to %s: %v", id, state, err)
		return nil, false
	}
	if changed {
		if it.TaskID != "" {
			d.publishTask(it.TaskID)
		}
		d.kickItems()
		// A pool card that finished an item is idle: the next one is typed. See item_pool.go.
		if state == store.ItemFinished && it.Pool != "" {
			go d.feedPool(it.Pool)
		}
	}
	return it, changed
}

// itemEnding is an ending that moved an item.
type itemEnding struct {
	item    *store.ItemRow
	changed bool
}

// itemEnded moves a card's item for an ending: done is finished, blocked is blocked. Nil for a card with no item
// here, which keeps the stage-1 behaviour. FINISHED IS TERMINAL, and the same state again is no change, so a replay is
// one revision and one notice.
func (d *Daemon) itemEnded(task *store.Task, in FinishRequest) *itemEnding {
	isolated := isItem(task) || poolOf(task) != ""
	if !in.Ended || (!isolated && workOfTags(task.Tags) == "") {
		return nil
	}
	it, err := d.st.ItemOfTask(task.ID)
	if err != nil || it == nil {
		return nil
	}
	target := ""
	switch in.Status {
	case ReportDone:
		target = store.ItemFinished
	case ReportBlocked:
		target = store.ItemBlocked
	}
	if target == "" || it.State == store.ItemFinished {
		if !isolated {
			return nil
		}
		return &itemEnding{item: it}
	}
	// A FINISHED ITEM CARRIES THE COMMIT IT NAMED, as its note: the hub reads the sha from there, which is how a designer's
	// finishing becomes a design it can look at. A blocked one has no note, as before.
	note := ""
	if target == store.ItemFinished {
		note = strings.TrimSpace(in.SHA)
	}
	moved, changed := d.moveItem(it.ID, target, note)
	if !isolated {
		// AN ATTACHED CARD THAT IS NOT ISOLATED keeps the report it always had, to its launcher, in its own words. Its
		// assignment moves, and nothing else about its ending does.
		return nil
	}
	if moved == nil {
		return &itemEnding{item: it}
	}
	return &itemEnding{item: moved, changed: changed}
}

// itemNoticeText is what the owner reads, from atrium. It never names the worker's tools or the owner.
func itemNoticeText(task *store.Task, it *store.ItemRow, in FinishRequest, sha string, recap string) string {
	who := task.WireName
	if task.Alias != "" {
		who = task.Alias
	}
	head := "item " + it.ID + " (@" + who + ") "
	if it.State == store.ItemBlocked {
		ask := strings.Join(strings.Fields(in.Ask), " ")
		return head + "blocked: " + ask + ". answer: atrium_item unblock " + it.ID + " <your answer>, or atrium_item cancel " + it.ID
	}
	head += "finished"
	if sha != "" {
		if len(sha) > 8 {
			sha = sha[:8]
		}
		head += " " + sha
	}
	if line, _, _ := strings.Cut(strings.TrimSpace(recap), "\n"); line != "" {
		head += ": " + line
	}
	return head + ". read it: atrium_item get " + it.ID
}

// itemNotify tells the owner, once, of a blocked or finished item. A human owner is told by the board and no session
// hears. A role owner is whoever holds the role now, asked of the hub. Reached by nobody, the end is held for the
// orchestrator like any other report that found no one.
func (d *Daemon) itemNotify(task *store.Task, it *store.ItemRow, text string) {
	d.itemNotifyKeyed(task, it, itemNoticeKey(it.ID, it.Revision, it.State), text)
}

// itemNotifyKeyed is itemNotify under a key of the caller's: one notice per key.
func (d *Daemon) itemNotifyKeyed(task *store.Task, it *store.ItemRow, key, text string) {
	switch it.OwnerKind {
	case store.ItemOwnerHuman:
		return
	case store.ItemOwnerRole:
		if !d.itemNotifyHolder(task, it, key, text) {
			d.orphanReport(task, text)
			d.itemUnreached(task, it)
		}
		return
	}
	// A POOL ITEM'S OWNER is whoever submitted it, not the pool card's launcher, which launched the card for every item.
	if it.Pool != "" {
		if owner, err := d.st.GetByWireName(d.st.Qualify(it.Owner)); err == nil && owner != nil {
			d.itemQueue(task, owner.ID, key, text)
			return
		}
		d.orphanReport(task, text)
		d.itemUnreached(task, it)
		return
	}
	if launcher := d.launcherOf(task); launcher != nil {
		d.itemQueue(task, launcher.ID, key, text)
		return
	}
	if spec := d.launcherRelay(task, text); spec != nil {
		if _, err := d.holdRelay(task, spec.FromWire, spec.ToName, spec.ToRoom, spec.ToCard, spec.Text, "", "",
			store.RelaySourceNotice); err != nil {
			log.Printf("[atrium] could not hold item %s's notice for %s: %v", it.ID, spec.ToRoom, err)
			d.orphanReport(task, text)
			d.itemUnreached(task, it)
		}
		return
	}
	d.orphanReport(task, text)
	d.itemUnreached(task, it)
}

// itemUnreached alerts for a blocked item whose notice found nobody. The report held for the orchestrator is the
// notice, and this is the stuck alert the spec asks for when nothing resolves at delivery.
func (d *Daemon) itemUnreached(task *store.Task, it *store.ItemRow) {
	if it.State == store.ItemBlocked {
		d.itemAlert(task, it, ItemStuckOwnerGone)
	}
}

// itemQueue queues the notice on a card here, and resumes it when it is parked: the owner is the one who has to act.
func (d *Daemon) itemQueue(worker *store.Task, toID, key, text string) {
	if _, err := d.st.ItemNotice(worker.ID, toID, key, truncatePeer(text)); err != nil {
		log.Printf("[atrium] could not queue item notice %s: %v", key, err)
		return
	}
	if to, err := d.st.Get(toID); err == nil && isParked(to) {
		if err := d.unpark(to.ID, "report"); err != nil {
			log.Printf("[atrium] could not resume %s for an item notice: %v", to.DisplayTitle(), err)
		}
	}
	d.publishTask(toID)
}

// itemNotifyHolder asks the hub who holds a role owner's role, and says whether anybody was told.
func (d *Daemon) itemNotifyHolder(task *store.Task, it *store.ItemRow, key, text string) bool {
	ha, _ := d.relay().(HubAsker)
	if ha == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), relayWait)
	defer cancel()
	res, err := ha.Hub(ctx, "", http.MethodGet, "/_hub/items/"+it.ID, nil)
	if err != nil || !res.OK {
		return false
	}
	var got struct {
		Holder struct {
			Room string `json:"room"`
			Card string `json:"card"`
		} `json:"holder"`
	}
	if json.Unmarshal(res.Data, &got) != nil || got.Holder.Card == "" {
		return false
	}
	if room := d.otherRoom(got.Holder.Room); room != "" {
		_, err := d.holdRelay(task, task.WireName, "", room, got.Holder.Card, truncatePeer(text), "", "", store.RelaySourceNotice)
		return err == nil
	}
	if _, err := d.st.Get(got.Holder.Card); err != nil {
		return false
	}
	d.itemQueue(task, got.Holder.Card, key, text)
	return true
}

// ── a turn starting ─────────────────────────────────────────────────────────

// itemTurnStarted runs a blocked item again when a turn starts from its answer, or from the human typing. A running or
// finished item is untouched. The answer being typed, or delivered, is not a turn.
func (d *Daemon) itemTurnStarted(taskID string, operator bool) {
	it, err := d.st.ItemOfTask(taskID)
	if err != nil || it == nil || it.State != store.ItemBlocked {
		return
	}
	if it.UnblockDelivered || operator {
		d.moveItem(it.ID, store.ItemRunning, "")
	}
}

// itemCarried is a Stop hook carrying messages: one of them being the item's answer is the turn starting.
func (d *Daemon) itemCarried(taskID string, msgs []*store.Message) {
	if len(msgs) == 0 {
		return
	}
	it, err := d.st.ItemOfTask(taskID)
	if err != nil || it == nil || it.State != store.ItemBlocked || it.UnblockMsg == "" {
		return
	}
	for _, m := range msgs {
		if m.ID == it.UnblockMsg {
			d.moveItem(it.ID, store.ItemRunning, "")
			return
		}
	}
}

// ── what an owner does ──────────────────────────────────────────────────────

func (d *Daemon) itemOf(id string) (*store.ItemRow, int, error) {
	it, err := d.st.ItemGet(id)
	switch {
	case errors.Is(err, store.ErrNoItem):
		return nil, http.StatusNotFound, errors.New("this room holds no item " + id)
	case err != nil:
		return nil, http.StatusInternalServerError, err
	}
	return it, http.StatusOK, nil
}

// itemUnblock types the owner's answer to the worker as atrium. The item stays blocked: it runs again when the
// worker's turn starts from the answer, not when the words are typed. The text never names the owner.
func (d *Daemon) itemUnblock(id, answer, by string) (map[string]any, int, error) {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return nil, http.StatusBadRequest, errors.New("an unblock carries the answer")
	}
	it, code, err := d.itemOf(id)
	if err != nil {
		return nil, code, err
	}
	if it.State != store.ItemBlocked {
		return nil, http.StatusConflict, errors.New("item " + id + " is " + it.State + ", not blocked")
	}
	target, err := d.st.Get(it.TaskID)
	if it.TaskID == "" || err != nil {
		return nil, http.StatusGone, errors.New("the card that runs item " + id + " is gone")
	}
	text := itemAnswerPrefix + answer
	typed, mid, err := d.deliverPeerCaused(target, store.ReservedHandle, text, "", "", d.waitsForTurn(target.ID, WhenImmediate))
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	if typed {
		mid = "typed"
	}
	if err := d.st.ItemAwaitUnblock(id, mid, answer); err != nil {
		return nil, http.StatusInternalServerError, err
	}
	if typed {
		_, _ = d.st.ItemUnblockDelivered(mid)
	}
	log.Printf("[atrium] item %s unblocked by %s: the answer is %s", id, by, map[bool]string{true: "typed", false: "queued"}[typed])
	return map[string]any{"ok": true, "item_id": id, "state": it.State, "typed": typed}, http.StatusOK, nil
}

// itemCancel finishes an item at its owner's word and asks the worker to leave. Nobody is noticed: the owner did it.
func (d *Daemon) itemCancel(id, by string) (map[string]any, int, error) {
	it, code, err := d.itemOf(id)
	if err != nil {
		return nil, code, err
	}
	if it.State == store.ItemFinished {
		return nil, http.StatusConflict, errors.New("item " + id + " is already finished")
	}
	moved, _ := d.moveItem(id, store.ItemFinished, "cancelled by owner")
	if moved == nil {
		return nil, http.StatusInternalServerError, errors.New("could not cancel item " + id)
	}
	switch {
	case it.TaskID == "":
		// Still queued: no card has it, so there is nobody to ask.
	case it.Pool != "":
		// A pool card stays open for the next item. It is told to put this one down, which is not a turn it owes.
		if card, err := d.st.Get(it.TaskID); err == nil {
			text := "item " + id + " was cancelled. stop work on it and wait for the next item."
			if _, _, err := d.deliverPeerCaused(card, store.ReservedHandle, text, "", "", d.waitsForTurn(card.ID, WhenImmediate)); err != nil {
				log.Printf("[atrium] item %s cancelled by %s, and its card was not told: %v", id, by, err)
			}
		}
	default:
		if card, err := d.st.Get(it.TaskID); err == nil {
			d.startTaskCapture(card, FinishRequest{}, true)
		}
		go func() {
			if err := d.askExit(it.TaskID, 10*time.Second); err != nil {
				log.Printf("[atrium] item %s cancelled by %s, and its worker was not asked to leave: %v", id, by, err)
			}
		}()
	}
	return map[string]any{"ok": true, "item_id": id, "state": moved.State}, http.StatusOK, nil
}

// itemSentBackPrefix leads the note typed to a worker whose finished work was sent back.
const itemSentBackPrefix = "your work was sent back. what to change: "

// itemSendBack is the human sending a finished item back with a note: the item runs again, and the note is typed to its card
// as atrium, the way an unblock answer is. THE ITEM RUNS AT ONCE, not when the turn starts, because a sent-back design is
// work the designer owes from the moment it is said, and the hub needs the next finish to read. The text never names who sent
// it back. A card that is gone is a 410 and the item is left finished.
func (d *Daemon) itemSendBack(id, note, by string) (map[string]any, int, error) {
	note = strings.TrimSpace(note)
	if note == "" {
		return nil, http.StatusBadRequest, errors.New("a send back carries the note")
	}
	it, code, err := d.itemOf(id)
	if err != nil {
		return nil, code, err
	}
	if it.State != store.ItemFinished {
		return nil, http.StatusConflict, errors.New("item " + id + " is " + it.State + ", not finished, so there is nothing to send back")
	}
	target, err := d.st.Get(it.TaskID)
	if it.TaskID == "" || err != nil {
		return nil, http.StatusGone, errors.New("the card that ran item " + id + " is gone")
	}
	text := itemSentBackPrefix + note
	typed, _, err := d.deliverPeerCaused(target, store.ReservedHandle, text, "", "", d.waitsForTurn(target.ID, WhenImmediate))
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	moved, _ := d.moveItem(id, store.ItemRunning, "sent back")
	if moved == nil {
		return nil, http.StatusInternalServerError, errors.New("could not send item " + id + " back")
	}
	log.Printf("[atrium] item %s sent back by %s: the note is %s", id, by, map[bool]string{true: "typed", false: "queued"}[typed])
	return map[string]any{"ok": true, "item_id": id, "state": moved.State, "typed": typed}, http.StatusOK, nil
}

// handleItemAct is `POST /v1/items/{id}/{unblock|cancel|sendback}` with `{answer, by}`, which the hub calls for an owner.
func (d *Daemon) handleItemAct(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Answer string `json:"answer"`
		By     string `json:"by"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err)
		return
	}
	var out map[string]any
	var code int
	var err error
	switch r.PathValue("act") {
	case "unblock":
		out, code, err = d.itemUnblock(r.PathValue("id"), in.Answer, in.By)
	case "cancel":
		out, code, err = d.itemCancel(r.PathValue("id"), in.By)
	case "sendback":
		out, code, err = d.itemSendBack(r.PathValue("id"), in.Answer, in.By)
	default:
		writeJSONErr(w, http.StatusNotFound, errors.New("an item is unblocked, cancelled or sent back"))
		return
	}
	if err != nil {
		writeJSONErr(w, code, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// ResolveItemOwner is resolveItemOwner for a test that holds it equal to the hub's.
func ResolveItemOwner(spec, creator string) (kind, owner string, err error) {
	return resolveItemOwner(spec, creator)
}
