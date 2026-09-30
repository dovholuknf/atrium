package link

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// pruneRoom answers a prune with how many it removed, and records the bodies.
type pruneRoom struct {
	mu      sync.Mutex
	got     []string
	removed int
	hang    chan struct{}
}

func (p *pruneRoom) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v1/tasks/prune" {
		io.WriteString(w, `{}`)
		return
	}
	b, _ := io.ReadAll(r.Body)
	p.mu.Lock()
	p.got = append(p.got, string(b))
	p.mu.Unlock()
	if p.hang != nil {
		select {
		case <-p.hang:
		case <-r.Context().Done():
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int{"removed": p.removed})
}

func (p *pruneRoom) bodies() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.got...)
}

func postPrune(t *testing.T, front, body, room string) (int, map[string]any, time.Duration) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, front+"/v1/tasks/prune", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if room != "" {
		req.Header.Set("X-Atrium-Room", room)
	}
	start := time.Now()
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	took := time.Since(start)
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out, took
}

// A PRUNE FROM THE ALL VIEW CLEARS THE COLUMN ON EVERY ROOM (item 92), with the
// body passed through whole and the counts added up.
func TestAPruneWithNoRoomNamedReachesEveryRoom(t *testing.T) {
	a, b := &pruneRoom{removed: 2}, &pruneRoom{removed: 3}
	front, _, done := two(t, a, b)
	defer done()

	code, out, _ := postPrune(t, front.URL, `{"statuses":["done"]}`, "")
	if code != http.StatusOK {
		t.Fatalf("answered %d %v", code, out)
	}
	if out["removed"] != float64(5) {
		t.Errorf("removed = %v, want the 5 both rooms removed", out["removed"])
	}
	for name, r := range map[string]*pruneRoom{"alpha": a, "beta": b} {
		if got := r.bodies(); len(got) != 1 || got[0] != `{"statuses":["done"]}` {
			t.Errorf("room %s was sent %v", name, got)
		}
	}
}

// A PRUNE FROM A VIEW SCOPED TO ONE ROOM CLEARS THAT ROOM ONLY. It deletes cards,
// and that view confirmed a count from its own room.
func TestAPruneWithARoomNamedStaysOnThatRoom(t *testing.T) {
	a, b := &pruneRoom{removed: 2}, &pruneRoom{removed: 3}
	front, _, done := two(t, a, b)
	defer done()

	if code, out, _ := postPrune(t, front.URL, `{"statuses":["dead"]}`, "beta"); code != http.StatusOK {
		t.Fatalf("answered %d %v", code, out)
	}
	if len(a.bodies()) != 0 {
		t.Errorf("a prune scoped to beta reached alpha: %v", a.bodies())
	}
	if len(b.bodies()) != 1 {
		t.Errorf("beta was sent %d prunes, want 1", len(b.bodies()))
	}
}

// A room that never answers neither fails nor stalls the prune.
func TestAHungRoomDoesNotStallThePrune(t *testing.T) {
	ok, hung := &pruneRoom{removed: 1}, &pruneRoom{hang: make(chan struct{})}
	defer close(hung.hang)
	front, _, done := two(t, ok, hung)
	defer done()

	code, out, took := postPrune(t, front.URL, `{}`, "")
	if code != http.StatusOK {
		t.Fatalf("answered %d with one room taking it", code)
	}
	if took > pruneBound+2*time.Second {
		t.Errorf("reply took %v, bound is %v", took, pruneBound)
	}
	un, _ := out["unreached"].([]any)
	if len(un) != 1 || un[0].(map[string]any)["room"] != "beta" {
		t.Errorf("unreached = %v, wanted beta", out["unreached"])
	}
	if out["removed"] != float64(1) {
		t.Errorf("removed = %v, want 1", out["removed"])
	}
}
