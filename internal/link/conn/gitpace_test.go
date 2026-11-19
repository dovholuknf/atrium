package conn

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/link/wire"
)

func TestGitWritesYieldWhileAControlDialWaits(t *testing.T) {
	var slept []time.Duration
	r := &Room{}
	done := r.controlDialing()
	r.yieldSleep = func(d time.Duration) {
		slept = append(slept, d)
		if len(slept) == 3 {
			done()
		}
	}
	r.yieldToControl()
	if len(slept) != 3 {
		t.Fatalf("a git write held back %d times, want 3: it must wait until the control dial returns", len(slept))
	}
	r.yieldToControl()
	if len(slept) != 3 {
		t.Fatalf("a git write held back with no control dial waiting")
	}
}

func TestGitWriteYieldIsBounded(t *testing.T) {
	var total time.Duration
	r := &Room{yieldSleep: func(d time.Duration) { total += d }}
	r.controlDialing()
	r.yieldToControl()
	if total != gitYieldMax {
		t.Fatalf("a git write waited %v for a dial that never returned, want %v", total, gitYieldMax)
	}
}

func TestAGitWriteHeldInTheYieldIsNotAnIdleRoom(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	go func() { _, _ = io.Copy(io.Discard, b) }()
	r := &Room{}
	r.pace.lastAt = time.Now()
	done := r.controlDialing()
	var held float64
	r.yieldSleep = func(time.Duration) {
		if held != 0 {
			return
		}
		r.pace.mu.Lock()
		r.pace.rate = paceFloor
		r.pace.mu.Unlock()
		r.pace.sample(10*time.Millisecond, time.Now().Add(2*paceBeat))
		held = r.pace.rate
		done()
	}
	c := movingConn{&buffered{Conn: a, br: bufio.NewReader(a)}, r, new(int64)}
	if _, err := c.Write([]byte("push")); err != nil {
		t.Fatal(err)
	}
	if held == 0 {
		t.Fatal("a git write held in the yield for a control dial was read as an idle room, so the pace was dropped")
	}
}

type pipeDialer struct{ c net.Conn }

func (d pipeDialer) Dial(context.Context) (net.Conn, error) { return d.c, nil }
func (pipeDialer) Describe() string                         { return "pipe" }

func TestARelayHoldsTheYieldUntilItsReply(t *testing.T) {
	a, b := net.Pipe()
	r := &Room{Name: "sgg", up: true, Dial: pipeDialer{a}, T: DefaultTimings()}
	held := make(chan int32, 1)
	go func() {
		defer b.Close()
		dec := json.NewDecoder(b)
		var v json.RawMessage
		_ = dec.Decode(&v)
		_ = json.NewEncoder(b).Encode(map[string]any{"ok": true})
		_ = dec.Decode(&v)
		held <- r.ctlDialing.Load()
		_ = json.NewEncoder(b).Encode(map[string]any{"ok": true})
	}()
	if _, err := r.Relay(context.Background(), wire.RelayRequest{}); err != nil {
		t.Fatal(err)
	}
	if n := <-held; n == 0 {
		t.Fatal("the yield was released before the relay's reply")
	}
	if n := r.ctlDialing.Load(); n != 0 {
		t.Fatalf("the yield was still held after the relay returned: %d", n)
	}
}
