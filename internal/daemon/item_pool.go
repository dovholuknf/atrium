package daemon

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"

	"github.com/dovholuknf/atrium/internal/small"
	"github.com/dovholuknf/atrium/internal/store"
)

// ITEMS, STAGE 3: POOLS. A pool is the resident cards that carry the tag `atrium:pool:<name>`, and it is fed ONE ITEM AT
// A TIME PER CARD. See docs/changes/items-stage3.md and docs/rnd/atrium-items-decisions.md section 7.
//
//   - THE QUEUE LIVES ON THE POOL'S ROOM, because a pool is cards on one room. A submitted item is a mirror row in the
//     state `queued` with no card. The hub learns of it when a card takes it, as the first push (`running`), since the hub
//     takes an item only with the card that runs it.
//   - A CARD IS BUSY from the moment it is typed an item until that item is `finished`. A `blocked` item counts as busy,
//     so the next one waits behind the answer.
//   - MORE THROUGHPUT IS MORE CARDS. A new item goes to the first idle card, and an item with no idle card waits in the
//     order it was submitted. Nothing is ever typed mid-item: the brief waits for the turn that ended the last one.
//   - THE WORKER IS TOLD NOTHING NEW. An item is typed to it as `atrium`, like any other, and it ends with the tools it
//     already has.

// PoolTag marks a card as a member of a pool: `atrium:pool:<name>`. Put it on a resident launch.
const PoolTag = "atrium:pool:"

