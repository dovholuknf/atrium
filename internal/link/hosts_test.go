package link

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/edge"
)

// THE HOSTS ARE A SETTING, applied with no restart: a PUT on the hub reaches
// every Named listener on the next request, and an entry it will not take is
// saved and shown with why.
func TestTheHostsSettingAppliesWithoutARestart(t *testing.T) {
	t.Cleanup(func() { edge.SetExtra(nil) })
	t.Setenv(edge.EnvHosts, "")
	p := NewProxy(NewHub(Timings{}), nil, "", nil)
	st := &fakeSettings{m: map[string]string{}}
	p.SetLaunchCaps(st)
	front := httptest.NewServer(p)
	defer front.Close()

	board := edge.Named(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	status := func(host string) int {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Host = host
		board.ServeHTTP(w, r)
		return w.Code
	}
	if status("abc123.shares.zrok.io") != http.StatusForbidden {
		t.Fatal("a share name was answered before it was set")
	}

	code, got := do(t, http.MethodPut, front.URL+"/_hub/hosts", "application/json",
		`{"hosts":["*.shares.zrok.io"," sg4.lan ","*.duckdns.org",""]}`)
	if code != http.StatusOK {
		t.Fatalf("PUT answered %d %v", code, got)
	}
	if status("abc123.shares.zrok.io") != http.StatusOK || status("sg4.lan:7778") != http.StatusOK {
		t.Fatal("a name just set is not answered")
	}
	if status("evil.duckdns.org") != http.StatusForbidden {
		t.Fatal("a wildcard over a dynamic DNS domain was taken")
	}
	ignored, _ := got["ignored"].([]any)
	if len(ignored) != 1 || !strings.Contains(ignored[0].(map[string]any)["name"].(string), "duckdns") {
		t.Fatalf("the ignored entries are %v", got["ignored"])
	}
	if st.m[SettingExtraHosts] != `["*.shares.zrok.io","sg4.lan","*.duckdns.org"]` {
		t.Fatalf("stored %q", st.m[SettingExtraHosts])
	}

	// A hub starting finds them again.
	edge.SetExtra(nil)
	LoadExtraHosts(st)
	if status("abc123.shares.zrok.io") != http.StatusOK {
		t.Fatal("the stored hosts were not applied at start")
	}

	code, got = do(t, http.MethodGet, front.URL+"/_hub/hosts", "", "")
	if code != http.StatusOK || len(got["hosts"].([]any)) != 3 {
		t.Fatalf("GET answered %d %v", code, got)
	}
}

// Set from the hub's machine only: this list is the rebinding guard.
func TestTheHostsAreSetFromTheHubsMachineOnly(t *testing.T) {
	t.Cleanup(func() { edge.SetExtra(nil) })
	p := NewProxy(NewHub(Timings{}), nil, "", nil)
	st := &fakeSettings{m: map[string]string{}}
	p.SetLaunchCaps(st)
	from := func(method, body string) int {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, "/_hub/hosts", strings.NewReader(body))
		r.RemoteAddr = "192.0.2.7:5555"
		p.ServeHTTP(w, r)
		return w.Code
	}
	if code := from(http.MethodPut, `{"hosts":["*.example.org"]}`); code != http.StatusForbidden {
		t.Errorf("a PUT from elsewhere answered %d, not 403", code)
	}
	if st.m[SettingExtraHosts] != "" {
		t.Errorf("a refused PUT wrote %q", st.m[SettingExtraHosts])
	}
	if code := from(http.MethodGet, ""); code != http.StatusOK {
		t.Errorf("a GET from elsewhere answered %d", code)
	}
}
