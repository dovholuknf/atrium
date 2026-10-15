//go:build integration

package link

import (
	"net"
	"strings"
	"testing"
	"time"
)

// A PEER THAT NEVER FINISHES ITS HALF cannot hold the connection forever.
func TestSpliceClosesBothAfterTheLinger(t *testing.T) {
	callerEnd, a := tcpPair(t)
	_, b := tcpPair(t)
	res := make(chan spliceResult, 1)
	go func() { res <- splice(a, b, 100*time.Millisecond, nil) }()
	callerEnd.(*net.TCPConn).CloseWrite()
	select {
	case r := <-res:
		if !strings.Contains(r.EndedBy, endLinger) {
			t.Fatalf("ended by %q", r.EndedBy)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a half closed stream outlived its linger")
	}
}
