package edge

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The machine's own user: loopback at the far end, a loopback Host, no
// forwarding header. Anything else, and a local proxy above all, is not.
func TestLocalOperator(t *testing.T) {
	req := func(remote, host string, hdr ...string) *http.Request {
		r := httptest.NewRequest(http.MethodPut, "/_hub/hosts", nil)
		r.RemoteAddr, r.Host = remote, host
		for i := 0; i+1 < len(hdr); i += 2 {
			r.Header.Set(hdr[i], hdr[i+1])
		}
		return r
	}
	for _, c := range []struct {
		name string
		r    *http.Request
		want bool
	}{
		{"a browser on this machine", req("127.0.0.1:5555", "127.0.0.1:7778"), true},
		{"localhost", req("127.0.0.1:5555", "localhost:7778"), true},
		{"ipv6 loopback", req("[::1]:5555", "[::1]:7778"), true},
		{"the CLI with no port", req("127.0.0.1:5555", "127.0.0.1"), true},
		{"another machine", req("203.0.113.7:5555", "127.0.0.1:7778"), false},
		{"zrok share", req("127.0.0.1:5555", "atrium.shares.zrok.io", "X-Forwarded-For", "198.51.100.4", "X-Proxy", "zrok"), false},
		{"a proxy that keeps the Host", req("127.0.0.1:5555", "127.0.0.1:7778", "X-Forwarded-For", "198.51.100.4"), false},
		{"X-Proxy alone", req("127.0.0.1:5555", "127.0.0.1:7778", "X-Proxy", "zrok"), false},
		{"Forwarded", req("127.0.0.1:5555", "127.0.0.1:7778", "Forwarded", "for=198.51.100.4"), false},
		{"X-Real-Ip", req("127.0.0.1:5555", "127.0.0.1:7778", "X-Real-Ip", "198.51.100.4"), false},
		{"a public Host from loopback", req("127.0.0.1:5555", "sg4.lan:7778"), false},
		{"no port on the remote", req("127.0.0.1", "127.0.0.1"), false},
	} {
		if got := LocalOperator(c.r); got != c.want {
			t.Errorf("%s: LocalOperator = %v, want %v", c.name, got, c.want)
		}
	}
	if !strings.Contains(ProxyNote(req("127.0.0.1:5555", "x.shares.zrok.io", "X-Proxy", "zrok")), "proxy") {
		t.Error("a share's refusal does not say a proxy is why")
	}
	if ProxyNote(req("203.0.113.7:5555", "127.0.0.1:7778")) != "" {
		t.Error("a plain remote refusal blamed a proxy")
	}
}
