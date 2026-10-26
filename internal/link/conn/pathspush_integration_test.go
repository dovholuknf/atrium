//go:build integration

package conn

import (
	"context"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

// A hub whose addresses changed tells a room that is already attached, with no reattach.
func TestAHubPushesChangedPathsToAnAttachedRoom(t *testing.T) {
	lightSlot(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	paths := []Path{{Transport: "direct", Addr: "192.168.102.41:7779"}}
	hub := NewHub(Timings{Beat: 100 * time.Millisecond, Silence: time.Second, Warm: 1})
	hub.Paths = func() []Path {
		mu.Lock()
		defer mu.Unlock()
		return append([]Path(nil), paths...)
	}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go func() { _ = hub.ServeVia(ctx, ln, "direct") }()

	var hm sync.Mutex
	var heard [][]Path
	room := &Room{
		Name: "sg3", Dial: &switchable{addr: ln.Addr().String()},
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }),
		T:       Timings{Beat: 100 * time.Millisecond, Warm: 1, Backoff: 20 * time.Millisecond},
		OnPaths: func(ps []Path) {
			hm.Lock()
			heard = append(heard, ps)
			hm.Unlock()
		},
	}
	go func() { _ = room.Run(ctx) }()
	waitFor(t, 5*time.Second, func() bool { return hub.Has("sg3") })

	mu.Lock()
	paths = []Path{{Transport: "direct", Addr: "192.168.1.68:7779"}, {Transport: "direct", Addr: "hub.example:7779"}}
	mu.Unlock()
	hub.PushPaths()

	waitFor(t, 5*time.Second, func() bool {
		hm.Lock()
		defer hm.Unlock()
		return len(heard) > 0 && len(heard[len(heard)-1]) == 2 && heard[len(heard)-1][0].Addr == "192.168.1.68:7779"
	})
}
