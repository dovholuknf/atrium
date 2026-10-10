//go:build integration

package link

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// SETTING OR TESTING THE NOTIFY COMMAND FROM OFF THE MACHINE IS REFUSED (f-024),
// before the body is read and before anything runs. Reading the setting and
// presence stay open, because the phone board does both over an overlay.
func TestNotifyCommandIsLoopbackOnly(t *testing.T) {
	n, _, _ := armed(t)
	p := NewProxy(NewHub(Timings{}), nil, "", nil)
	p.SetNotify(n)
	before := n.Status()

	from := func(method, path, body string) int {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.RemoteAddr = "192.0.2.7:5555"
		p.ServeHTTP(w, r)
		return w.Code
	}
	if code := from(http.MethodPut, "/_hub/notify", `{"enabled":false,"command":["x"]}`); code != http.StatusForbidden {
		t.Errorf("a PUT from elsewhere answered %d, not 403", code)
	}
	if code := from(http.MethodPost, "/_hub/notify/test", ""); code != http.StatusForbidden {
		t.Errorf("a test from elsewhere answered %d, not 403", code)
	}
	if after := n.Status(); after.Enabled != before.Enabled {
		t.Errorf("a refused PUT changed the setting: %+v to %+v", before, after)
	}
	if code := from(http.MethodGet, "/_hub/notify", ""); code != http.StatusOK {
		t.Errorf("a GET from elsewhere answered %d, not 200", code)
	}
	if code := from(http.MethodPost, "/_hub/presence", `{"visible":true,"tab":"t1"}`); code != http.StatusOK {
		t.Errorf("presence from elsewhere answered %d, not 200", code)
	}
}
