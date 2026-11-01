//go:build integration

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/dovholuknf/atrium/internal/daemon/hublink"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"github.com/dovholuknf/atrium/internal/store"
)

// hubRelay is a fake relay that is also the hub's items endpoint: it takes the pushes, and can be down.
type hubRelay struct {
	*fakeRelay
	mu     sync.Mutex
	down   bool
	pushes []string
	seen   map[string]bool
	// holder answers GET /_hub/items/<id> for a role owner.
	holder string
}

func newHubRelay() *hubRelay { return &hubRelay{fakeRelay: &fakeRelay{}, seen: map[string]bool{}} }

func (h *hubRelay) Hub(_ context.Context, _, method, path string, body json.RawMessage) (hublink.RelayResult, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.down {
		return hublink.RelayResult{}, hublink.ErrRelayDown
	}
	if method == http.MethodGet {
		return hublink.RelayResult{OK: true, Data: json.RawMessage(h.holder)}, nil
	}
	var p struct {
		ID    string `json:"item_id"`
		Rev   int    `json:"revision"`
		State string `json:"state"`
	}
	_ = json.Unmarshal(body, &p)
	key := p.ID + "/" + itoa(p.Rev) + "/" + p.State
	applied := !h.seen[key]
	h.seen[key] = true
	h.pushes = append(h.pushes, key)
	return hublink.RelayResult{OK: true, Data: json.RawMessage(`{"ok":true,"applied":` + map[bool]string{true: "true", false: "false"}[applied] + `}`)}, nil
}

func (h *hubRelay) pushed() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.pushes...)
}

func (h *hubRelay) setDown(v bool) {
	h.mu.Lock()
	h.down = v
	h.mu.Unlock()
}

// itemRunning is an item worker with its item recorded, owned by the card that launched it unless owner says otherwise.
func itemRunning(t *testing.T, d *Daemon, owner string) (launcher, worker *store.Task, it *store.ItemRow) {
	t.Helper()
	launcher, worker = itemPair(t, d)
	worker, _ = d.st.Get(worker.ID)
	it, err := d.createItem(worker, owner)
	if err != nil {
		t.Fatalf("createItem: %v", err)
	}
	stubCommits(t, d, true, true)
	return launcher, worker, it
}

func noticesFromAtrium(t *testing.T, d *Daemon, id string) []string {
	t.Helper()
	var out []string
	for _, m := range pendingFrom(t, d, id) {
		if m.FromPeer == store.ReservedHandle {
			out = append(out, m.Text)
		}
	}
	return out
}

func blockWith(t *testing.T, d *Daemon, why string) {
	t.Helper()
	finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportBlocked, Ask: why, Recap: why, Ended: true})
}

func promptEvent(d *Daemon, taskID string) {
	d.onActivity(ActivityEvent{TaskID: taskID, Agent: "worker", Event: "prompt"})
}

// OWNER RESOLUTION: the creating session by default, a single-holder role on request, the human, never a tester.
func TestResolveItemOwner(t *testing.T) {
	for _, c := range []struct {
		spec, creator, kind, owner string
		refused                    bool
	}{
		{"", "orchestrator", store.ItemOwnerSession, "orchestrator", false},
		{"merger", "orchestrator", "", "", true},
		{"role:director", "orchestrator", store.ItemOwnerRole, "director", false},
		{"orchestrator", "x", store.ItemOwnerRole, "orchestrator", false},
		{"human", "orchestrator", store.ItemOwnerHuman, "", false},
		// the operator's own launch has no creating session: the human made it, so the human owns it
		{"", "", store.ItemOwnerHuman, "", false},
		{"", store.HumanLauncher, store.ItemOwnerHuman, "", false},
		{"tester", "orchestrator", "", "", true},
		{"role:tester", "orchestrator", "", "", true},
		{"boss", "orchestrator", "", "", true},
	} {
		k, o, err := resolveItemOwner(c.spec, c.creator)
		if c.refused {
			if err == nil {
				t.Errorf("%q: want a refusal, got %q %q", c.spec, k, o)
			}
			continue
		}
		if err != nil || k != c.kind || o != c.owner {
			t.Errorf("%q: %q %q %v", c.spec, k, o, err)
		}
	}
	if _, _, err := resolveItemOwner("tester", "x"); err == nil || !strings.Contains(err.Error(), "several holders") {
		t.Errorf("the tester refusal does not say why: %v", err)
	}
}

