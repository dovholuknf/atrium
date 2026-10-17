package link

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/zitictx"
	"github.com/openziti/sdk-golang/ziti"
	"github.com/openziti/sdk-golang/ziti/edge"
)

type fakeZiti struct {
	reg  *zitiReg
	done chan struct{}
	once sync.Once
}

type zitiReg struct {
	built, closed, dialed, relogins atomic.Int32
	block                           bool
}

func (r *zitiReg) make() (zitiConn, error) {
	r.built.Add(1)
	return &fakeZiti{reg: r, done: make(chan struct{})}, nil
}

func (f *fakeZiti) Dial(string) (edge.Conn, error) {
	f.reg.dialed.Add(1)
	if f.reg.block {
		<-f.done
	}
	return nil, errors.New("fake: no route")
}
func (f *fakeZiti) Listen(string) (edge.Listener, error) { return nil, errors.New("fake") }
func (f *fakeZiti) Relogin() error                       { f.reg.relogins.Add(1); return nil }
func (f *fakeZiti) Close() {
	f.once.Do(func() { f.reg.closed.Add(1); close(f.done) })
}

func newFakeZiti(block bool) (*Ziti, *zitiReg) {
	reg := &zitiReg{block: block}
	return &Ziti{Service: "hub", newCtx: reg.make}, reg
}

type sdkCtx struct {
	ziti.Context
	reg *zitiReg
}

func (c *sdkCtx) Dial(string) (edge.Conn, error) {
	c.reg.dialed.Add(1)
	return nil, errors.New("fake: no route")
}
func (c *sdkCtx) Reauthenticate() error { c.reg.relogins.Add(1); return nil }
func (c *sdkCtx) Close()                { c.reg.closed.Add(1) }

func fakeSDK(t *testing.T) *zitiReg {
	t.Helper()
	reg := &zitiReg{}
	restore := zitictx.SetSDK(
		func(string) (*ziti.Config, error) { return &ziti.Config{}, nil },
		func(*ziti.Config) (ziti.Context, error) { reg.built.Add(1); return &sdkCtx{reg: reg}, nil })
	t.Cleanup(func() { zitictx.CloseAll(); restore() })
	return reg
}

func TestZitiRedialsAndResetsBuildOneContextAndCloseNone(t *testing.T) {
	z, reg := newFakeZiti(false)
	l := NewLadder([]Rung{{Path: Path{Transport: "ziti", Service: "hub"}, Dial: z}})
	var hour atomic.Int64
	z.gate.now = func() time.Time { return time.Unix(hour.Load(), 0) }
	for i := 0; i < 200; i++ {
		hour.Add(3600)
		_, _ = l.Dial(context.Background())
		l.Reset()
	}
	l.Close()
	if reg.built.Load() != 1 || reg.closed.Load() != 0 {
		t.Fatalf("built %d, closed %d, want 1 and 0", reg.built.Load(), reg.closed.Load())
	}
}

func TestManyZitiValuesForOneIdentityShareOneProcessContext(t *testing.T) {
	reg := fakeSDK(t)
	var hour atomic.Int64
	for i := 0; i < 20; i++ {
		z := &Ziti{Identity: "id.json", Service: "hub"}
		z.gate.now = func() time.Time { return time.Unix(hour.Add(3600), 0) }
		for j := 0; j < 5; j++ {
			_, _ = z.Dial(context.Background())
			z.Reset()
		}
		z.Close()
	}
	if reg.built.Load() != 1 || zitictx.Live() != 1 || reg.closed.Load() != 0 {
		t.Fatalf("built %d, live %d, closed %d, want 1, 1 and 0", reg.built.Load(), zitictx.Live(), reg.closed.Load())
	}
}

func TestZitiResetLogsInAgainWithoutBuilding(t *testing.T) {
	z, reg := newFakeZiti(false)
	_, _ = z.Dial(context.Background())
	z.Reset()
	deadline := time.Now().Add(2 * time.Second)
	for reg.relogins.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if reg.relogins.Load() != 1 || reg.built.Load() != 1 {
		t.Fatalf("relogins %d, built %d, want 1 and 1", reg.relogins.Load(), reg.built.Load())
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

func TestZitiDialReturnsWhenItsContextIsDoneWhileTheSDKIsStuck(t *testing.T) {
	z, reg := newFakeZiti(true)
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := z.Dial(ctx); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("stuck dial returned no error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a dial stuck in the SDK outlived its deadline")
	}
	if reg.closed.Load() != 0 {
		t.Fatal("a stuck dial closed the context")
	}
}

func TestProvenForwardsResetAndClose(t *testing.T) {
	z, reg := newFakeZiti(false)
	var d Dialer = Proven{Dialer: z}
	_, _ = z.Dial(context.Background())
	d.(resetter).Reset()
	d.(closer).Close()
	if reg.closed.Load() != 0 {
		t.Fatal("Reset or Close through Proven closed the process's context")
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
	for i := 0; i < 100; i++ {
		l.Set([]Rung{{pa, a}, {pb, b}, {pc, c}})
	}
	if a.closed.Load()+b.closed.Load()+c.closed.Load() != 0 {
		t.Fatal("a Set with the same dialers closed one")
	}
	l.Set([]Rung{{pa, a}})
	if a.closed.Load() != 0 || b.closed.Load() != 1 || c.closed.Load() != 1 {
		t.Fatalf("closed a=%d b=%d c=%d, want 0 1 1", a.closed.Load(), b.closed.Load(), c.closed.Load())
	}
	l.Set([]Rung{{pb, b}})
	if a.closed.Load() != 0 {
		t.Fatal("closed the rung the ladder stands on")
	}
}
