package link

import (
	"bufio"
	"context"
	"net"
	"testing"
	"time"
)

// THE WELCOME IS THE FIRST FRAME A ROOM READS, even when an attach hook asks
// the room for a connection straight away.
//
// The hub publishes a room before it writes the welcome, and publishing fires
// the attach hooks. A hook that wants a connection (the live one pushes the
// input-lag switch) finds none idle and writes `{"need":1}` on the control
// connection. When that landed first, the room read it as a welcome with no
// `ok`, logged "the hub refused this room:" with nothing after the colon, and
// redialled every five seconds. On 2026-09-29 that kept the live rooms off the
// hub for two and five minutes after a restart. The hook here gives the `need`
// a head start, which made the old code lose every time.
func TestTheWelcomeIsTheFirstFrameEvenWhenAnAttachHookWantsAConnection(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	hub := NewHub(Timings{Beat: 200 * time.Millisecond, Silence: 2 * time.Second, Warm: 1})
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	hub.OnAttach = func(name, _, _ string) {
		go func() {
			dctx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			if c, err := hub.Dial(dctx, name); err == nil {
				c.Close()
			}
		}()
		time.Sleep(100 * time.Millisecond)
	}
	go func() { _ = hub.Serve(ctx, ln) }()

	for i := 0; i < 5; i++ {
		conn, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		br := bufio.NewReader(conn)
		w, err := sayHello(conn, br, hello{Kind: "control", Room: "raced"})
		conn.Close()
		if err != nil {
			t.Fatalf("attach %d: %v (welcome %+v)", i, err, w)
		}
		if w.Session == "" {
			t.Fatalf("attach %d: a welcome with no session: %+v", i, w)
		}
	}
}