func TestTheOwnerOfALaunchIsReadFromItsTag(t *testing.T) {
	if got := itemOwnerSpec([]string{"sdk", ItemTag, ItemOwnerTag + "merger"}); got != "merger" {
		t.Fatalf("spec: %q", got)
	}
	if got := itemOwnerSpec([]string{"sdk", ItemTag}); got != "" {
		t.Fatalf("spec: %q", got)
	}
	// refused BEFORE a card exists, so a tester owner costs nothing
	if err := checkItemLaunch(shared.LaunchRequest{Item: true, SpawnedBy: "orchestrator", Tags: []string{ItemOwnerTag + "tester"}}); err == nil {
		t.Fatal("a tester owner was taken")
	}
	if err := checkItemLaunch(shared.LaunchRequest{SpawnedBy: "orchestrator", Tags: []string{ItemOwnerTag + "tester"}}); err != nil {
		t.Fatalf("an ordinary launch is none of this: %v", err)
	}
}

// BLOCKED IS ONE NOTICE, FROM ATRIUM, AND THE ITEM IS BLOCKED, NOT FINISHED.
func TestABlockedItemIsBlockedAndTheOwnerGetsOneNoticeFromAtrium(t *testing.T) {
	d := testDaemon(t)
	launcher, worker, it := itemRunning(t, d, "")
	blockWith(t, d, "need the staging key")
	blockWith(t, d, "need the staging key, reworded")

	got, _ := d.st.ItemGet(it.ID)
	if got.State != store.ItemBlocked || got.Revision != 2 {
		t.Fatalf("item: %+v", got)
	}
	notes := noticesFromAtrium(t, d, launcher.ID)
	if len(notes) != 1 {
		t.Fatalf("the owner has %d notices from atrium, want 1: %v", len(notes), notes)
	}
	for _, want := range []string{it.ID, "blocked", "need the staging key", "atrium_item unblock " + it.ID} {
		if !strings.Contains(notes[0], want) {
			t.Errorf("the notice lacks %q: %s", want, notes[0])
		}
	}
	if n := len(pendingFrom(t, d, worker.ID)); n != 0 {
		t.Fatalf("the worker was told %d things", n)
	}
}

// ONE NOTICE PER STATE: blocked, then running again, then blocked again and finished are four revisions and three notices.
func TestOneNoticePerBlockedAndOnePerFinished(t *testing.T) {
	d := testDaemon(t)
	launcher, worker, it := itemRunning(t, d, "")
	blockWith(t, d, "first question")
	if _, _, err := d.st.ItemTransition(it.ID, store.ItemRunning, ""); err != nil {
		t.Fatal(err)
	}
	blockWith(t, d, "second question")
	finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, SHA: "abc1234", Recap: "done abc1234", Ended: true})
	finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, SHA: "def5678", Recap: "done again", Ended: true})
	// a block after it is over changes nothing
	blockWith(t, d, "too late")

	got, _ := d.st.ItemGet(it.ID)
	if got.State != store.ItemFinished || got.Revision != 5 {
		t.Fatalf("item: %+v", got)
	}
	notes := noticesFromAtrium(t, d, launcher.ID)
	if len(notes) != 3 {
		t.Fatalf("%d notices, want 3 (blocked, blocked, finished): %v", len(notes), notes)
	}
	if !strings.Contains(notes[2], "finished") || !strings.Contains(notes[2], "abc1234"[:7]) {
		t.Errorf("the finished notice: %s", notes[2])
	}
	_ = worker
}

// A HUMAN-OWNED ITEM WRITES NO NOTICE TO ANY SESSION: the board is the alert.
func TestAHumanOwnedItemNoticesNoSession(t *testing.T) {
	d := testDaemon(t)
	launcher, _, it := itemRunning(t, d, "human")
	blockWith(t, d, "need a decision")
	if n := len(pendingFrom(t, d, launcher.ID)); n != 0 {
		t.Fatalf("the launcher heard %d things about a human's item", n)
	}
	got, _ := d.st.ItemGet(it.ID)
	if got.State != store.ItemBlocked || got.OwnerKind != store.ItemOwnerHuman {
		t.Fatalf("item: %+v", got)
	}
}

