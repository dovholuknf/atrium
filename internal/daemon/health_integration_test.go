//go:build integration

package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// healthEvents reads `health` events off the board stream until it has want of
// them or gives up.
func healthEvents(t *testing.T, d *Daemon, want int, trigger func()) []map[string]any {
	t.Helper()
	ts := httptest.NewServer(d.ap.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/v1/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	time.Sleep(100 * time.Millisecond)
	trigger()

	got := make(chan map[string]any, 8)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		kind := ""
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "event: ") {
				kind = strings.TrimPrefix(line, "event: ")
			}
			if strings.HasPrefix(line, "data: ") && kind == "health" {
				body := map[string]any{}
				_ = json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &body)
				got <- body
			}
		}
	}()
	var out []map[string]any
	deadline := time.After(5 * time.Second)
	for len(out) < want {
		select {
		case b := <-got:
			out = append(out, b)
		case <-deadline:
			t.Fatalf("got %d health events, want %d: %v", len(out), want, out)
		}
	}
	return out
}

// r-017: a storage halt is pushed as `health`, since the stream stays up and the
// board would otherwise only learn of it by polling /v1/health.
func TestAHaltIsPublishedAsHealth(t *testing.T) {
	d := testDaemon(t)
	was := healthHalted
	defer func() { healthHalted = was }()
	cause := errors.New("disk full")
	healthHalted = func(*Daemon) (bool, error) { return true, cause }

	evs := healthEvents(t, d, 1, func() { d.onHalt(cause) })
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
	d := testDaemon(t)
	d.settle.begin()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	evs := healthEvents(t, d, 2, func() {
		go d.watchSettle(ctx)
		// Close the window: nothing pending and past the floor.
		time.Sleep(200 * time.Millisecond)
		d.settle.done()
	})
	if evs[0]["settling"] != true {
		t.Fatalf("first health should say settling: %v", evs[0])
	}
	if evs[1]["settling"] != false || evs[1]["halted"] != false {
		t.Fatalf("second health should say settled and not halted: %v", evs[1])
	}
}
