//go:build integration

package link

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/base/tokenprice"
)

// usageRoom is a room's two ndjson routes, as lines.
type usageRoom struct {
	usage, deliveries []map[string]any
	asked             []string
	broken            bool
}

func (f *usageRoom) lines(_ context.Context, _, path string, each func([]byte) error) error {
	f.asked = append(f.asked, path)
	if f.broken {
		return fmt.Errorf("the room answered 503")
	}
	src := f.usage
	if strings.HasPrefix(path, "/v1/deliveries/export") {
		src = f.deliveries
	}
	for _, r := range src {
		b, _ := json.Marshal(r)
		if err := each(b); err != nil {
			return err
		}
	}
	return nil
}

func (x *itemRig) attach(t *testing.T, id, card, step string) {
	t.Helper()
	code, body := x.send("POST", "/_hub/items/sync", js(map[string]any{"item_id": "it-" + card, "room": "sg4", "card": card,
		"generation": 1, "state": "running", "revision": 1, "owner_kind": "human", "created_by": "orch", "work_id": id, "step": step}), loopAddr)
	if code != http.StatusOK {
		t.Fatalf("attach: %d %s", code, body)
	}
}

func usageLine(id, card, model string, from, to time.Time, in, out int64) map[string]any {
	return map[string]any{"id": id, "task_id": card, "started_at": from.Format(time.RFC3339Nano), "ended_at": to.Format(time.RFC3339Nano),
		"model": model, "input": in, "output": out, "cache_write_5m": 100, "cache_write_1h": 10, "cache_read": 5000}
}

func (x *itemRig) cost(t *testing.T, id string) map[string]any {
	t.Helper()
	code, body := x.send("GET", "/_hub/work/"+id+"/cost", "", "203.0.113.9:4000")
	if code != http.StatusOK {
		t.Fatalf("cost: %d %s", code, body)
	}
	return obj(t, body)["cost"].(map[string]any)
}

func TestAPullKeepsAnItemsRowsOnceAndAReplayAddsNothing(t *testing.T) {
	x := newItemRig(t)
	x.attach(t, "f-x", "b1", "building")
	room := &usageRoom{}
	x.p.roomLines = room.lines
	now := time.Now().UTC()
	room.usage = []map[string]any{
		usageLine("u1", "b1", "claude-opus-5-5", now.Add(time.Minute), now.Add(2*time.Minute), 1000, 200),
		usageLine("u2", "b1", "claude-opus-5-5", now.Add(3*time.Minute), now.Add(4*time.Minute), 3000, 400),
		usageLine("u3", "stranger", "claude-opus-5-5", now.Add(3*time.Minute), now.Add(4*time.Minute), 9, 9),
		usageLine("old", "b1", "claude-opus-5-5", now.Add(-5*time.Hour), now.Add(-4*time.Hour), 7, 7),
	}
	s, err := x.p.PullRoomUsage(context.Background(), "sg4", now.Add(time.Hour))
	if err != nil || s.Added != 2 || s.Kept != 2 {
		t.Fatalf("first pull: %+v %v", s, err)
	}
	before := x.cost(t, "f-x")
	s, err = x.p.PullRoomUsage(context.Background(), "sg4", now.Add(2*time.Hour))
	if err != nil || s.Added != 0 {
		t.Fatalf("a replay added %d: %+v %v", s.Added, s, err)
	}
	after := x.cost(t, "f-x")
	if fmt.Sprint(before["total"]) != fmt.Sprint(after["total"]) {
		t.Fatalf("a replayed pull moved the total: %v -> %v", before["total"], after["total"])
	}
	total := after["total"].(map[string]any)
	if total["total_tokens"].(float64) != float64(1000+200+3000+400+2*(100+10+5000)) {
		t.Fatalf("tokens = %v", total["total_tokens"])
	}
	if after["estimated"] != true || after["prices"] != "prices-2026-09-28" || after["headline_suffix"] != "+ coordination" {
		t.Fatalf("the figure must say what it is: %v", after)
	}
	// Opus: 4000 in, 600 out, 200 w5m, 20 w1h, 10000 read.
	want := tokenprice.FormatCents(tokenprice.Price{Input: 4, CacheWrite5m: 5, CacheWrite1h: 8, CacheRead: 0.2, Output: 20}.
		Cost(tokenprice.Tokens{Input: 4000, Output: 600, CacheWrite5m: 200, CacheWrite1h: 20, CacheRead: 10000}).Cents())
	if got := after["headline"].(map[string]any)["display"]; got != want {
		t.Fatalf("headline %v, want %s", got, want)
	}
}

