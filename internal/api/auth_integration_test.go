//go:build integration

package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// PUT /v1/auth is for the operator on the machine. Anybody signed in to a
// published board reaches this route, and could otherwise turn the login off.
func TestPutAuthIsLoopbackOperatorOnly(t *testing.T) {
	saved := 0
	s := &Server{
		SaveAuth:   func([]byte) error { saved++; return nil },
		AuthConfig: func() any { return map[string]any{} },
	}
	put := func(remote, host string, hdr map[string]string) int {
		r := httptest.NewRequest(http.MethodPut, "/v1/auth", strings.NewReader(`{"enabled":false}`))
		r.RemoteAddr, r.Host = remote, host
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		s.putAuth(w, r)
		return w.Code
	}
	if got := put("127.0.0.1:5000", "127.0.0.1:7777", nil); got != http.StatusOK {
		t.Fatalf("the operator was refused: %d", got)
	}
	if got := put("198.51.100.4:5000", "127.0.0.1:7777", nil); got != http.StatusForbidden {
		t.Fatalf("a remote caller got %d", got)
	}
	if got := put("127.0.0.1:5000", "board.example", nil); got != http.StatusForbidden {
		t.Fatalf("a proxied public Host got %d", got)
	}
	if got := put("127.0.0.1:5000", "127.0.0.1:7777",
		map[string]string{"X-Forwarded-For": "203.0.113.9"}); got != http.StatusForbidden {
		t.Fatalf("a forwarded request got %d", got)
	}
	if saved != 1 {
		t.Fatalf("SaveAuth ran %d times, wanted 1", saved)
	}
}
