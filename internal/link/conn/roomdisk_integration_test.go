//go:build integration

package conn

import (
	"context"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestARoomsDiskNoticeRidesTheBeatAndClears(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hub := NewHub(Timings{Beat: 50 * time.Millisecond, Silence: 2 * time.Second, Warm: 1})
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go func() { _ = hub.Serve(ctx, ln) }()

	var notice atomic.Value
	notice.Store("")
	room := &Room{Name: "sg3", Dial: plain{addr: ln.Addr().String()}, Handler: http.NotFoundHandler(),
		T:    Timings{Beat: 50 * time.Millisecond, Warm: 1, Backoff: 50 * time.Millisecond},
		Disk: func() string { return notice.Load().(string) }}
	go func() { _ = room.Run(ctx) }()
	attached := func() string {
		for _, a := range hub.Rooms() {
			if a.Name == "sg3" {
				return a.DiskLow
			}
		}
		return "<absent>"
	}
	waitFor(t, 5*time.Second, func() bool { return attached() == "" })
	notice.Store("sg3: disk getting low. C: has 7 GB free")
	waitFor(t, 5*time.Second, func() bool { return attached() == "sg3: disk getting low. C: has 7 GB free" })
	notice.Store("")
	waitFor(t, 5*time.Second, func() bool { return attached() == "" })
}