func TestAFailedPullLeavesTheCursorAndANewCardPullsItsEarlierRows(t *testing.T) {
	x := newItemRig(t)
	x.attach(t, "f-x", "b1", "building")
	room := &usageRoom{}
	x.p.roomLines = room.lines
	now := time.Now().UTC()
	room.usage = []map[string]any{usageLine("u1", "b1", "claude-sonnet-5-5", now.Add(time.Minute), now.Add(2*time.Minute), 10, 1)}
	room.broken = true
	if _, err := x.p.PullRoomUsage(context.Background(), "sg4", now.Add(time.Hour)); err == nil {
		t.Fatal("a room that does not answer is an error the tick logs")
	}
	if _, _, have, _ := x.st.UsageCursor("sg4"); have {
		t.Fatal("a failed pull moved the cursor")
	}
	room.broken = false
	if s, _ := x.p.PullRoomUsage(context.Background(), "sg4", now.Add(time.Hour)); s.Added != 1 {
		t.Fatalf("after the room came back: %+v", s)
	}
	// A second card, attached later, has a row from before the cursor.
	room.usage = append(room.usage, usageLine("u9", "r1", "claude-sonnet-5-5", now.Add(3*time.Minute), now.Add(4*time.Minute), 20, 2))
	x.attach(t, "f-x", "r1", "reviewed")
	if s, _ := x.p.PullRoomUsage(context.Background(), "sg4", now.Add(2*time.Hour)); s.Added != 1 {
		t.Fatalf("the new card's row: %+v", s)
	}
}

func TestAModelWithNoPriceShowsAQuestionMarkOnTheRoute(t *testing.T) {
	x := newItemRig(t)
	x.attach(t, "f-x", "b1", "building")
	room := &usageRoom{}
	x.p.roomLines = room.lines
	now := time.Now().UTC()
	room.usage = []map[string]any{usageLine("u1", "b1", "claude-brand-new-1", now.Add(time.Minute), now.Add(2*time.Minute), 10, 1)}
	x.p.PullRoomUsage(context.Background(), "sg4", now.Add(time.Hour))
	c := x.cost(t, "f-x")
	h := c["headline"].(map[string]any)
	if h["display"] != "?" || h["known"] != false || h["unpriced_tokens"].(float64) == 0 {
		t.Fatalf("headline = %v", h)
	}
	if h["total_tokens"].(float64) == 0 {
		t.Fatal("the tokens are never hidden behind the question mark")
	}
}

// THE ORCHESTRATOR'S ANSWERS ARE COORDINATION, divided equally among the items a turn was about, with no token lost, and
// a turn for a card that is itself on an item is not counted twice.
func TestCoordinationComesFromDeliveriesAndSplitsAcrossItems(t *testing.T) {
	x := newItemRig(t)
	x.attach(t, "f-a", "ca", "building")
	x.attach(t, "f-b", "cb", "building")
	room := &usageRoom{}
	x.p.roomLines = room.lines
	now := time.Now().UTC()
	d := func(id, to, from, group string, tok int64) map[string]any {
		return map[string]any{"id": id, "task_id": to, "from_task": from, "kind": "report", "at": now.Add(time.Minute).Format(time.RFC3339Nano),
			"costed_at": now.Add(2 * time.Minute).Format(time.RFC3339Nano), "group": group, "model": "claude-opus-5-5",
			"input": tok, "output": 1, "cache_write_5m": 0, "cache_write_1h": 0, "cache_read": 0}
	}
	room.deliveries = []map[string]any{
		// one turn of the orchestrator, answering a report from each item: 3 tokens in, divided between two items.
		d("d1", "orch", "ca", "", 3), d("d2", "orch", "cb", "d1", 0),
		// a delivery to a card that is on an item is that card's own turn, counted in its usage row.
		d("d3", "ca", "cb", "", 500),
		// a turn nobody on an item caused is overhead.
		d("d4", "orch", "elsewhere", "", 800),
	}
	if s, err := x.p.PullRoomUsage(context.Background(), "sg4", now.Add(time.Hour)); err != nil || s.Added != 2 {
		t.Fatalf("pull: %+v %v", s, err)
	}
	a, b := x.cost(t, "f-a")["coordination"].(map[string]any), x.cost(t, "f-b")["coordination"].(map[string]any)
	ta, tb := a["tokens"].(map[string]any), b["tokens"].(map[string]any)
	if ta["input"].(float64)+tb["input"].(float64) != 3 || ta["output"].(float64)+tb["output"].(float64) != 1 {
		t.Fatalf("a turn about two items lost a token: %v %v", ta, tb)
	}
	if ta["input"].(float64) == 0 || tb["input"].(float64) == 0 {
		t.Fatalf("each item takes a share: %v %v", ta, tb)
	}
	// A replay adds nothing, even though the deliveries are read again.
	if s, _ := x.p.PullRoomUsage(context.Background(), "sg4", now.Add(2*time.Hour)); s.Added != 0 {
		t.Fatalf("replay added %d", s.Added)
	}
	// The headline is the steps alone: nothing here is a step.
	if h := x.cost(t, "f-a")["headline"].(map[string]any); h["display"] != "-" {
		t.Fatalf("headline = %v", h)
	}
}

func TestThePriceRoute(t *testing.T) {
	x := newItemRig(t)
	code, body := x.send("GET", "/_hub/prices", "", "203.0.113.9:4000")
	if code != http.StatusOK || !strings.Contains(body, "claude-opus-5-5") || !strings.Contains(body, "claude-fable-5-1") ||
		!strings.Contains(body, `"version":"prices-2026-09-28"`) {
		t.Fatalf("prices: %d %s", code, body)
	}
	if code, body = x.send("GET", "/_hub/work/cost", "", "203.0.113.9:4000"); code != http.StatusOK || !strings.Contains(body, `"items"`) {
		t.Fatalf("all costs: %d %s", code, body)
	}
}
