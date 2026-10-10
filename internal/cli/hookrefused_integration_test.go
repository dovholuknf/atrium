//go:build integration

package cli

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// A REFUSED HOOK IS AN UNREACHABLE HOOK, stage 1 of docs/rnd/security-design.md.
// Whatever the agent listener answers, 401, 403, 500 or a body that is not the
// answer, no hook says anything to Claude Code: the permission falls to its own
// prompt, the turn ends as it would, and nothing else is printed. So no later
// stage can brick a session by refusing it.
//
// AND NO HOOK LOOKS LIKE A BROWSER. The browser edge passes a request carrying
// neither Origin nor Sec-Fetch-Site, which is what keeps every hook working
// under CrossOriginProtection.
func TestARefusedHookIsAnUnreachableHook(t *testing.T) {
	answers := []struct {
		code int
		body string
	}{
		{http.StatusUnauthorized, `{"error":"no"}`},
		{http.StatusForbidden, `{"error":"no"}`},
		{http.StatusInternalServerError, `{"error":"broken"}`},
		{http.StatusOK, `not json {`},
		{http.StatusOK, `{"decision":"approve"`},
	}
	for _, a := range answers {
		var mu sync.Mutex
		var browserish []string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			if r.Header.Get("Origin") != "" || r.Header.Get("Sec-Fetch-Site") != "" {
				browserish = append(browserish, r.URL.Path)
			}
			mu.Unlock()
			// The gate says gated, so the permission hook goes on to ask.
			if r.URL.Path == "/gate" {
				_, _ = w.Write([]byte(`{"gate":true}`))
				return
			}
			w.WriteHeader(a.code)
			_, _ = w.Write([]byte(a.body))
		}))
		permEnv(t, "on")

		if out := permissionHook(srv.URL, []byte(bashPayload), func() int { return 1 }); out != nil {
			t.Errorf("%d %q: the permission hook said %s, want nothing", a.code, a.body, out)
		}
		if out := turnEnded(srv.URL, "end", "tester", "claude"); out != keepGoing {
			t.Errorf("%d %q: the stop hook said %q, want nothing", a.code, a.body, out)
		}
		reportActivity(srv.URL, "pre", "tester")
		_ = reportSession(srv.URL, "start", "tester", "claude")
		srv.Close()
		if len(browserish) > 0 {
			t.Errorf("hooks sent Origin or Sec-Fetch-Site to %v", browserish)
		}
	}
}
