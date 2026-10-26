//go:build integration

package conn

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// A room on ziti, with direct also up. Turning ziti off leaves the link attached over direct, nothing dials the ziti
// side again, and turning it on lets the room climb back. The two transports are plain TCP, as in
// transportorder_integration_test.go: what is under test is the switch.
func TestTurningZitiOffKeepsTheLinkOnDirectAndOnResumes(t *testing.T) {
	lightSlot(t)
	lnD, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	lnZ, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hub := NewHub(Timings{Beat: 100 * time.Millisecond, Silence: time.Second, Warm: 1})
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go func() { _ = hub.ServeVia(ctx, lnD, "direct") }()
	go func() { _ = hub.ServeVia(ctx, lnZ, "ziti") }()

	direct := &switchable{addr: lnD.Addr().String()}
	ziti := &switchable{addr: lnZ.Addr().String()}
	ladder := NewLadder([]Rung{
		{Path: Path{Transport: "ziti", Service: "atrium-hub"}, Dial: ziti},
		{Path: Path{Transport: "direct", Addr: lnD.Addr().String()}, Dial: direct},
	})
	var mu sync.Mutex
	var saved []string
	room := &Room{
		Name: "m1mini", Dial: ladder,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }),
		T: Timings{Beat: 100 * time.Millisecond, Warm: 1, Backoff: 20 * time.Millisecond,
			ProbeEvery: 50 * time.Millisecond, PathsEvery: 50 * time.Millisecond},
		PersistOff: func(off []string) error {
			mu.Lock()
			saved = off
			mu.Unlock()
			return nil
		},
	}
	go func() { _ = room.Run(ctx) }()

	waitFor(t, 5*time.Second, func() bool { a, ok := attachedAs(hub, "m1mini"); return ok && a.Via == "ziti" })
	waitFor(t, 5*time.Second, func() bool {
		for _, p := range room.pathsNow() {
			if p.Kind == "direct" && p.State == PathUp {
				return true
			}
		}
		return false
	})

	if err := room.SetTransport(ctx, "ziti", false); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, func() bool { a, ok := attachedAs(hub, "m1mini"); return ok && a.Via == "direct" })
	mu.Lock()
	if len(saved) != 1 || saved[0] != "ziti" {
		t.Fatalf("the choice written down is %v, want [ziti]", saved)
	}
	mu.Unlock()

	// No further dial of ziti, not by the climb, not by the probe, not by a data dial.
	n := ziti.n()
	time.Sleep(500 * time.Millisecond)
	if got := ziti.n(); got != n {
		t.Fatalf("ziti was dialled %d more times while turned off", got-n)
	}
	if a, ok := attachedAs(hub, "m1mini"); !ok || a.Via != "direct" {
		t.Fatalf("the link did not stay on direct: %+v", a)
	}
	var state string
	for _, p := range room.pathsNow() {
		if p.Kind == "ziti" {
			state = p.State
		}
	}
	if state != PathOff {
		t.Fatalf("the row says ziti is %q, want off", state)
	}

	// Over the link's own route, the way the hub's board asks.
	rec := httptest.NewRecorder()
	room.transportsHandler(rec, httptest.NewRequest(http.MethodPost, "/v1/link/transports",
		strings.NewReader(`{"transport":"ziti","on":true}`)))
	if rec.Code != 200 {
		t.Fatalf("turning on answered %d: %s", rec.Code, rec.Body)
	}
	waitFor(t, 10*time.Second, func() bool { a, ok := attachedAs(hub, "m1mini"); return ok && a.Via == "ziti" })
	mu.Lock()
	if len(saved) != 0 {
		t.Fatalf("still written down as off: %v", saved)
	}
	mu.Unlock()
}

// The last working path is never turned off, and the room says so in those words.
func TestTheLastWorkingPathIsNotTurnedOff(t *testing.T) {
	lightSlot(t)
	lnD, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	lnZ, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hub := NewHub(Timings{Beat: 100 * time.Millisecond, Silence: time.Second, Warm: 1})
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go func() { _ = hub.ServeVia(ctx, lnD, "direct") }()
	go func() { _ = hub.ServeVia(ctx, lnZ, "ziti") }()
	direct := &switchable{addr: lnD.Addr().String()}
	direct.down.Store(true)
	ziti := &switchable{addr: lnZ.Addr().String()}
	ladder := NewLadder([]Rung{
		{Path: Path{Transport: "ziti", Service: "atrium-hub"}, Dial: ziti},
		{Path: Path{Transport: "direct", Addr: lnD.Addr().String()}, Dial: direct},
	})
	room := &Room{Name: "sg3", Dial: ladder, Handler: http.NotFoundHandler(),
		T: Timings{Beat: 100 * time.Millisecond, Warm: 1, Backoff: 20 * time.Millisecond, DialWait: time.Second}}
	go func() { _ = room.Run(ctx) }()
	waitFor(t, 5*time.Second, func() bool { _, ok := attachedAs(hub, "sg3"); return ok })

	err = room.SetTransport(ctx, "ziti", false)
	if err == nil || !strings.Contains(err.Error(), "last working path to the hub") {
		t.Fatalf("turning off the only working path answered %v", err)
	}
	if ladder.IsOff("ziti") {
		t.Fatal("it was turned off anyway")
	}
	if a, ok := attachedAs(hub, "sg3"); !ok || a.Via != "ziti" {
		t.Fatalf("the link moved: %+v", a)
	}
}
