package link

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

// A hub asking a room to restart has to reach the room's OnRestart with the ask
// intact. The hub forwards one line and does nothing else, so this is the whole
// contract between the two halves.
func TestAskRestartReachesTheRoom(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	hub := NewHub(Timings{Beat: 200 * time.Millisecond, Silence: time.Second, Warm: 1})
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go func() { _ = hub.Serve(ctx, ln) }()

	got := make(chan RestartAsk, 1)
	room := &Room{
		Name:      "testroom",
		Dial:      plain{addr: ln.Addr().String()},
		Handler:   http.NewServeMux(),
		T:         Timings{Beat: 200 * time.Millisecond, Warm: 1, Backoff: 50 * time.Millisecond},
		OnRestart: func(a RestartAsk) { got <- a },
	}
	go func() { _ = room.Run(ctx) }()
	waitFor(t, 5*time.Second, func() bool { return hub.Has("testroom") })

	if err := hub.AskRestart("testroom", RestartAsk{Why: "new binary", Force: true, WaitSeconds: 12}); err != nil {
		t.Fatalf("AskRestart: %v", err)
	}

	select {
	case a := <-got:
		if a.Why != "new binary" || !a.Force || a.WaitSeconds != 12 {
			t.Fatalf("the ask arrived changed: %+v", a)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the room's OnRestart never fired")
	}
}

// Asking a room that is not attached is an error, not a silent success. There is
// nothing to forward the instruction down.
func TestAskRestartUnknownRoom(t *testing.T) {
	hub := NewHub(Timings{})
	if err := hub.AskRestart("ghost", RestartAsk{}); err == nil {
		t.Fatal("asking an unattached room to restart should fail")
	}
}

// Two restart asks in quick succession run the handler once, not twice. Without
// the guard both reach OnRestart, both spawn a detached restarter, and the two
// race to bind the same address; the loser fails to bind and exits as an
// orphaned process. The guard drops the duplicate while the first is in flight.
func TestDuplicateRestartAsksRunOnce(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	hub := NewHub(Timings{Beat: 200 * time.Millisecond, Silence: time.Second, Warm: 1})
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go func() { _ = hub.Serve(ctx, ln) }()

	calls := make(chan RestartAsk, 4)
	release := make(chan struct{})
	room := &Room{
		Name:    "testroom",
		Dial:    plain{addr: ln.Addr().String()},
		Handler: http.NewServeMux(),
		T:       Timings{Beat: 200 * time.Millisecond, Warm: 1, Backoff: 50 * time.Millisecond},
		// Stay in flight until released, so the second ask arrives while the first
		// is still holding the guard.
		OnRestart: func(a RestartAsk) {
			calls <- a
			<-release
		},
	}
	go func() { _ = room.Run(ctx) }()
	waitFor(t, 5*time.Second, func() bool { return hub.Has("testroom") })

	if err := hub.AskRestart("testroom", RestartAsk{Why: "first"}); err != nil {
		t.Fatalf("first AskRestart: %v", err)
	}
	// The first handler is in flight now.
	select {
	case <-calls:
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatal("the first restart ask never reached the room")
	}

	if err := hub.AskRestart("testroom", RestartAsk{Why: "second"}); err != nil {
		t.Fatalf("second AskRestart: %v", err)
	}
	select {
	case <-calls:
		close(release)
		t.Fatal("a duplicate restart ask started a second restarter while the first was in flight")
	case <-time.After(500 * time.Millisecond):
	}
	close(release)
}
