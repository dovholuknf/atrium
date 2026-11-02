//go:build integration

package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/dovholuknf/atrium/internal/daemon/roomconf"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/base/testwait"
)

// healthEvents reads `health` events off the board stream until it has want of
// them or gives up.
func healthEvents(t *testing.T, d *Daemon, want int, trigger func(heard func(n int))) []map[string]any {
	t.Helper()
	ts := httptest.NewServer(d.ap.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/v1/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var mu sync.Mutex
	var out []map[string]any
	pinged := make(chan struct{}, 1)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		kind := ""
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "event: ") {
				kind = strings.TrimPrefix(line, "event: ")
			}
			if strings.HasPrefix(line, "data: ") && kind == "ping" {
				select {
				case pinged <- struct{}{}:
				default:
				}
			}
			if strings.HasPrefix(line, "data: ") && kind == "health" {
				body := map[string]any{}
				_ = json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &body)
				mu.Lock()
				out = append(out, body)
				mu.Unlock()
			}
		}
	}()
	testwait.EventuallyHTTP(t, 5*time.Second, "the event stream to subscribe", func() bool {
		d.ap.Broadcast("ping", nil)
		select {
		case <-pinged:
			return true
		default:
			return false
		}
	})
	heard := func(n int) {
		t.Helper()
		testwait.Eventually(t, 5*time.Second, fmt.Sprintf("%d health events", n), func() bool {
			mu.Lock()
			defer mu.Unlock()
			return len(out) >= n
		})
	}
	trigger(heard)
	heard(want)
	mu.Lock()
	defer mu.Unlock()
	return append([]map[string]any(nil), out[:want]...)
}

// r-017: a storage halt is pushed as `health`, since the stream stays up and the
// board would otherwise only learn of it by polling /v1/health.
func TestAHaltIsPublishedAsHealth(t *testing.T) {
	d := testDaemon(t)
	cause := errors.New("disk full")
	d.hk.HealthHalted = func(roomconf.HaltReader) (bool, error) { return true, cause }

	evs := healthEvents(t, d, 1, func(func(int)) { d.onHalt(cause) })
	if evs[0]["halted"] != true || evs[0]["cause"] != "disk full" {
		t.Fatalf("health after a halt: %v", evs[0])
	}
	if _, ok := evs[0]["settling"]; !ok {
		t.Fatalf("health carries no settling: %v", evs[0])
	}
}

// The settle window is a clock and a pending set, so nothing fires when it
// closes. The watcher says it is open, then says once that it closed.
func TestTheSettleWindowOpeningAndClosingIsPublished(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	d.settle.begin()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	evs := healthEvents(t, d, 2, func(heard func(int)) {
		go d.WatchSettle(ctx)
		heard(1)
		d.settle.done()
	})
	if evs[0]["settling"] != true {
		t.Fatalf("first health should say settling: %v", evs[0])
	}
	if evs[1]["settling"] != false || evs[1]["halted"] != false {
		t.Fatalf("second health should say settled and not halted: %v", evs[1])
	}
}
