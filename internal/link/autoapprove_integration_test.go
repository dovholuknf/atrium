//go:build integration

package link

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/dovholuknf/atrium/internal/link/notify"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// gate is a room holding one undecided permission until somebody decides it.
type gate struct {
	mu       sync.Mutex
	decided  bool
	decision string
	reason   string
	by       string
}

func (g *gate) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/permissions" && r.Method == http.MethodGet:
			g.mu.Lock()
			pending := !g.decided
			g.mu.Unlock()
			if pending {
				fmt.Fprint(w, `{"permissions":[{"id":"p1","tool":"Bash","command":"ls"}]}`)
			} else {
				fmt.Fprint(w, `{"permissions":[]}`)
			}
		case r.URL.Path == "/v1/permissions/p1/decide" && r.Method == http.MethodPost:
			var body struct{ Decision, Reason, By string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			g.mu.Lock()
			g.decided = true
			g.decision, g.reason, g.by = body.Decision, body.Reason, body.By
			g.mu.Unlock()
			fmt.Fprint(w, `{"ok":true}`)
		default:
			// `/v1/state` and the event stream the announcer and feeds ask for.
			// Answered emptily so nothing here fails a room.
			fmt.Fprint(w, `{"cards":[]}`)
		}
	})
}

func (g *gate) answered() (bool, string, string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.decided, g.decision, g.reason
}

// oneRoom attaches a single room to a hub and returns the proxy so a test can
// wire an inventory onto it, which is what starts the approver.
func oneRoom(t *testing.T, name string, h http.Handler) (*Proxy, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hub := NewHub(Timings{Beat: 200 * time.Millisecond, Silence: 2 * time.Second, Warm: 2})
	ctx, stop := context.WithCancel(context.Background())
	go func() { _ = hub.Serve(ctx, ln) }()

	room := &Room{
		Name: name, Dial: plain{addr: ln.Addr().String()}, Handler: h,
		T: Timings{Beat: 200 * time.Millisecond, Warm: 2, Backoff: 50 * time.Millisecond},
	}
	go func() { _ = room.Run(ctx) }()
	waitFor(t, 5*time.Second, func() bool { return hub.Has(name) })

	proxy := NewProxy(hub, nil, "", nil)
	front := httptest.NewServer(proxy)
	return proxy, func() { front.Close(); stop(); ln.Close() }
}

// board-wide auto ON: the hub approves a request the room only ever reported,
// never itself decided.
func TestHubApprovesAPendingRequestWhenBoardWideAutoIsOn(t *testing.T) {
	g := &gate{}
	proxy, done := oneRoom(t, "alpha", g.handler())
	defer done()

	// Wiring the inventory starts the approver, and this inventory says the flag
	// is on.
	proxy.SetInventory(&remembering{boardAuto: true})

	waitFor(t, 5*time.Second, func() bool {
		decided, _, _ := g.answered()
		return decided
	})
	_, decision, reason := g.answered()
	if decision != "approve" {
		t.Fatalf("the hub decided %q, wanted approve", decision)
	}
	// Recorded under its own name, never "you". The body carries `by` and nothing
	// else identifying the caller.
	g.mu.Lock()
	by := g.by
	g.mu.Unlock()
	if by != "global-auto" {
		t.Errorf("the decide body carried by=%q, wanted global-auto", by)
	}
	if reason != notify.HubAutoReason {
		t.Errorf("the request was approved with reason %q, wanted the board-wide one", reason)
	}
}

// board-wide auto OFF: the request stays frozen. The hub does nothing, and the
// room's own gate would ask a human exactly as before.
func TestHubLeavesARequestPendingWhenBoardWideAutoIsOff(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: a real link, hub and git rig over loopback")
	}
	lightSlot(t)
	g := &gate{}
	proxy, done := oneRoom(t, "alpha", g.handler())
	defer done()

	inv := &looked{remembering: &remembering{boardAuto: false}}
	proxy.SetInventory(inv)

	waitFor(t, 5*time.Second, func() bool { return inv.looks.Load() >= 1 })
	if decided, _, _ := g.answered(); decided {
		t.Fatalf("a request was approved with board-wide auto off")
	}
}

type looked struct {
	*remembering
	looks atomic.Int64
}

func (l *looked) BoardAuto() (bool, *time.Time, error) {
	on, until, err := l.remembering.BoardAuto()
	l.looks.Add(1)
	return on, until, err
}
