package cli

import (
	"context"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/daemon"
	"github.com/dovholuknf/atrium/internal/link"
)

type tcpDial struct{ addr string }

func (p tcpDial) Dial(ctx context.Context) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "tcp", p.addr)
}
func (p tcpDial) Describe() string { return p.addr }

// L2 of the review. linkRelay.Say is the one line between the room and the hub, so this drives it
// against a real hub and reads `wake` off the request the hub received.
func TestTheRoomsRelaySayCarriesWakeToTheHub(t *testing.T) {
	var mu sync.Mutex
	var got []link.RelayRequest
	tm := link.Timings{Beat: 200 * time.Millisecond, Silence: time.Second, Warm: 1, Backoff: 50 * time.Millisecond}
	hub := link.NewHub(tm)
	hub.Relay = func(_ context.Context, _ string, req link.RelayRequest) link.RelayAnswer {
		mu.Lock()
		got = append(got, req)
		mu.Unlock()
		return link.RelayAnswer{OK: true, Delivered: "queued"}
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = hub.Serve(ctx, ln) }()
	room := &link.Room{Name: "m1mini", Version: "b", Dial: tcpDial{ln.Addr().String()},
		Handler: http.NotFoundHandler(), T: tm}
	go func() { _ = room.Run(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	for !room.State().Up {
		if time.Now().After(deadline) {
			t.Fatal("the room never attached")
		}
		time.Sleep(20 * time.Millisecond)
	}

	rctx, rcancel := context.WithTimeout(ctx, 10*time.Second)
	defer rcancel()
	for _, wake := range []bool{true, false} {
		res, err := linkRelay{room}.Say(rctx, daemon.RelaySay{From: "sa1", Room: "sg4", To: "orch", Text: "x", Wake: wake})
		if err != nil || !res.OK {
			t.Fatalf("say: %+v, %v", res, err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 || !got[0].Wake || got[1].Wake {
		t.Fatalf("the hub got %+v, want wake on the first and not the second", got)
	}
}
