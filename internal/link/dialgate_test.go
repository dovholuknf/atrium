package link

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// fakeClock is a clock a test moves by hand.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// A controller that refuses every dial gets one dial per backoff step and never a tight loop, over ten minutes that
// cost no real time.
func TestRefusingControllerIsDialledOnABackoffNotInALoop(t *testing.T) {
	z, reg := newFakeZiti(false)
	clk := &fakeClock{t: time.Unix(1_000_000, 0)}
	z.gate.now = clk.now
	dials := 0
	for elapsed := time.Duration(0); elapsed < 10*time.Minute; elapsed += 100 * time.Millisecond {
		before := reg.dialed.Load()
		_, _ = z.Dial(context.Background())
		if reg.dialed.Load() != before {
			dials++
		}
		clk.advance(100 * time.Millisecond)
	}
	// 0, 1, 3, 7, 15, 31, 63 s, then a dial every minute: 7 + 9 up to ten minutes.
	if dials > 17 {
		t.Fatalf("%d dials in 10 minutes against a refusing controller, want at most 17", dials)
	}
	if dials < 7 {
		t.Fatalf("%d dials in 10 minutes, the backoff is holding the room off for good", dials)
	}
	if reg.built.Load() != 1 || reg.closed.Load() != 0 {
		t.Fatalf("built %d contexts, closed %d, want 1 and 0", reg.built.Load(), reg.closed.Load())
	}
}

func TestFirstTenSecondsOfARefusalIsFourDials(t *testing.T) {
	z, reg := newFakeZiti(false)
	clk := &fakeClock{t: time.Unix(1_000_000, 0)}
	z.gate.now = clk.now
	for i := 0; i < 100; i++ {
		_, _ = z.Dial(context.Background())
		clk.advance(100 * time.Millisecond)
	}
	if got := reg.dialed.Load(); got != 4 {
		t.Fatalf("%d dials in 10 s, want 4 (at 0, 1, 3 and 7 s)", got)
	}
}

func TestADialThatWorksClearsTheBackoff(t *testing.T) {
	var g dialGate
	clk := &fakeClock{t: time.Unix(1_000_000, 0)}
	g.now = clk.now
	for i := 0; i < 5; i++ {
		g.failed()
		clk.advance(time.Hour)
	}
	g.failed()
	if _, ok := g.allow(); ok {
		t.Fatal("allowed straight after a failure")
	}
	g.worked()
	if _, ok := g.allow(); !ok {
		t.Fatal("still held off after a dial worked")
	}
	g.failed()
	if w, _ := g.allow(); w != dialFloor {
		t.Fatalf("first wait after a success is %s, want %s", w, dialFloor)
	}
}

func TestHammeringNeedsFailuresWithinAMinute(t *testing.T) {
	var g dialGate
	clk := &fakeClock{t: time.Unix(1_000_000, 0)}
	g.now = clk.now
	for i := 0; i < hammerFails; i++ {
		g.failed()
	}
	if !g.hammering() {
		t.Fatal("not flagged after a burst of failures")
	}
	clk.advance(2 * time.Minute)
	if g.hammering() {
		t.Fatal("still flagged two minutes on")
	}
}

func TestProxyRefusesOverlaySetupAndTeardown(t *testing.T) {
	p := &Proxy{}
	for _, path := range []string{"/v1/overlays/zrok/setup", "/v1/overlays/ziti/teardown"} {
		for _, c := range []struct{ name, remote, host, fwd string }{
			{"loopback", "127.0.0.1:50000", "127.0.0.1:7778", ""},
			{"forwarded", "127.0.0.1:50000", "127.0.0.1:7778", "1.2.3.4"},
			{"non-loopback", "10.0.0.5:50000", "10.0.0.5:7778", ""},
		} {
			t.Run(path+"/"+c.name, func(t *testing.T) {
				r := httptest.NewRequest("POST", path, nil)
				r.RemoteAddr, r.Host = c.remote, c.host
				if c.fwd != "" {
					r.Header.Set("X-Forwarded-For", c.fwd)
				}
				w := httptest.NewRecorder()
				p.ServeHTTP(w, r)
				if w.Code != http.StatusForbidden {
					t.Fatalf("%d %s", w.Code, w.Body.String())
				}
			})
		}
	}
}
