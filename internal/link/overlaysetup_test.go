package link

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProxyRefusesOverlaySetupAndTeardown(t *testing.T) {
	p := &Proxy{}
	for _, path := range []string{"/v1/overlays/zrok/setup", "/v1/overlays/ziti/teardown"} {
		for _, c := range []struct{ name, remote, host, fwd string }{
			{"loopback", "127.0.0.1:50000", "127.0.0.1:7778", ""},
			{"forwarded", "127.0.0.1:50000", "127.0.0.1:7778", "1.2.3.4"},
			{"non-loopback", "10.0.0.5:50000", "10.0.0.5:7778", ""},
		} {
			t.Run(path+"/"+c.name, func(t *testing.T) {
				r := httptest.NewRequest("POST", path, nil)
				r.RemoteAddr, r.Host = c.remote, c.host
				if c.fwd != "" {
					r.Header.Set("X-Forwarded-For", c.fwd)
				}
				w := httptest.NewRecorder()
				p.ServeHTTP(w, r)
				if w.Code != http.StatusForbidden {
					t.Fatalf("%d %s", w.Code, w.Body.String())
				}
			})
		}
	}
}
