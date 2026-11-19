//go:build integration

package conn

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/link/wire"
)

type gatedDialer struct {
	calls chan struct{}
	conns chan net.Conn
}

func (d gatedDialer) Dial(ctx context.Context) (net.Conn, error) {
	d.calls <- struct{}{}
	select {
	case c := <-d.conns:
		return c, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (gatedDialer) Describe() string { return "gated" }

func TestARelayWaitsForASpareBeingDialled(t *testing.T) {
	d := gatedDialer{calls: make(chan struct{}, 4), conns: make(chan net.Conn)}
	r := &Room{Name: "sgg", up: true, Dial: d, T: DefaultTimings()}
	r.gitMoved.Store(time.Now().UnixNano())
	r.keepSpare()
	<-d.calls
	a, b := net.Pipe()
	got := make(chan string, 1)
	go answerRelay(b, got)
	errs := make(chan error, 1)
	go func() {
		_, err := r.Relay(context.Background(), wire.RelayRequest{Op: RelayHub})
		errs <- err
	}()
	select {
	case <-d.calls:
		t.Fatal("the relay dialled its own connection while a spare was being dialled")
	case <-time.After(200 * time.Millisecond): // wait: a negative check, nothing must dial
	}
	d.conns <- a
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	if op := <-got; op != RelayHub {
		t.Fatalf("the spare carried op %q, want %q", op, RelayHub)
	}
	<-d.calls
	r.mu.Lock()
	offered := r.spareTaken != nil
	r.mu.Unlock()
	if !offered {
		t.Fatal("the relay dialled its own connection instead of waiting for the spare being dialled")
	}
	r.gitMoved.Store(0)
	last, _ := net.Pipe()
	d.conns <- last
	for deadline := time.Now().Add(5 * time.Second); r.takeSpare() == nil && time.Now().Before(deadline); {
	}
}
