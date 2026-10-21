package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOverlaySetupAndTeardownAreLocalOperatorOnly(t *testing.T) {
	s := &Server{
		bus:             newBus(),
		SetupOverlay:    func(string, []byte) (string, error) { return "ok", nil },
		TeardownOverlay: func(string) (string, error) { return "ok", nil },
	}
	cases := []struct {
		name   string
		remote string
		host   string
		header string
		want   int
	}{
		{"loopback", "127.0.0.1:50000", "127.0.0.1:7781", "", http.StatusOK},
		{"forwarded", "127.0.0.1:50000", "127.0.0.1:7781", "1.2.3.4", http.StatusForbidden},
		{"non-loopback", "10.0.0.5:50000", "10.0.0.5:7781", "", http.StatusForbidden},
	}
	routes := map[string]http.HandlerFunc{"setup": s.setupOverlay, "teardown": s.teardownOverlay}
	for route, h := range routes {
		for _, c := range cases {
			t.Run(route+"/"+c.name, func(t *testing.T) {
				r := httptest.NewRequest("POST", "/v1/overlays/zrok/"+route, strings.NewReader("{}"))
				r.RemoteAddr = c.remote
				r.Host = c.host
				if c.header != "" {
					r.Header.Set("X-Forwarded-For", c.header)
				}
				w := httptest.NewRecorder()
				h(w, r)
				if w.Code != c.want {
					t.Fatalf("%d %s", w.Code, w.Body.String())
				}
			})
		}
	}
}
