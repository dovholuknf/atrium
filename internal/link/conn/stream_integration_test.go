//go:build integration

package conn

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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

// A room's own loopback board is not where the route is served: only the link handler has it.
func TestTheRoomsOwnBoardHasNoStreamRoute(t *testing.T) {
	var seen atomic.Int64
	r := &Room{Name: "x", Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { seen.Add(1) })}
	rec := httptest.NewRecorder()
	r.linkHandler().ServeHTTP(rec, httptest.NewRequest("GET", "/_link/other", nil))
	if rec.Code != http.StatusNotFound || seen.Load() != 0 {
		t.Fatalf("%d, the board handler was asked %d times", rec.Code, seen.Load())
	}
}
