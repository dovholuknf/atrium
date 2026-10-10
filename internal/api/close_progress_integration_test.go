//go:build integration

package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// closeEvents collects the close-progress events of one close id until the channel is quiet.
func closeEvents(ch chan message) []map[string]any {
	var out []map[string]any
	for {
		select {
		case m := <-ch:
			if m.kind != closeProgressEvent {
				continue
			}
			var ev map[string]any
			_ = json.Unmarshal(m.data, &ev)
			out = append(out, ev)
		case <-time.After(200 * time.Millisecond):
			return out
		}
	}
}

func TestCloseSaysEachStepInItsOwnOrderAndTicksThemOff(t *testing.T) {
	t.Parallel()
	oh, card, wt, _ := openForClose(t)
	ch := oh.srv.bus.subscribe()
	defer oh.srv.bus.unsubscribe(ch)
	code, out := closeCall(t, oh, card, map[string]any{"confirm": true, "progress": "close-1"})
	if code != 200 || !out.Closed {
		t.Fatalf("%d %+v", code, out)
	}
	if exists(wt) {
		t.Error("the worktree is still on disk")
	}
	var order []string
	state := map[string]string{}
	for _, ev := range closeEvents(ch) {
		if ev["id"] != "close-1" {
			t.Errorf("an event carries the id %v", ev["id"])
		}
		key, _ := ev["key"].(string)
		st, _ := ev["state"].(string)
		if st == "run" {
			order = append(order, key)
		}
		state[key] = st
	}
	// The runner first, the review before the worktree, the worktree before its branch, the card last.
	want := []string{"session", "review", "worktree:" + strconv.Itoa(worktreeSeq(t, oh.srv.st, card))}
	at := -1
	for _, w := range want {
		found := -1
		for i, k := range order {
			if k == w && i > at {
				found = i
				break
			}
		}
		if found < 0 {
			t.Fatalf("step %q is not in order in %v", w, order)
		}
		at = found
	}
	if len(order) == 0 || order[len(order)-1] != "card" {
		t.Errorf("the card is not the last step: %v", order)
	}
	for k, st := range state {
		if st != "ok" {
			t.Errorf("step %s ended %s", k, st)
		}
	}
}

func TestClosePreviewSizesAWorktreeWithItsIgnoredTreesAndAsksWithTheSize(t *testing.T) {
	t.Parallel()
	oh, card, wt, _ := openForClose(t)
	dir := filepath.Join(filepath.FromSlash(wt), "node_modules", "big")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "blob.bin"), make([]byte, 3<<20), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := httpDo(t, oh.h, "GET", "/v1/tasks/"+card+"/close")
	var pv closePreview
	_ = json.Unmarshal(rec.Body.Bytes(), &pv)
	if pv.DiskBytes < 3<<20 {
		t.Errorf("the preview frees %d bytes and leaves out the 3 MB tree", pv.DiskBytes)
	}
	var sized bool
	for _, it := range pv.Items {
		if it.Kind == "worktree" && it.Bytes >= 3<<20 {
			sized = true
		}
	}
	if !sized {
		t.Errorf("no worktree row carries its size: %+v", pv.Items)
	}
	for _, a := range pv.Asks {
		if a.Bytes < 3<<20 {
			t.Errorf("the ask carries %d bytes", a.Bytes)
		}
	}
}
