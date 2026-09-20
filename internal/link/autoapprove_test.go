package link

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// The whole point of moving the flag to the hub: a request from a session the
// room's own gate never auto-approved is answered by the HUB the moment it is
// pending, when board-wide auto is on, and gates when it is off.
//
// ── what the fake room stands in for ─────────────────────
//
// A real room's `onPermRequest` runs the chain and parks anything nothing
// answered in `needs-permission`, reporting it on `GET /v1/permissions`. This
// room reports one such request and never decides it itself, which is exactly a
// freshly launched or reconnecting session frozen waiting for a human. The room
// has NO board-wide auto of its own, so if the request is approved it can only be
// the hub that did it.

// gate is a room holding one undecided permission until somebody decides it.
type gate struct {
	mu       sync.Mutex
	decided  bool
	decision string
	reason   string
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
			var body struct{ Decision, Reason string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			g.mu.Lock()
			g.decided = true
			g.decision, g.reason = body.Decision, body.Reason
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
	if reason != hubAutoReason {
		t.Errorf("the request was approved with reason %q, wanted the board-wide one", reason)
	}
}

// board-wide auto OFF: the request stays frozen. The hub does nothing, and the
// room's own gate would ask a human exactly as before.
func TestHubLeavesARequestPendingWhenBoardWideAutoIsOff(t *testing.T) {
	g := &gate{}
	proxy, done := oneRoom(t, "alpha", g.handler())
	defer done()

	proxy.SetInventory(&remembering{boardAuto: false})

	// A few sweep intervals is long enough that an approver that was going to act
	// would have. Nothing should have touched the request.
	time.Sleep(3 * autoSweepEvery)
	if decided, _, _ := g.answered(); decided {
		t.Fatalf("a request was approved with board-wide auto off")
	}
}
