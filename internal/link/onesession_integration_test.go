//go:build integration

package link

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/zitictx"
)

const (
	flatDials      = 300
	flatReconnects = 3
)

func TestEveryTransportHoldsOneSessionAcrossDialsClosesAndReconnects(t *testing.T) {
	t.Run("direct", func(t *testing.T) {
		r := newMuxRig(t, false, nil)
		for k := 0; k <= flatReconnects; k++ {
			if k > 0 {
				r.tcp.cut()
				waitFor(t, 10*time.Second, func() bool {
					return muxFor(r.room).Opened() == k+1 && r.hub.Has("testroom")
				})
			}
			for i := 0; i < flatDials; i++ {
				c, err := r.room.Dial(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				_ = c.Close()
			}
			r.get(t, 10)
		}
		waitFor(t, 5*time.Second, func() bool { return r.mux.Sessions() == 1 })
		if n, want := r.tcp.accepted.Load(), int32(1+flatReconnects); n != want {
			t.Fatalf("the hub accepted %d TCP connections over %d reconnects, want %d", n, flatReconnects, want)
		}
		if n := muxFor(r.room).Opened(); n != 1+flatReconnects {
			t.Fatalf("the room opened %d sessions, want %d", n, 1+flatReconnects)
		}
	})

	t.Run("ziti", func(t *testing.T) {
		reg := fakeSDK(t)
		flatOverlay(t, reg, func() Dialer { return &Ziti{Identity: "ziti-id.json", Service: "hub"} })
	})

	t.Run("zrok", func(t *testing.T) {
		reg := fakeSDK(t)
		flatOverlay(t, reg, func() Dialer { return &Zrok{Token: "tok", API: sdkZrok{identity: "zrok-env.json"}} })
	})
}

// flatOverlay dials an overlay through a ladder, resets it as the room does after failed dials, and rebuilds the
// dialer as a room does on each reconnect. The fake SDK counts contexts, the sessions of an overlay.
func flatOverlay(t *testing.T, reg *zitiReg, mk func() Dialer) {
	t.Helper()
	var clock atomic.Int64
	ungate := func(d Dialer) {
		now := func() time.Time { return time.Unix(clock.Add(3600), 0) }
		switch d := d.(type) {
		case *Ziti:
			d.gate.now = now
		case *Zrok:
			d.gate.now = now
		}
	}
	for k := 0; k <= flatReconnects; k++ {
		d := mk()
		ungate(d)
		l := NewLadder([]Rung{{Path: Path{Transport: "overlay"}, Dial: d}})
		for i := 0; i < flatDials; i++ {
			if c, err := l.Dial(context.Background()); err == nil {
				_ = c.Close()
			}
			if i%50 == 0 {
				l.Reset()
			}
		}
		l.Close()
	}
	if reg.built.Load() != 1 || zitictx.Live() != 1 || reg.closed.Load() != 0 {
		t.Fatalf("built %d, live %d, closed %d, want 1, 1 and 0", reg.built.Load(), zitictx.Live(), reg.closed.Load())
	}
	if n, want := reg.dialed.Load(), int32((1+flatReconnects)*flatDials); n != want {
		t.Fatalf("dialed %d circuits, want %d", n, want)
	}
}
