//go:build integration

package link

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

type itemRig struct {
	*backlogRig
	st    *hubstore.Store
	calls []string
	reply int
}

func newItemRig(t *testing.T) *itemRig {
	t.Helper()
	b := newBacklogRig(t)
	st := b.p.backlogStore()
	b.p.SetItems(st)
	x := &itemRig{backlogRig: b, st: st, reply: http.StatusOK}
	// THE ROOM IS A FUNCTION HERE: what the hub would post to it, recorded.
	b.p.itemRoomCall = func(_ context.Context, room, path string, body []byte) (int, []byte, error) {
		x.calls = append(x.calls, room+" "+path+" "+string(body))
		return x.reply, []byte(`{"ok":true}`), nil
	}
	return x
}

func (x *itemRig) push(t *testing.T, rev int, state, ownerKind, owner string) (int, string) {
	t.Helper()
	return x.send("POST", "/_hub/items/sync", js(map[string]any{"item_id": "it-1", "room": "room-e", "card": "c1",
		"generation": 1, "state": state, "revision": rev, "owner_kind": ownerKind, "owner": owner,
		"created_by": owner, "note": "n"}), loopAddr)
}

func TestItemSyncIsIdempotentAndQualifiesASessionOwner(t *testing.T) {
	x := newItemRig(t)
	code, body := x.push(t, 1, "running", "session", "orch")
	if code != http.StatusOK || obj(t, body)["applied"] != true {
		t.Fatalf("first: %d %s", code, body)
	}
	if got := str(obj(t, body), "item", "owner"); got != "orch@room-e" {
		t.Fatalf("a bare owner handle was not given its room: %q", got)
	}
	code, body = x.push(t, 1, "running", "session", "orch")
	if code != http.StatusOK || obj(t, body)["applied"] != false {
		t.Fatalf("replay: %d %s", code, body)
	}
	if code, _ = x.push(t, 2, "boom", "session", "orch"); code != http.StatusBadRequest {
		t.Fatalf("a state that is not one: %d", code)
	}
	// a read needs no machine
	code, body = x.send("GET", "/_hub/items?owner_kind=session&state=running", "", "203.0.113.9:4000")
	if code != http.StatusOK || !strings.Contains(body, `"it-1"`) {
		t.Fatalf("list: %d %s", code, body)
	}
	if code, _ = x.send("POST", "/_hub/items/sync", "{}", "203.0.113.9:4000"); code != http.StatusForbidden {
		t.Fatalf("a write from elsewhere: %d", code)
	}
}

func TestItemSyncRefusesATesterOwner(t *testing.T) {
	x := newItemRig(t)
	if code, body := x.push(t, 1, "running", "role", "tester"); code != http.StatusBadRequest ||
		!strings.Contains(body, "several holders") {
		t.Fatalf("tester: %d %s", code, body)
	}
}

func TestOnlyTheOwnerUnblocksAndOnlyABlockedItem(t *testing.T) {
	x := newItemRig(t)
	x.push(t, 1, "running", "session", "orch")
	do := func(by, doWhat, answer string) (int, string) {
		return x.send("POST", "/_hub/items/it-1", js(map[string]any{"do": doWhat, "by": by, "answer": answer}), loopAddr)
	}
	// running: nothing to unblock
	if code, _ := do("orch@room-e", "unblock", "use the key"); code != http.StatusConflict {
		t.Fatalf("a running item: %d", code)
	}
	x.push(t, 2, "blocked", "session", "orch")
	if code, _ := do("someone@room-e", "unblock", "use the key"); code != http.StatusForbidden {
		t.Fatalf("a stranger: %d", code)
	}
	if code, _ := do("orch@room-d", "unblock", "use the key"); code != http.StatusForbidden {
		t.Fatalf("the same name on another room: %d", code)
	}
	if code, _ := do("orch@room-e", "unblock", " "); code != http.StatusBadRequest {
		t.Fatalf("no answer: %d", code)
	}
	if len(x.calls) != 0 {
		t.Fatalf("a refused unblock reached the room: %v", x.calls)
	}
	code, body := do("ORCH@room-e", "unblock", "use the key")
	if code != http.StatusOK {
		t.Fatalf("the owner: %d %s", code, body)
	}
	if len(x.calls) != 1 || !strings.HasPrefix(x.calls[0], "room-e /v1/items/it-1/unblock ") ||
		!strings.Contains(x.calls[0], "use the key") {
		t.Fatalf("the room was asked: %v", x.calls)
	}
	// the hub's row is still blocked: only the room's turn start moves it
	if it, _ := x.st.ItemByID("it-1"); it.State != "blocked" {
		t.Fatalf("the hub moved the item itself: %+v", it)
	}
}

