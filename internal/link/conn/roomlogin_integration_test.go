//go:build integration

package conn

import (
	"context"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A room saying on its beat that its claude login is gone is shown in the attached view, rung once for the spell,
// and shown as fine again when it says so.
func TestARoomsLoginNeededRidesTheBeatAndRingsOnce(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hub := NewHub(Timings{Beat: 50 * time.Millisecond, Silence: 2 * time.Second, Warm: 1})
	var rang atomic.Int32
	var mu sync.Mutex
	var heard []string
	hub.OnLogin = func(room, reason string) {
		rang.Add(1)
		mu.Lock()
		heard = append(heard, room+":"+reason)
		mu.Unlock()
	}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go func() { _ = hub.Serve(ctx, ln) }()

	var reason atomic.Value
	reason.Store("")
	room := &Room{Name: "m1mini", Dial: plain{addr: ln.Addr().String()}, Handler: http.NotFoundHandler(),
		T:     Timings{Beat: 50 * time.Millisecond, Warm: 1, Backoff: 50 * time.Millisecond},
		Login: func() string { return reason.Load().(string) }}
	go func() { _ = room.Run(ctx) }()
	attachedLogin := func() string {
		for _, a := range hub.Rooms() {
			if a.Name == "m1mini" {
				return a.LoginNeeded
			}
		}
		return "<absent>"
	}
	waitFor(t, 5*time.Second, func() bool { return attachedLogin() == "" })

	reason.Store("claude needs a login")
	waitFor(t, 5*time.Second, func() bool { return attachedLogin() == "claude needs a login" })
	time.Sleep(300 * time.Millisecond) // wait: a negative check, several beats repeating the same reason must not ring again
	if n := rang.Load(); n != 1 {
		t.Fatalf("rang %d times for one spell, wanted 1", n)
	}
	mu.Lock()
	if len(heard) != 1 || heard[0] != "m1mini:claude needs a login" {
		t.Fatalf("heard %v", heard)
	}
	mu.Unlock()

	reason.Store("")
	waitFor(t, 5*time.Second, func() bool { return attachedLogin() == "" })
	reason.Store("claude needs a login")
	waitFor(t, 5*time.Second, func() bool { return rang.Load() == 2 })
}