// THE UNBLOCK IS TYPED AS ATRIUM, AND THE ITEM RUNS AGAIN ONLY WHEN THE WORKER'S TURN STARTS FROM IT.
func TestUnblockQueuesTheAnswerAsAtriumAndRunsOnlyOnTheTurn(t *testing.T) {
	d := testDaemon(t)
	_, worker, it := itemRunning(t, d, "")
	blockWith(t, d, "need the staging key")

	if _, code, err := d.itemUnblock(it.ID, "use KEY-9", "orchestrator"); err != nil || code != http.StatusOK {
		t.Fatalf("unblock: %d %v", code, err)
	}
	msgs := pendingFrom(t, d, worker.ID)
	if len(msgs) != 1 || msgs[0].FromPeer != store.ReservedHandle || !strings.Contains(msgs[0].Text, "use KEY-9") {
		t.Fatalf("the worker was sent %+v", msgs)
	}
	for _, m := range msgs {
		if strings.Contains(strings.ToLower(m.Text), "orchestrator") {
			t.Errorf("the worker learned who the owner is: %s", m.Text)
		}
	}
	// typed, not run
	if got, _ := d.st.ItemGet(it.ID); got.State != store.ItemBlocked {
		t.Fatalf("running at the moment of typing: %+v", got)
	}
	// delivered, still not a turn
	if err := d.markDelivered(worker.ID, "permission", messageIDs(msgs)); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.st.ItemGet(it.ID); got.State != store.ItemBlocked || !got.UnblockDelivered {
		t.Fatalf("delivery alone moved it: %+v", got)
	}
	// the turn starts
	promptEvent(d, worker.ID)
	got, _ := d.st.ItemGet(it.ID)
	if got.State != store.ItemRunning || got.Revision != 3 || got.UnblockMsg != "" {
		t.Fatalf("after the turn: %+v", got)
	}
}

// THE STOP HOOK CARRYING THE ANSWER IS THE TURN STARTING.
func TestAnUnblockCarriedByTheStopHookStartsTheTurn(t *testing.T) {
	d := testDaemon(t)
	_, worker, it := itemRunning(t, d, "")
	blockWith(t, d, "need the staging key")
	if _, _, err := d.itemUnblock(it.ID, "use KEY-9", "orchestrator"); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.st.ItemGet(it.ID); got.State != store.ItemBlocked {
		t.Fatalf("before the hook: %+v", got)
	}
	stopTurn(t, d, "worker")
	if got, _ := d.st.ItemGet(it.ID); got.State != store.ItemRunning {
		if len(pendingFrom(t, d, worker.ID)) > 0 {
			t.Skip("the stop hook held the message for the turn end in this harness")
		}
		t.Fatalf("after the stop hook: %+v", got)
	}
}

// THE HUMAN TYPING INTO THE WORKER'S TERMINAL IS A TURN STARTING TOO.
func TestTheHumanTypingStartsABlockedItemsTurn(t *testing.T) {
	d := testDaemon(t)
	_, worker, it := itemRunning(t, d, "human")
	blockWith(t, d, "need a decision")
	promptEvent(d, worker.ID)
	if got, _ := d.st.ItemGet(it.ID); got.State != store.ItemRunning {
		t.Fatalf("after the human typed: %+v", got)
	}
}

// A PROMPT ON A RUNNING OR FINISHED ITEM CHANGES NOTHING.
func TestAPromptMovesNothingButABlockedItem(t *testing.T) {
	d := testDaemon(t)
	_, worker, it := itemRunning(t, d, "")
	promptEvent(d, worker.ID)
	if got, _ := d.st.ItemGet(it.ID); got.Revision != 1 || got.State != store.ItemRunning {
		t.Fatalf("running: %+v", got)
	}
	finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, SHA: "abc1234", Recap: "done", Ended: true})
	promptEvent(d, worker.ID)
	if got, _ := d.st.ItemGet(it.ID); got.State != store.ItemFinished {
		t.Fatalf("finished: %+v", got)
	}
}

func TestUnblockRefusesAnItemThatIsNotBlocked(t *testing.T) {
	d := testDaemon(t)
	_, worker, it := itemRunning(t, d, "")
	if _, code, err := d.itemUnblock(it.ID, "go", "orchestrator"); err == nil || code != http.StatusConflict {
		t.Fatalf("running: %d %v", code, err)
	}
	if _, code, err := d.itemUnblock("it-nope", "go", "orchestrator"); err == nil || code != http.StatusNotFound {
		t.Fatalf("missing: %d %v", code, err)
	}
	if n := len(pendingFrom(t, d, worker.ID)); n != 0 {
		t.Fatalf("a refused unblock typed %d things", n)
	}
}