// A HUMAN-OWNED ITEM HAS NO UNBLOCK ON THE BOARD OR ANYWHERE ELSE THE HUB SERVES.
func TestAHumanOwnedItemCannotBeUnblockedThroughTheHub(t *testing.T) {
	x := newItemRig(t)
	x.push(t, 1, "running", "human", "")
	x.push(t, 2, "blocked", "human", "")
	for _, by := range []string{"", "orch@room-e"} {
		code, _ := x.send("POST", "/_hub/items/it-1", js(map[string]any{"do": "unblock", "by": by, "answer": "go"}), loopAddr)
		if code != http.StatusForbidden {
			t.Fatalf("by %q: %d", by, code)
		}
	}
	if len(x.calls) != 0 {
		t.Fatalf("the room was asked: %v", x.calls)
	}
	// The board is the human, from the hub's machine only.
	body := js(map[string]any{"do": "unblock", "by": "board", "answer": "go"})
	if code, _ := x.send("POST", "/_hub/items/it-1", body, "203.0.113.9:4000"); code != http.StatusForbidden {
		t.Fatalf("board from afar: %d", code)
	}
	if code, _ := x.send("POST", "/_hub/items/it-1", body, loopAddr); code != 200 || len(x.calls) != 1 {
		t.Fatalf("board: %d %v", code, x.calls)
	}
}

func TestGetAnItemCarriesItsHistory(t *testing.T) {
	x := newItemRig(t)
	x.push(t, 1, "running", "human", "")
	x.push(t, 2, "blocked", "human", "")
	_, body := x.send("GET", "/_hub/items/it-1", "", loopAddr)
	h, _ := obj(t, body)["history"].([]any)
	if len(h) != 2 {
		t.Fatalf("history: %s", body)
	}
}

func TestCancelGoesToTheRoomForTheOwner(t *testing.T) {
	x := newItemRig(t)
	x.push(t, 1, "running", "session", "orch")
	if code, _ := x.send("POST", "/_hub/items/it-1", js(map[string]any{"do": "cancel", "by": "orch@room-e"}), loopAddr); code != 200 {
		t.Fatalf("cancel: %d", code)
	}
	if len(x.calls) != 1 || !strings.Contains(x.calls[0], "/v1/items/it-1/cancel") {
		t.Fatalf("calls: %v", x.calls)
	}
}

func TestGetAnItemNamesTheCurrentHolderOfARoleOwner(t *testing.T) {
	x := newItemRig(t)
	x.push(t, 1, "running", "role", "director")
	_, body := x.send("GET", "/_hub/items/it-1", "", loopAddr)
	if obj(t, body)["holder"] != nil {
		t.Fatalf("a holder from nowhere: %s", body)
	}
	x.st.RoleSet(hubstore.RoleDirector, "room-e", "c7", "operator")
	_, body = x.send("GET", "/_hub/items/it-1", "", loopAddr)
	if str(obj(t, body), "holder", "room") != "room-e" || str(obj(t, body), "holder", "card") != "c7" {
		t.Fatalf("holder: %s", body)
	}
	if code, _ := x.send("GET", "/_hub/items/it-9", "", loopAddr); code != http.StatusNotFound {
		t.Fatalf("missing: %d", code)
	}
}

