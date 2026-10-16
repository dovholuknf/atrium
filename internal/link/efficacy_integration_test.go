//go:build integration

package link

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

// A FRESH TEST HUB, never the live one (section 12.7): the rig is a hub store in a temp dir.

func TestAnEventRecordedTwiceCountsOnceAndShowsInTheItemAndTheRollup(t *testing.T) {
	x := newItemRig(t)
	if code, body := x.push(t, 1, "finished", "human", ""); code != http.StatusOK {
		t.Fatalf("push: %d %s", code, body)
	}
	ev := js(map[string]any{"kind": "not_accepted", "note": "misses the halt", "at": "2026-10-13T09:00:00Z"})
	for i, want := range []bool{true, false} {
		code, body := x.send("POST", "/_hub/work/adhoc-it-1/event", ev, loopAddr)
		if code != http.StatusOK || obj(t, body)["recorded"] != want {
			t.Fatalf("record %d: %d %s", i, code, body)
		}
	}
	if code, _ := x.send("POST", "/_hub/work/adhoc-it-1/event", ev, "203.0.113.9:4000"); code != http.StatusForbidden {
		t.Fatalf("from elsewhere: %d", code)
	}
	if code, _ := x.send("POST", "/_hub/work/adhoc-it-1/event", js(map[string]any{"kind": "revert"}), loopAddr); code != http.StatusBadRequest {
		t.Fatalf("atrium sees reverts for itself: %d", code)
	}
	if err := x.p.CollectEfficacy(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	evs, _ := x.st.WorkEvents("adhoc-it-1")
	if len(evs["adhoc-it-1"]) != 1 || evs["adhoc-it-1"][0].Source != hubstore.EvReported {
		t.Fatalf("%+v", evs)
	}
	// Collecting again changes nothing: the events are keyed, the outcome is derived again.
	if err := x.p.CollectEfficacy(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if evs, _ = x.st.WorkEvents("adhoc-it-1"); len(evs["adhoc-it-1"]) != 1 {
		t.Fatalf("%+v", evs)
	}
	code, body := x.send("GET", "/_hub/work/adhoc-it-1", "", "203.0.113.9:4000")
	if code != http.StatusOK || obj(t, body)["efficacy"] == nil {
		t.Fatalf("the item has no efficacy: %d %s", code, body)
	}
	code, body = x.send("GET", "/_hub/work/efficacy?by=week", "", "203.0.113.9:4000")
	if code != http.StatusOK {
		t.Fatalf("efficacy: %d %s", code, body)
	}
	o := obj(t, body)
	if o["by"] != "week" || o["estimated"] != true || len(o["periods"].([]any)) != 8 {
		t.Fatalf("%s", body)
	}
	if code, _ := x.send("POST", "/_hub/work/efficacy", "{}", loopAddr); code == http.StatusOK {
		t.Fatal("efficacy is a read")
	}
}
