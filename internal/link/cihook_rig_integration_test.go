//go:build integration

package link

import (
	forgepkg "github.com/dovholuknf/atrium/internal/link/forge"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

func TestTheLoopbackRouteIsForTheLocalOperatorOnly(t *testing.T) {
	x := newCIRig(t)
	p := NewProxy(NewHub(Timings{}), nil, "", nil)
	p.SetCIHook(x.hook)
	post := func(remote, host string, hdr map[string]string) int {
		r := httptest.NewRequest(http.MethodPost, "/_hub/ci-hook", strings.NewReader(ciBody("o/r", 1, 1, "CI", "main", "success")))
		r.RemoteAddr, r.Host = remote, host
		r.Header.Set(forgepkg.CISecretHeader, x.secret)
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		p.ServeHTTP(w, r)
		return w.Code
	}
	if c := post("127.0.0.1:5000", "127.0.0.1:7778", nil); c != http.StatusAccepted {
		t.Fatalf("local: %d", c)
	}
	if c := post("127.0.0.1:5000", "hub.example.com", nil); c != http.StatusForbidden {
		t.Fatalf("a proxied host: %d", c)
	}
	if c := post("127.0.0.1:5000", "127.0.0.1:7778", map[string]string{"X-Forwarded-For": "1.2.3.4"}); c != http.StatusForbidden {
		t.Fatalf("a forwarded request: %d", c)
	}
	if c := post("10.0.0.5:5000", "127.0.0.1:7778", nil); c != http.StatusForbidden {
		t.Fatalf("a remote caller: %d", c)
	}
}

func TestPuttingRoutesValidatesAndStores(t *testing.T) {
	x := newCIRig(t)
	p := NewProxy(NewHub(Timings{}), nil, "", nil)
	p.SetCIHook(x.hook)
	put := func(body string) (int, string) {
		r := httptest.NewRequest(http.MethodPut, "/_hub/ci-hook/routes", strings.NewReader(body))
		r.RemoteAddr, r.Host = "127.0.0.1:5000", "127.0.0.1:7778"
		w := httptest.NewRecorder()
		p.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	if c, b := put(`[{"repo":"o/r","to":"qa"},{"repo":"bad","to":"qa"}]`); c != http.StatusBadRequest || !strings.Contains(b, "rule 2") {
		t.Fatalf("a bad rule: %d %s", c, b)
	}
	if raw, _ := x.st.CIHookRoutes(); raw != "" {
		t.Fatalf("a refused list was stored: %s", raw)
	}
	if c, b := put(`{"routes":[{"repo":"o/r","to":"qa","wake":true}],"repos":["o/r"]}`); c != http.StatusOK {
		t.Fatalf("a good list: %d %s", c, b)
	}
	routes, _ := x.st.CIHookRoutes()
	if rs, err := ParseCIRoutes(routes); err != nil || len(rs) != 1 || !rs[0].Wake {
		t.Fatalf("stored %q: %v %v", routes, rs, err)
	}
	if repos, _ := x.st.CIHookRepos(); len(repos) != 1 || repos[0] != "o/r" {
		t.Fatalf("repos %v", repos)
	}
	r := httptest.NewRequest(http.MethodPut, "/_hub/ci-hook/routes", strings.NewReader(`[]`))
	r.RemoteAddr, r.Host = "10.0.0.5:5000", "127.0.0.1:7778"
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("a remote PUT: %d", w.Code)
	}
}

func TestACIRunReachesTheCardOnARoomThroughTheHubsOwnSayPath(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	st, err := hubstore.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	secret, _ := st.MintCIHookSecret(false)
	hook := NewCIHook(st, x.proxy.Deliverer(), nil)
	x.proxy.SetCIHook(hook)
	if err := st.SetCIHookRoutes(`[{"repo":"o/r","to":"@orch","kind":"needs"}]`); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodPost, x.board+"/_hub/ci-hook", strings.NewReader(ciBody("o/r", 9, 1, "CI", "main", "failure")))
	req.Header.Set(forgepkg.CISecretHeader, secret)
	res, err := http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != http.StatusAccepted {
		t.Fatalf("post: %v %v", res, err)
	}
	res.Body.Close()
	hook.Pass(relayCtx(t))
	got := x.sg4.messages()
	if len(got) != 1 || got[0]["id"] != "s1" || got[0]["from"] != "ci@hub" || got[0]["kind"] != "needs" ||
		!strings.HasPrefix(got[0]["text"], "CI failure: o/r CI on main") || !strings.HasSuffix(got[0]["text"], forgepkg.CISignoff) {
		t.Fatalf("sg4 got %+v", got)
	}
}

func TestAParkedCardIsPendingUntilARouteWakesIt(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	x.sg4.mu.Lock()
	x.sg4.parked = map[string]bool{"s1": true}
	x.sg4.mu.Unlock()
	st, err := hubstore.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	secret, _ := st.MintCIHookSecret(false)
	hook := NewCIHook(st, x.proxy.Deliverer(), nil)
	x.proxy.SetCIHook(hook)
	_ = st.SetCIHookRoutes(`[{"repo":"o/r","to":"@orch"}]`)
	req, _ := http.NewRequest(http.MethodPost, x.board+"/_hub/ci-hook", strings.NewReader(ciBody("o/r", 9, 1, "CI", "main", "failure")))
	req.Header.Set(forgepkg.CISecretHeader, secret)
	res, err := http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != http.StatusAccepted {
		t.Fatalf("post: %v %v", res, err)
	}
	res.Body.Close()
	hook.Pass(relayCtx(t))
	if rows, _ := st.CIDeliveriesPending(); len(rows) != 1 {
		t.Fatalf("a parked card with wake off should stay pending, got %d", len(rows))
	}
	if len(x.sg4.messages()) != 0 {
		t.Fatal("a message reached a parked card")
	}
	_ = st.SetCIHookRoutes(`[{"repo":"o/r","to":"@orch","wake":true}]`)
	req, _ = http.NewRequest(http.MethodPost, x.board+"/_hub/ci-hook", strings.NewReader(ciBody("o/r", 10, 1, "CI", "main", "failure")))
	req.Header.Set(forgepkg.CISecretHeader, secret)
	res, err = http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != http.StatusAccepted {
		t.Fatalf("second post: %v %v", res, err)
	}
	res.Body.Close()
	hook.Pass(relayCtx(t))
	if rs := x.sg4.resumes(); len(rs) != 1 || rs[0] != "s1" {
		t.Fatalf("resumed %v, want s1", rs)
	}
	if got := x.sg4.messages(); len(got) != 1 {
		t.Fatalf("sg4 got %+v, want the newer run only", got)
	}
	if rows, _ := st.CIDeliveriesPending(); len(rows) != 0 {
		t.Fatalf("%d still pending", len(rows))
	}
}
