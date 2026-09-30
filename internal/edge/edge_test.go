package edge

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

var ok = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

func status(h http.Handler, method, host string, headers map[string]string) int {
	req := httptest.NewRequest(method, "http://"+host+"/v1/launch", nil)
	req.Host = host
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

// THE AUDIT'S BROWSER PROBES: a cross-site write, a rebound Host and a foreign
// websocket are 403, and the same requests the way a hook or curl sends them
// answer as before.
func TestTheBrowserEdge(t *testing.T) {
	h := Loopback(ok)
	cases := []struct {
		name    string
		method  string
		host    string
		headers map[string]string
		want    int
	}{
		{"a hook or curl", http.MethodPost, "127.0.0.1:7777", nil, 200},
		{"localhost", http.MethodPost, "localhost:7778", nil, 200},
		{"ipv6 loopback", http.MethodPost, "[::1]:7778", nil, 200},
		{"the board itself", http.MethodPost, "127.0.0.1:7778",
			map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "http://127.0.0.1:7778"}, 200},
		{"a cross-site write", http.MethodPost, "127.0.0.1:7778",
			map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"}, 403},
		{"a cross-site write with only Origin", http.MethodPost, "127.0.0.1:7778",
			map[string]string{"Origin": "https://evil.example"}, 403},
		{"another port is another origin", http.MethodPost, "127.0.0.1:7778",
			map[string]string{"Sec-Fetch-Site": "same-site", "Origin": "http://127.0.0.1:7781"}, 403},
		{"a cross-site read is a read", http.MethodGet, "127.0.0.1:7778",
			map[string]string{"Sec-Fetch-Site": "cross-site"}, 200},
		{"a rebound host", http.MethodGet, "evil.example:7778", nil, 403},
		{"a rebound host to a lan name", http.MethodPost, "sg4:7778", nil, 403},
		{"a foreign websocket", http.MethodGet, "127.0.0.1:7778",
			map[string]string{"Upgrade": "websocket", "Connection": "Upgrade", "Origin": "https://evil.example"}, 403},
		{"the board's own websocket", http.MethodGet, "127.0.0.1:7778",
			map[string]string{"Upgrade": "websocket", "Connection": "Upgrade", "Origin": "http://127.0.0.1:7778"}, 200},
		{"a websocket with no Origin", http.MethodGet, "127.0.0.1:7778",
			map[string]string{"Upgrade": "websocket", "Connection": "Upgrade"}, 200},
	}
	for _, c := range cases {
		if got := status(h, c.method, c.host, c.headers); got != c.want {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}
}

// A SHARE NAMES ITS OWN HOST, so a shared listener checks origins and not hosts.
func TestASharedListenerTakesAnyHost(t *testing.T) {
	h := Shared(ok)
	if got := status(h, http.MethodPost, "board.share.zrok.io", map[string]string{
		"Sec-Fetch-Site": "same-origin", "Origin": "https://board.share.zrok.io"}); got != 200 {
		t.Fatalf("the board over a share answered %d", got)
	}
	if got := status(h, http.MethodPost, "board.share.zrok.io", map[string]string{
		"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"}); got != 403 {
		t.Fatalf("a cross-site write over a share answered %d", got)
	}
}

// For CHOOSES BY THE ADDRESS: a loopback one checks the Host, a wide one does not.
func TestForChoosesByAddress(t *testing.T) {
	for addr, want := range map[string]int{"127.0.0.1:7781": 403, "localhost:7781": 403, "0.0.0.0:7781": 200, ":7781": 200} {
		if got := status(For(addr, ok), http.MethodGet, "sg4:7781", nil); got != want {
			t.Errorf("%s with Host sg4 answered %d, want %d", addr, got, want)
		}
	}
}

// ViaLink IS SET BY THE LINK'S WRAPPER AND NOTHING ELSE.
func TestViaLink(t *testing.T) {
	var seen bool
	h := MarkLink(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen = ViaLink(r) }))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if !seen {
		t.Fatal("a request through MarkLink is not ViaLink")
	}
	if ViaLink(httptest.NewRequest(http.MethodGet, "/", nil)) {
		t.Fatal("a plain request is ViaLink")
	}
}
