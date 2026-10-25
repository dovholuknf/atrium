package link

import (
	"context"
	"crypto/tls"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// blockDialer never returns from Dial, like a ziti dial that ignores its context.
type blockDialer struct {
	release chan struct{}
	started atomic.Int32
	resets  atomic.Int32
}

func (d *blockDialer) Dial(context.Context) (net.Conn, error) {
	d.started.Add(1)
	<-d.release
	return nil, net.ErrClosed
}
func (d *blockDialer) Describe() string { return "block" }
func (d *blockDialer) Reset()           { d.resets.Add(1) }

func newDialRoom(d Dialer, deadline time.Duration) (*Room, *atomic.Int32) {
	r := &Room{Name: "r", Dial: d, T: Timings{DataDeadline: deadline}.Fill()}
	r.conns = make(chan net.Conn, 1)
	var drops atomic.Int32
	r.dropLink = func() { drops.Add(1) }
	return r, &drops
}

func (r *Room) inflight() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.dials)
}

func TestOpenBoundsDialsAgainstABlockedDialer(t *testing.T) {
	d := &blockDialer{release: make(chan struct{})}
	defer close(d.release)
	r, _ := newDialRoom(d, time.Hour)
	before := runtime.NumGoroutine()
	for i := 0; i < 1000; i++ {
		r.open(context.Background(), 4, "s")
	}
	if got := r.inflight(); got != maxDataDials {
		t.Fatalf("in flight = %d, want %d", got, maxDataDials)
	}
	if grew := runtime.NumGoroutine() - before; grew > maxDataDials+4 {
		t.Fatalf("goroutines grew by %d after 1000 asks", grew)
	}
}

func TestStuckDialsPastTheirDeadlineDropTheLink(t *testing.T) {
	d := &blockDialer{release: make(chan struct{})}
	defer close(d.release)
	r, drops := newDialRoom(d, time.Millisecond)
	r.open(context.Background(), dataRedialAfter, "s")
	deadline := time.Now().Add(2 * time.Second)
	for drops.Load() == 0 && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	if drops.Load() != 1 {
		t.Fatalf("drops = %d, want 1", drops.Load())
	}
	if d.resets.Load() != 1 {
		t.Fatalf("resets = %d, want 1", d.resets.Load())
	}
	if r.inflight() != dataRedialAfter {
		t.Fatalf("stuck dials must keep their slots, in flight = %d", r.inflight())
	}
}

func TestAFullCapOfLateDialsCountsEachAskAsAFailure(t *testing.T) {
	d := &blockDialer{release: make(chan struct{})}
	defer close(d.release)
	r, drops := newDialRoom(d, time.Nanosecond)
	r.open(context.Background(), maxDataDials, "s")
	for i := 0; i < dataRedialAfter; i++ {
		r.open(context.Background(), 1, "s")
	}
	deadline := time.Now().Add(2 * time.Second)
	for drops.Load() == 0 && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	if drops.Load() == 0 {
		t.Fatal("asks against a full cap of late dials never dropped the link")
	}
}

func TestReattachingNeverExceedsTheCap(t *testing.T) {
	d := &blockDialer{release: make(chan struct{})}
	defer close(d.release)
	r, _ := newDialRoom(d, time.Hour)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		r.open(ctx, 4, "s")
		r.open(ctx, 4, "s")
		cancel()
		if got := r.inflight(); got > maxDataDials {
			t.Fatalf("attachment %d: in flight %d exceeds %d", i, got, maxDataDials)
		}
	}
	wg.Wait()
	if int(d.started.Load()) > maxDataDials {
		t.Fatalf("%d dials ever started against a blocked dialer, cap is %d", d.started.Load(), maxDataDials)
	}
}

// nilTLSDialer fails after release with a typed nil *tls.Conn, the shape that crashed the sgg room.
type nilTLSDialer struct{ release chan struct{} }

func (d nilTLSDialer) Dial(context.Context) (net.Conn, error) {
	<-d.release
	return (*tls.Conn)(nil), net.ErrClosed
}

func TestLateFailedDialTypedNilNotClosed(t *testing.T) {
	d := nilTLSDialer{release: make(chan struct{})}
	base := runtime.NumGoroutine()
	if _, err := dialWithin(context.Background(), d, time.Millisecond); err == nil {
		t.Fatal("dialWithin answered a dial that outlived its wait")
	}
	l := NewLadder([]Rung{{Path: Path{Transport: "direct", Addr: "h:1"}, Dial: d}})
	if _, err, _ := l.probeRung(context.Background(), l.Rungs()[0], time.Millisecond); err == nil {
		t.Fatal("probeRung answered a dial that outlived its wait")
	}
	close(d.release)
	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > base && time.Now().Before(deadline) {
		runtime.Gosched()
	}
}

func (nilTLSDialer) Describe() string { return "niltls" }

type failDialer struct{ resets atomic.Int32 }

func (d *failDialer) Dial(context.Context) (net.Conn, error) { return nil, net.ErrClosed }
func (d *failDialer) Describe() string                       { return "fail" }
func (d *failDialer) Reset()                                 { d.resets.Add(1) }

func TestGitDialsThatFailDropTheLinkSoTheNextAttachDialsAfresh(t *testing.T) {
	d := &failDialer{}
	r, drops := newDialRoom(d, time.Hour)
	r.up, r.session, r.hubGit = true, "s", true
	for i := 0; i < dataRedialAfter; i++ {
		if _, err := r.DialGit(context.Background()); err == nil {
			t.Fatal("DialGit succeeded against a dialer that fails")
		}
	}
	if drops.Load() != 1 || d.resets.Load() != 1 {
		t.Fatalf("drops = %d, resets = %d, want 1 and 1", drops.Load(), d.resets.Load())
	}
}
