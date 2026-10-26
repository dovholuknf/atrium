package conn

import (
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// tcpPair is two ends of one real TCP connection, since net.Pipe has no half close.
func tcpPair(t *testing.T) (a, b net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan net.Conn, 1)
	go func() {
		c, _ := ln.Accept()
		got <- c
	}()
	a, err = net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	b = <-got
	t.Cleanup(func() { a.Close(); b.Close() })
	return a, b
}

// HALF CLOSE PASSES THROUGH: the caller finishes writing, the target still answers, and both ends see what the other
// said. This is ssh's own shutdown.
func TestSpliceHalfCloseKeepsTheOtherDirectionOpen(t *testing.T) {
	callerEnd, a := tcpPair(t)
	targetEnd, b := tcpPair(t)
	res := make(chan spliceResult, 1)
	go func() { res <- splice(a, b, 5*time.Second, nil) }()

	if _, err := callerEnd.Write([]byte("request")); err != nil {
		t.Fatal(err)
	}
	callerEnd.(*net.TCPConn).CloseWrite()

	// The target reads the request and then sees the caller's EOF, and only then answers.
	got, err := io.ReadAll(targetEnd)
	if err != nil || string(got) != "request" {
		t.Fatalf("target read %q, %v", got, err)
	}
	if _, err := targetEnd.Write([]byte("reply")); err != nil {
		t.Fatalf("the target could not answer after the caller's half close: %v", err)
	}
	targetEnd.Close()

	back, err := io.ReadAll(callerEnd)
	if err != nil || string(back) != "reply" {
		t.Fatalf("caller read %q, %v", back, err)
	}
	r := <-res
	if r.ToB != 7 || r.FromB != 5 || r.EndedBy != endCaller {
		t.Fatalf("the splice said %+v", r)
	}
}

// STOP ENDS BOTH AT ONCE, which is how remote access being turned off ends a stream.
func TestSpliceStopEndsBothAtOnce(t *testing.T) {
	callerEnd, a := tcpPair(t)
	_, b := tcpPair(t)
	stop := make(chan struct{})
	res := make(chan spliceResult, 1)
	go func() { res <- splice(a, b, time.Hour, stop) }()
	close(stop)
	select {
	case r := <-res:
		if r.EndedBy != endStopped {
			t.Fatalf("ended by %q", r.EndedBy)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stop did not end the splice")
	}
	_ = callerEnd.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := callerEnd.Read(make([]byte, 1)); err == nil {
		t.Fatal("the caller's connection was left open")
	}
}

func TestCleanClaimIsOneBoundedLine(t *testing.T) {
	if got := CleanClaim("claude@sg4\r\nssh-open fake line"); strings.ContainsAny(got, "\r\n") {
		t.Fatalf("a claim carried a line break: %q", got)
	}
	if got := CleanClaim(strings.Repeat("x", 500)); len(got) != 100 {
		t.Fatalf("a claim was %d long", len(got))
	}
}
