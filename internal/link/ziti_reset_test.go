package link

import (
	"context"
	"errors"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openziti/sdk-golang/ziti/edge"
)

// fakeZiti stands in for the SDK's context: it starts a goroutine that lives until Close, as the refresh loop does.
type fakeZiti struct {
	reg    *zitiReg
	done   chan struct{}
	exited sync.WaitGroup
	once   sync.Once
	block  bool
}

type zitiReg struct {
	built, live, maxLive atomic.Int32
	block                bool
	all                  []*fakeZiti
	mu                   sync.Mutex
}

func (r *zitiReg) make() (zitiConn, error) {
	f := &fakeZiti{reg: r, done: make(chan struct{}), block: r.block}
	r.built.Add(1)
	n := r.live.Add(1)
	for {
		m := r.maxLive.Load()
		if n <= m || r.maxLive.CompareAndSwap(m, n) {
			break
		}
	}
	f.exited.Add(1)
	go func() { defer f.exited.Done(); <-f.done }()
	r.mu.Lock()
	r.all = append(r.all, f)
	r.mu.Unlock()
	return f, nil
}

func (r *zitiReg) wait() {
	r.mu.Lock()
	all := append([]*fakeZiti(nil), r.all...)
	r.mu.Unlock()
	for _, f := range all {
		f.exited.Wait()
	}
}

func (f *fakeZiti) Dial(string) (edge.Conn, error) {
	if f.block {
		<-f.done
	}
	return nil, errors.New("fake: no route")
}
func (f *fakeZiti) Listen(string) (edge.Listener, error) { return nil, errors.New("fake") }
func (f *fakeZiti) Close() {
	f.once.Do(func() { f.reg.live.Add(-1); close(f.done) })
}

func newFakeZiti(block bool) (*Ziti, *zitiReg) {
	reg := &zitiReg{block: block}
	return &Ziti{Service: "hub", newCtx: reg.make}, reg
}

func TestZitiRedialsNeverHoldMoreThanOneContext(t *testing.T) {
	base := runtime.NumGoroutine()
	z, reg := newFakeZiti(false)
	l := NewLadder([]Rung{{Path: Path{Transport: "ziti", Service: "hub"}, Dial: z}})
	// Each cycle is an hour apart, so the backoff never holds a dial back.
	var hour atomic.Int64
	z.gate.now = func() time.Time { return time.Unix(hour.Load(), 0) }
	for i := 0; i < 200; i++ {
		hour.Add(3600)
		_, _ = l.Dial(context.Background())
		l.Reset()
	}
	if reg.maxLive.Load() != 1 {
		t.Fatalf("most live contexts at once = %d, want 1", reg.maxLive.Load())
	}
	if reg.built.Load() != 200 {
		t.Fatalf("built %d contexts for 200 reset cycles", reg.built.Load())
	}
	l.Close()
	reg.wait()
	if reg.live.Load() != 0 {
		t.Fatalf("%d contexts still live after Close", reg.live.Load())
	}
	if got := runtime.NumGoroutine(); got > base+2 {
		t.Fatalf("goroutines %d, was %d before: contexts leaked", got, base)
	}
}

func TestZitiDialsReuseOneContext(t *testing.T) {
	z, reg := newFakeZiti(false)
	for i := 0; i < 100; i++ {
		_, _ = z.Dial(context.Background())
	}
	if reg.built.Load() != 1 {
		t.Fatalf("built %d contexts for 100 dials", reg.built.Load())
	}
	z.Close()
	z.Close()
}

func TestZitiResetFreesAStuckDial(t *testing.T) {
	z, reg := newFakeZiti(true)
	done := make(chan error, 1)
	go func() { _, err := z.Dial(context.Background()); done <- err }()
	for reg.built.Load() == 0 {
		runtime.Gosched()
	}
	z.Reset() // must not wait on the stuck dial
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("stuck dial returned no error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Reset did not free the stuck dial")
	}
}

func TestProvenForwardsResetAndClose(t *testing.T) {
	z, reg := newFakeZiti(false)
	var d Dialer = Proven{Dialer: z}
	_, _ = z.Dial(context.Background())
	d.(resetter).Reset()
	if reg.live.Load() != 0 {
		t.Fatal("Reset through Proven did not close the context")
	}
	_, _ = z.Dial(context.Background())
	d.(closer).Close()
	if reg.live.Load() != 0 {
		t.Fatal("Close through Proven did not close the context")
	}
}

type countDialer struct{ closed atomic.Int32 }

func (d *countDialer) Dial(context.Context) (net.Conn, error) { return nil, errors.New("no") }
func (d *countDialer) Describe() string                       { return "count" }
func (d *countDialer) Close()                                 { d.closed.Add(1) }

func TestLadderSetClosesDroppedDialersAndKeepsTheOneItStandsOn(t *testing.T) {
	a, b, c := &countDialer{}, &countDialer{}, &countDialer{}
	pa, pb, pc := Path{Transport: "ziti", Service: "a"}, Path{Transport: "ziti", Service: "b"}, Path{Transport: "ziti", Service: "c"}
	l := NewLadder([]Rung{{pa, a}, {pb, b}, {pc, c}})
	// Same dialers again: nothing closes, and a hundred refreshes build nothing.
	for i := 0; i < 100; i++ {
		l.Set([]Rung{{pa, a}, {pb, b}, {pc, c}})
	}
	if a.closed.Load()+b.closed.Load()+c.closed.Load() != 0 {
		t.Fatal("a Set with the same dialers closed one")
	}
	// Drop b and c. The ladder stands on a, so only b and c go.
	l.Set([]Rung{{pa, a}})
	if a.closed.Load() != 0 || b.closed.Load() != 1 || c.closed.Load() != 1 {
		t.Fatalf("closed a=%d b=%d c=%d, want 0 1 1", a.closed.Load(), b.closed.Load(), c.closed.Load())
	}
	// Dropping the rung it stands on keeps it until the ladder moves.
	l.Set([]Rung{{pb, b}})
	if a.closed.Load() != 0 {
		t.Fatal("closed the rung the ladder stands on")
	}
}
