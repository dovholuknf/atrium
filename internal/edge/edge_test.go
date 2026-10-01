package edge

import (
	"net/http"
	"net/http/httptest"
	"os"
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

// A SHARE ANSWERS ITS OWN NAME AND NO OTHER. A rebound page is same origin, its
// Origin and Host both the attacker's, so only the Host check stops it.
func TestASharedListenerAnswersOnlyItsNames(t *testing.T) {
	h := Named(ok, "https://board.share.zrok.io")
	if got := status(h, http.MethodPost, "board.share.zrok.io", map[string]string{
		"Sec-Fetch-Site": "same-origin", "Origin": "https://board.share.zrok.io"}); got != 200 {
		t.Fatalf("the board over its share answered %d", got)
	}
	if got := status(h, http.MethodPost, "127.0.0.1:9191", nil); got != 200 {
		t.Fatalf("a private access proxy on loopback answered %d", got)
	}
	if got := status(h, http.MethodPost, "evil.example:7778", map[string]string{
		"Sec-Fetch-Site": "same-origin", "Origin": "http://evil.example:7778"}); got != 403 {
		t.Fatalf("a rebound page over a share answered %d", got)
	}
	if got := status(h, http.MethodPost, "board.share.zrok.io", map[string]string{
		"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"}); got != 403 {
		t.Fatalf("a cross-site write over a share answered %d", got)
	}
}

// $ATRIUM_HOSTS ADDS A NAME to every listener.
func TestEnvHostsAddsNames(t *testing.T) {
	t.Setenv(EnvHosts, "sg4.lan, 10.0.0.9")
	h := Named(ok)
	for host, want := range map[string]int{"sg4.lan:7778": 200, "10.0.0.9": 200, "evil.example": 403} {
		if got := status(h, http.MethodGet, host, nil); got != want {
			t.Errorf("%s answered %d, want %d", host, got, want)
		}
	}
}

// A WILDCARD ANSWERS EVERY NAME UNDER A DOMAIN, for a share frontend that
// hands out a random name per share, and never the domain alone or a lookalike.
func TestEnvHostsWildcard(t *testing.T) {
	t.Setenv(EnvHosts, "*.shares.zrok.io, *.com")
	h := Named(ok)
	for host, want := range map[string]int{
		"hdzujxlq0dan.shares.zrok.io": 200,
		"atrium.shares.zrok.io:443":   200,
		"a.b.shares.zrok.io":          200,
		"shares.zrok.io":              403,
		"evilshares.zrok.io":          403,
		"shares.zrok.io.evil.example": 403,
		"example.com":                 403, // a wildcard over a bare label is ignored
	} {
		if got := status(h, http.MethodGet, host, nil); got != want {
			t.Errorf("%s answered %d, want %d", host, got, want)
		}
	}
}

// A WILDCARD OVER A PUBLIC SUFFIX IS IGNORED, because anybody can own a name
// under one and point it anywhere: a dynamic DNS domain, a second-level country
// domain, a pages host. Listed exactly, a name under one still works.
func TestAWildcardOverAPublicSuffixIsIgnored(t *testing.T) {
	t.Setenv(EnvHosts, "*.duckdns.org, *.co.uk, *.github.io, me.duckdns.org")
	h := Named(ok)
	for host, want := range map[string]int{
		"evil.duckdns.org": 403,
		"evil.co.uk":       403,
		"evil.github.io":   403,
		"me.duckdns.org":   200,
	} {
		if got := status(h, http.MethodGet, host, nil); got != want {
			t.Errorf("%s answered %d, want %d", host, got, want)
		}
	}
	ig := CheckNames([]string{"*.duckdns.org", "*.shares.zrok.io", "*.", "a*b.com"})
	if len(ig) != 3 || ig[0].Name != "*.duckdns.org" {
		t.Fatalf("ignored %+v", ig)
	}
}

// SetExtra reaches a listener built before it, from the next request.
func TestSetExtraReachesEveryNamedListener(t *testing.T) {
	t.Cleanup(func() { SetExtra(nil) })
	t.Setenv(EnvHosts, "")
	h := Named(ok)
	if status(h, http.MethodGet, "abc.shares.zrok.io", nil) != 403 {
		t.Fatal("answered before it was set")
	}
	SetExtra([]string{"*.shares.zrok.io"})
	if status(h, http.MethodGet, "abc.shares.zrok.io", nil) != 200 {
		t.Fatal("not answered after SetExtra")
	}
	SetExtra(nil)
	if status(h, http.MethodGet, "abc.shares.zrok.io", nil) != 403 {
		t.Fatal("still answered after the setting was cleared")
	}
}

// UNNAMED CHECKS ORIGINS, NOT HOSTS, for a ziti service nobody named.
func TestUnnamedChecksOriginsOnly(t *testing.T) {
	h := Unnamed(ok)
	if got := status(h, http.MethodGet, "board.ziti", nil); got != 200 {
		t.Fatalf("an unnamed listener refused its host: %d", got)
	}
	if got := status(h, http.MethodPost, "board.ziti", map[string]string{
		"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"}); got != 403 {
		t.Fatalf("an unnamed listener let a cross-site write through: %d", got)
	}
}

// For CHOOSES BY THE ADDRESS: loopback names, the bound address, or every name
// this machine has for a bind on every interface. Never any name at all.
func TestForChoosesByAddress(t *testing.T) {
	host, _ := os.Hostname()
	cases := []struct {
		addr, host string
		want       int
	}{
		{"127.0.0.1:7781", "sg4:7781", 403},
		{"localhost:7781", "localhost:7781", 200},
		{"192.168.1.68:7781", "192.168.1.68:7781", 200},
		{"192.168.1.68:7781", "evil.example:7781", 403},
		{"0.0.0.0:7781", host + ":7781", 200},
		{":7781", "evil.example:7781", 403},
	}
	for _, c := range cases {
		if got := status(For(c.addr, ok), http.MethodGet, c.host, nil); got != c.want {
			t.Errorf("%s with Host %s answered %d, want %d", c.addr, c.host, got, c.want)
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
