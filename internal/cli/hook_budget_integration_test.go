//go:build integration

package cli

import (
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func silentListener(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var conns []net.Conn
	done := make(chan struct{})
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			conns = append(conns, c)
		}
	}()
	t.Cleanup(func() {
		close(done)
		ln.Close()
		for _, c := range conns {
			c.Close()
		}
	})
	return "http://" + ln.Addr().String()
}

func closedAddress(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return "http://" + addr
}

func TestEveryHookFailsOpenInsideItsBudget(t *testing.T) {
	permEnv(t, "force")
	hooks := []struct {
		name   string
		budget time.Duration
		run    func(url string) string
	}{
		{"activity", 1500 * time.Millisecond, func(u string) string { reportActivity(u, "tool-start", "tester"); return "" }},
		{"permission", 3500 * time.Millisecond, func(u string) string {
			return string(permissionHook(u, []byte(bashPayload), func() int { return 1 }))
		}},
		{"session", 3500 * time.Millisecond, func(u string) string {
			_ = reportSession(u, "start", "tester", "claude")
			return ""
		}},
		{"turn", 2500 * time.Millisecond, func(u string) string { return turnEnded(u, "end", "tester", "claude") }},
	}
	targets := map[string]string{"silent": silentListener(t), "nothing listening": closedAddress(t)}
	for _, h := range hooks {
		for kind, url := range targets {
			start := time.Now()
			out := h.run(url)
			if took := time.Since(start); took > h.budget {
				t.Errorf("%s hook against %s took %s, budget %s", h.name, kind, took, h.budget)
			}
			if out != "" && out != keepGoing {
				t.Errorf("%s hook against %s printed %q, want nothing", h.name, kind, out)
			}
		}
	}
}

func TestPermissionHookStopsRetryingAReplyThatKeepsDying(t *testing.T) {
	permEnv(t, "force")
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gate" {
			_, _ = w.Write([]byte(`{"gate":true}`))
			return
		}
		posts.Add(1)
		hj, _ := w.(http.Hijacker)
		c, _, _ := hj.Hijack()
		c.Close()
	}))
	t.Cleanup(srv.Close)

	start := time.Now()
	if out := permissionHook(srv.URL, []byte(bashPayload), func() int { return 1 }); out != nil {
		t.Fatalf("said %s, want nothing", out)
	}
	if took := time.Since(start); took > 7*time.Second {
		t.Fatalf("retried for %s over %d posts", took, posts.Load())
	}
}
