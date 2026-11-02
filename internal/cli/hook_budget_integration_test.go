//go:build integration

package cli

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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
		{"activity", 1500 * time.Millisecond, func(u string) string { return reportActivity(u, "tool-start", "tester") }},
		{"permission", 2500 * time.Millisecond, func(u string) string {
			return string(permissionHook(u, []byte(bashPayload), func() int { return 1 }))
		}},
		{"session", 2500 * time.Millisecond, func(u string) string { return reportSession(u, "start", "tester", "claude").warn }},
		{"turn", 2500 * time.Millisecond, func(u string) string { return turnEnded(u, "end", "tester", "claude") }},
	}
	targets := map[string]string{"silent": silentListener(t), "nothing listening": closedAddress(t)}
	var wg sync.WaitGroup
	for _, h := range hooks {
		for kind, url := range targets {
			wg.Add(1)
			go func() {
				defer wg.Done()
				start := time.Now()
				out := h.run(url)
				if took := time.Since(start); took > h.budget {
					t.Errorf("%s hook against %s took %s, budget %s", h.name, kind, took, h.budget)
				}
				if !strings.Contains(out, "could not reach atrium at "+url) || !strings.Contains(out, "atrium run") {
					t.Errorf("%s hook against %s printed %q, want the address and what to do", h.name, kind, out)
				}
				if strings.Contains(out, "permissionDecision") || strings.Contains(out, `"decision"`) {
					t.Errorf("%s hook against %s decided something: %q", h.name, kind, out)
				}
			}()
		}
	}
	wg.Wait()
}

func TestPermissionHookStopsRetryingAReplyThatKeepsDying(t *testing.T) {
	permEnv(t, "force")
	t.Setenv("ATRIUM_PERM_RETRY_FOR", "400ms")
	t.Setenv("ATRIUM_PERM_RETRY_EVERY", "100ms")
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
	if out := permissionHook(srv.URL, []byte(bashPayload), func() int { return 1 }); !strings.Contains(string(out), "systemMessage") {
		t.Fatalf("said %s, want the warning", out)
	}
	if took := time.Since(start); took > 7*time.Second {
		t.Fatalf("retried for %s over %d posts", took, posts.Load())
	}
}

// additionalContext only where claude takes it, and never for codex.
func TestHookWarningTellsTheModelOnlyWhereItCan(t *testing.T) {
	for _, c := range []struct {
		event, runner string
		model         bool
	}{
		{"PreToolUse", "", true}, {"SessionStart", "claude", true}, {"PreToolUse", "codex", false},
		{"Stop", "", false}, {"SessionEnd", "", false}, {"", "", false},
	} {
		out := string(hookWarning(c.event, c.runner, "down"))
		if !strings.Contains(out, `"systemMessage":"down"`) || strings.Contains(out, "additionalContext") != c.model {
			t.Errorf("%s %q: %s", c.event, c.runner, out)
		}
	}
}
