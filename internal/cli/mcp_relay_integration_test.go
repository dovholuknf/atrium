//go:build integration

package cli

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The hub is down for the first request, then comes back on the same address. The
// relay answers once it is up, and sends the identity headers.
func TestMCPRelayRidesOutAHubRestart(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close() // the hub is down

	var out bytes.Buffer
	r := &mcpRelay{
		url: "http://" + addr + "/_hub/mcp", client: &http.Client{}, retryFor: 10 * time.Second,
		backoff: 20 * time.Millisecond, cap: 50 * time.Millisecond,
		headers: map[string]string{"X-Atrium-Agent": "w1", "X-Atrium-Room": "m1"},
		out:     &out,
	}
	var gotAgent, gotRoom atomic.Value
	go func() {
		time.Sleep(300 * time.Millisecond)
		l, err := net.Listen("tcp", addr)
		if err != nil {
			return
		}
		_ = http.Serve(l, http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
			gotAgent.Store(q.Header.Get("X-Atrium-Agent"))
			gotRoom.Store(q.Header.Get("X-Atrium-Room"))
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"ok\":true}}\n\n"))
		}))
	}()

	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n")
	if err := r.run(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != `{"jsonrpc":"2.0","id":1,"result":{"ok":true}}` {
		t.Fatalf("out = %q", got)
	}
	if gotAgent.Load() != "w1" || gotRoom.Load() != "m1" {
		t.Fatalf("headers = %v %v", gotAgent.Load(), gotRoom.Load())
	}
}

// A hub that stays down answers the caller with a JSON-RPC error, not silence.
func TestMCPRelayGivesUpWithAnError(t *testing.T) {
	var out bytes.Buffer
	r := &mcpRelay{
		url: "http://127.0.0.1:1/_hub/mcp", client: &http.Client{}, retryFor: 100 * time.Millisecond,
		backoff: 20 * time.Millisecond, cap: 20 * time.Millisecond, out: &out,
	}
	_ = r.run(context.Background(), strings.NewReader(`{"jsonrpc":"2.0","id":7,"method":"tools/call"}`+"\n"))
	if !strings.Contains(out.String(), `"id":7`) || !strings.Contains(out.String(), "unreachable") {
		t.Fatalf("out = %q", out.String())
	}
}
