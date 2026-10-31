//go:build integration

package daemon

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// The strip under a terminal follows the card's own task frames (r-new-context-strip-stuck-at-1of3). A cycle that
// runs to its end must show 1, 2 and 3 on the way and then publish a frame with no new_context, or the board keeps
// "1/3: waiting for atrium ready" and "let me type" for good.
func TestNewContextStepsAdvanceAndTheEndFrameCarriesNoChip(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d := testDaemon(t)
	fastNewContext(t, d)
	d.hk.NcTiming.SessionSettle = 500 * time.Millisecond
	task, f, _ := ncCard(t, d)
	id := task.ID

	ts := httptest.NewServer(d.ap.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/v1/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var mu sync.Mutex
	var frames []map[string]any
	go func() {
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		kind := ""
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "event: ") {
				kind = strings.TrimPrefix(line, "event: ")
			}
			if strings.HasPrefix(line, "data: ") && kind == "task" {
				var row map[string]any
				if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &row) == nil && row["id"] == id {
					mu.Lock()
					frames = append(frames, row)
					mu.Unlock()
				}
			}
		}
	}()
	time.Sleep(100 * time.Millisecond) // wait: let the frame recorder start, no event marks it

	stepIs := func(n int, label string) func() bool {
		return func() bool {
			v, _ := d.NewContextFor(id).(map[string]any)
			return v != nil && v["n"] == n && strings.HasPrefix(v["label"].(string), label)
		}
	}
	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "step 1", stepIs(1, "waiting for atrium ready"))
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), limitPrompt) })
	ncAck(t, d, task)
	until(t, "step 2", stepIs(2, "clearing the context"))
	until(t, "/clear", func() bool { return strings.Contains(f.written(), "/clear") })
	d.Wake.SawSession(id, time.Now())
	until(t, "step 3", stepIs(3, "waking it to read"))
	until(t, "the chip to go", func() bool { return d.NewContextFor(id) == nil })

	var last map[string]any
	until(t, "a task frame after the chip went", func() bool {
		mu.Lock()
		defer mu.Unlock()
		if len(frames) == 0 {
			return false
		}
		last = frames[len(frames)-1]
		return last["new_context"] == nil
	})
	mu.Lock()
	defer mu.Unlock()
	saw := false
	for _, fr := range frames {
		if fr["new_context"] != nil {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("no frame carried the cycle while it ran: %v", frames)
	}
}