var poolNameShape = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,39}$`)

// poolOf is the pool a card belongs to, or "".
func poolOf(t *store.Task) string {
	if t == nil {
		return ""
	}
	for _, tag := range t.Tags {
		if name, ok := strings.CutPrefix(strings.TrimSpace(tag), PoolTag); ok {
			return strings.ToLower(strings.TrimSpace(name))
		}
	}
	return ""
}

// poolBriefMax bounds the brief typed to a pool card, in bytes.
const poolBriefMax = 16000

type poolFeeder struct{ mu sync.Mutex }

// poolCards is the live cards of a pool: not dead, not parked. A parked or dead card takes nothing, and a pool with
// only those leaves its items queued until one comes back. A `done` card is live: a resident card is done after every
// item and stays open for the next, which is what a pool card is.
func (d *Daemon) poolCards(pool string) []*store.Task {
	all, err := d.st.List()
	if err != nil {
		log.Printf("[atrium] could not list the cards of pool %s: %v", pool, err)
		return nil
	}
	var out []*store.Task
	for _, t := range all {
		if poolOf(t) == pool && t.Status != store.StatusDead && !small.IsParked(t) {
			out = append(out, t)
		}
	}
	return out
}

// submitPoolItem queues an item on a pool and feeds it when a card is idle.
func (d *Daemon) submitPoolItem(pool, brief, ownerSpec, by string) (*store.ItemRow, int, error) {
	pool = strings.ToLower(strings.TrimSpace(pool))
	brief = strings.TrimSpace(brief)
	by, _, _ = strings.Cut(strings.TrimSpace(by), "@")
	switch {
	case !poolNameShape.MatchString(pool):
		return nil, http.StatusBadRequest, errors.New("a pool is named in lowercase letters, digits, dots, dashes and underscores")
	case brief == "":
		return nil, http.StatusBadRequest, errors.New("an item needs its brief")
	case len(brief) > poolBriefMax:
		return nil, http.StatusBadRequest, errors.New("that brief is too long. put the detail in a file the card can read")
	case len(d.poolCards(pool)) == 0:
		return nil, http.StatusConflict, errors.New("pool " + pool + " has no live card on this room. launch a resident card " +
			"with the tag " + PoolTag + pool)
	}
	kind, owner, err := resolveItemOwner(ownerSpec, by)
	if err != nil {
		return nil, http.StatusBadRequest, err
	}
	it, err := d.st.ItemCreate(store.ItemRow{
		ID: newItemID(), State: store.ItemQueued, OwnerKind: kind, Owner: owner, CreatedBy: by, Pool: pool, Brief: brief,
	})
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	d.feedPool(pool)
	if got, err := d.st.ItemGet(it.ID); err == nil {
		it = got
	}
	return it, http.StatusOK, nil
}

// feedPool types the next queued items to the idle cards of a pool, oldest item first, one per card. One pass at a time.
func (d *Daemon) feedPool(pool string) {
	if pool == "" {
		return
	}
	d.poolFeed.mu.Lock()
	defer d.poolFeed.mu.Unlock()
	queued, err := d.st.ItemsQueued(pool)
	if err != nil || len(queued) == 0 {
		if err != nil {
			log.Printf("[atrium] could not read pool %s's queue: %v", pool, err)
		}
		return
	}
	cards := d.poolCards(pool)
	for _, it := range queued {
		card := d.idlePoolCard(cards)
		if card == nil {
			return
		}
		cards = withoutCard(cards, card.ID)
		text := d.poolItemText(card, it)
		// WAITS FOR THE TURN: the end of the last item is a tool call inside a turn that has said nothing more yet.
		if _, _, err := d.deliverPeerCaused(card, store.ReservedHandle, text, "", "", d.waitsForTurn(card.ID, WhenDone)); err != nil {
			log.Printf("[atrium] could not hand item %s to %s: %v", it.ID, card.DisplayTitle(), err)
			continue
		}
		gen := 1
		if w, err := d.st.WorkItem(card.ID); err == nil && w.Generation > 0 {
			gen = w.Generation
		}
		if _, changed, err := d.st.ItemAssign(it.ID, card.ID, gen); err != nil {
			log.Printf("[atrium] item %s was handed to %s and could not be recorded: %v", it.ID, card.DisplayTitle(), err)
		} else if changed {
			d.publishTask(card.ID)
			d.kickItems()
		}
	}
}

// feedPools is feedPool for every pool with something waiting: the reaper's tick, which catches a card that came up, or
// went idle by a path that did not feed.
func (d *Daemon) feedPools() {
	pools, err := d.st.ItemPoolsQueued()
	if err != nil {
		log.Printf("[atrium] could not read the pools with items waiting: %v", err)
		return
	}
	for _, p := range pools {
		d.feedPool(p)
	}
}

func (d *Daemon) idlePoolCard(cards []*store.Task) *store.Task {
	for _, c := range cards {
		if busy, err := d.st.ItemCardBusy(c.ID); err == nil && !busy {
			return c
		}
	}
	return nil
}

func withoutCard(cards []*store.Task, id string) []*store.Task {
	out := make([]*store.Task, 0, len(cards))
	for _, c := range cards {
		if c.ID != id {
			out = append(out, c)
		}
	}
	return out
}

// poolItemText is what a card is typed for its next item. It points at LEARNINGS.md when the pool has one, because the
// file and not the conversation is what survives a new context. See learnings.go.
func (d *Daemon) poolItemText(card *store.Task, it *store.ItemRow) string {
	text := "item " + it.ID + ": " + it.Brief
	if n := learningsEntries(card); n > 0 {
		text += "\n\nRead LEARNINGS.md (" + itoa(n) + " entries) before you start."
	}
	return text
}

// handlePoolSubmit is `POST /v1/pools/{pool}/items` with `{brief, owner, by}`, which the hub calls for a submit.
func (d *Daemon) handlePoolSubmit(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Brief string `json:"brief"`
		Owner string `json:"owner"`
		By    string `json:"by"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		small.WriteJSONErr(w, http.StatusBadRequest, err)
		return
	}
	it, code, err := d.submitPoolItem(r.PathValue("pool"), in.Brief, in.Owner, in.By)
	if err != nil {
		small.WriteJSONErr(w, code, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "item_id": it.ID, "state": it.State, "pool": it.Pool})
}