func TestHubAskCarriesTheItemEndpointsAndNothingMore(t *testing.T) {
	for p, want := range map[string]bool{
		"/_hub/items":          true,
		"/_hub/items?state=x":  true,
		"/_hub/items/it-1":     true,
		"/_hub/items/sync":     true,
		"/_hub/itemsx":         false,
		"/_hub/items/../rooms": false,
		"/_hub/items//it-1":    false,
	} {
		if got := hubAskPath(p); got != want {
			t.Errorf("hubAskPath(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestSubmitCarriesAnItemToThePoolsRoom(t *testing.T) {
	x := newItemRig(t)
	submit := func(in map[string]any, from string) (int, string) {
		in["do"] = "submit"
		return x.send("POST", "/_hub/items", js(in), from)
	}
	code, _ := submit(map[string]any{"pool": "reviewer", "brief": "review it", "by": "orch@room-e"}, loopAddr)
	if code != http.StatusOK || len(x.calls) != 1 ||
		!strings.HasPrefix(x.calls[0], `room-e /v1/pools/reviewer/items {`) || !strings.Contains(x.calls[0], `"by":"orch"`) {
		t.Fatalf("a submit goes to the pool's room with the bare handle: %d %v", code, x.calls)
	}
	// the owner has to be on the pool's room
	if code, body := submit(map[string]any{"pool": "reviewer", "brief": "x", "room": "room-d", "by": "orch@room-e"}, loopAddr); code != http.StatusBadRequest ||
		!strings.Contains(body, "pool's room") {
		t.Fatalf("an owner on another room: %d %s", code, body)
	}
	if code, _ := submit(map[string]any{"pool": "reviewer", "by": "orch@room-e"}, loopAddr); code != http.StatusBadRequest {
		t.Fatalf("no brief: %d", code)
	}
	if code, _ := submit(map[string]any{"pool": "reviewer", "brief": "x", "by": "orch@room-e"}, "203.0.113.9:4000"); code != http.StatusForbidden {
		t.Fatalf("from elsewhere: %d", code)
	}
}

// A WORKER ON A BACKLOG ITEM THAT ENDS WITH atrium_done files its done text and sha on the item, which leaves open. A
// cancel carries no result and moves nothing.
func TestAFinishedAssignmentFilesItsResultOnTheBacklogItem(t *testing.T) {
	x := newItemRig(t)
	const sha = "abcdef1234567890abcdef1234567890abcdef12"
	for _, id := range []string{"f-r", "f-c"} {
		if code, body := x.send("POST", "/_hub/backlog", js(map[string]any{"id": id, "dept": "fabric", "title": "t"}), loopAddr); code != http.StatusCreated {
			t.Fatalf("file %s: %d %s", id, code, body)
		}
	}
	sync := func(item, card, work string, rev int, state, note, result string) {
		t.Helper()
		if code, body := x.send("POST", "/_hub/items/sync", js(map[string]any{"item_id": item, "room": "room-e", "card": card,
			"generation": 1, "state": state, "revision": rev, "owner_kind": "human", "created_by": "orch", "note": note,
			"work_id": work, "result": result}), loopAddr); code != http.StatusOK {
			t.Fatalf("sync %s r%d: %d %s", item, rev, code, body)
		}
	}
	sync("it-r", "c1", "f-r", 1, "running", "", "")
	sync("it-r", "c1", "f-r", 2, "finished", sha, "done "+sha+": the fix")
	sync("it-c", "c2", "f-c", 1, "running", "", "")
	sync("it-c", "c2", "f-c", 2, "finished", "cancelled by owner", "")
	x.p.WaitWork(10 * time.Second)
	_, body := x.send("GET", "/_hub/backlog/f-r", "", loopAddr)
	o := obj(t, body)
	if str(o, "status") != "built" || str(o, "result") != "done "+sha+": the fix" || str(o, "result_ref") != sha ||
		str(o, "result_card") != "room-e~c1" {
		t.Fatalf("f-r = %s", body)
	}
	if _, body = x.send("GET", "/_hub/backlog/f-c", "", loopAddr); str(obj(t, body), "status") != "open" ||
		strings.Contains(body, `"result"`) {
		t.Fatalf("a cancel moved f-c: %s", body)
	}
}