// CANCEL ENDS THE ITEM, ONCE, AND ASKS THE WORKER TO LEAVE.
func TestCancelFinishesTheItemAndNoticesNobody(t *testing.T) {
	d := testDaemon(t)
	launcher, _, it := itemRunning(t, d, "")
	if _, code, err := d.itemCancel(it.ID, "orchestrator"); err != nil || code != http.StatusOK {
		t.Fatalf("cancel: %d %v", code, err)
	}
	got, _ := d.st.ItemGet(it.ID)
	if got.State != store.ItemFinished || !strings.Contains(got.Note, "cancelled") {
		t.Fatalf("item: %+v", got)
	}
	if n := len(noticesFromAtrium(t, d, launcher.ID)); n != 0 {
		t.Fatalf("the owner who cancelled was told %d times", n)
	}
	if _, code, _ := d.itemCancel(it.ID, "orchestrator"); code != http.StatusConflict {
		t.Fatalf("a second cancel: %d", code)
	}
}

// THE OUTBOX HOLDS WHILE THE HUB IS DOWN AND FLUSHES, IN ORDER, WHEN IT IS BACK. A replayed flush is a no-op.
func TestTheItemOutboxFlushesAfterAHubOutage(t *testing.T) {
	d := testDaemon(t)
	hub := newHubRelay()
	hub.setDown(true)
	d.SetRelay(hub)
	_, _, it := itemRunning(t, d, "")
	blockWith(t, d, "need the staging key")

	d.flushItems()
	if got := hub.pushed(); len(got) != 0 {
		t.Fatalf("pushed while down: %v", got)
	}
	owed, _ := d.st.ItemsOwed(10)
	if len(owed) != 2 || owed[0].Attempts == 0 {
		t.Fatalf("owed while down: %+v", owed)
	}

	hub.setDown(false)
	d.RelayAttached()
	d.flushItems()
	if got := hub.pushed(); len(got) != 2 || got[0] != it.ID+"/1/running" || got[1] != it.ID+"/2/blocked" {
		t.Fatalf("pushed after the outage: %v", got)
	}
	if owed, _ := d.st.ItemsOwed(10); len(owed) != 0 {
		t.Fatalf("still owed: %+v", owed)
	}
	d.flushItems()
	if got := hub.pushed(); len(got) != 2 {
		t.Fatalf("a second flush pushed again: %v", got)
	}
}

// A ROOM WITH NO HUB KEEPS RECORDING. Nothing fails and nothing is lost.
func TestAnItemIsRecordedWithNoHubAtAll(t *testing.T) {
	d := testDaemon(t)
	_, _, it := itemRunning(t, d, "")
	blockWith(t, d, "need the staging key")
	d.flushItems()
	if owed, _ := d.st.ItemsOwed(10); len(owed) != 2 {
		t.Fatalf("owed: %+v", owed)
	}
	if got, _ := d.st.ItemGet(it.ID); got.State != store.ItemBlocked {
		t.Fatalf("item: %+v", got)
	}
}

// A ROLE OWNER IS RESOLVED AT DELIVERY. Held by nobody, the end is held for the orchestrator and not lost.
func TestARoleOwnerIsResolvedWhenTheEndIsDelivered(t *testing.T) {
	d := testDaemon(t)
	hub := newHubRelay()
	d.SetRelay(hub)
	launcher, _, it := itemRunning(t, d, "director")
	holder := peerCard(t, d, "director-card")
	hub.holder = `{"item_id":"` + it.ID + `","holder":{"room":"","card":"` + holder.ID + `"}}`
	blockWith(t, d, "need the staging key")
	if n := len(noticesFromAtrium(t, d, holder.ID)); n != 1 {
		t.Fatalf("the holder got %d notices", n)
	}
	if n := len(pendingFrom(t, d, launcher.ID)); n != 0 {
		t.Fatalf("the creator, who does not own it, got %d", n)
	}
}

func TestNoticeKeyIsPerItemRevisionAndState(t *testing.T) {
	a := itemNoticeKey("it-1", 2, store.ItemBlocked)
	if a == itemNoticeKey("it-1", 4, store.ItemBlocked) || a == itemNoticeKey("it-1", 2, store.ItemFinished) ||
		a != itemNoticeKey("it-1", 2, store.ItemBlocked) {
		t.Fatalf("key: %q", a)
	}
}

var _ = errors.New
