//go:build integration

package link

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const hubList = "version: 3\ncaps:\n  - {id: ssh:extra, kind: reach, probe: [ssh, extra, \"true\"], needs: [ssh]}\n"

func TestTheHubsCapabilityListIsSavedLintedAndVersioned(t *testing.T) {
	p := NewProxy(NewHub(Timings{}), nil, "", nil)
	st := &fakeSettings{m: map[string]string{}}
	p.SetLaunchCaps(st)
	front := httptest.NewServer(p)
	defer front.Close()
	put := func(y string) int {
		code, _ := do(t, http.MethodPut, front.URL+"/_hub/capability-list", "application/json", `{"yaml":`+quote(y)+`}`)
		return code
	}

	if code, got := do(t, http.MethodGet, front.URL+"/_hub/capability-list", "", ""); code != http.StatusOK || got["version"] != float64(0) {
		t.Fatalf("an empty hub answered %d %v", code, got)
	}
	if code := put(hubList); code != http.StatusOK {
		t.Fatalf("a good list answered %d", code)
	}
	if code, got := do(t, http.MethodGet, front.URL+"/_hub/capability-list", "", ""); code != http.StatusOK || got["version"] != float64(3) || got["hash"] == "" {
		t.Fatalf("GET after the save answered %d %v", code, got)
	}
	if code := put(hubList); code != http.StatusConflict {
		t.Errorf("the same version again answered %d, not 409", code)
	}
	for name, y := range map[string]string{
		"need":  "version: 9\ncaps: [{id: a, kind: reach, probe: [x], needs: [nope]}]",
		"kind":  "version: 9\ncaps: [{id: a, kind: weird, probe: [x]}]",
		"shell": "version: 9\ncaps: [{id: a, kind: reach, probe: [x], pass: shell}]",
	} {
		if code := put(y); code != http.StatusBadRequest {
			t.Errorf("%s answered %d, not 400", name, code)
		}
	}
	if got := st.m[SettingCapabilityList]; got != hubList {
		t.Errorf("a refused save changed the list: %q", got)
	}
}

func TestTheHubsCapabilityListIsSavedFromItsMachineOnlyAndRoomsMayRead(t *testing.T) {
	p := NewProxy(NewHub(Timings{}), nil, "", nil)
	p.SetLaunchCaps(&fakeSettings{m: map[string]string{}})
	from := func(method, body string) int {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, "/_hub/capability-list", strings.NewReader(body))
		r.RemoteAddr = "192.0.2.7:5555"
		p.ServeHTTP(w, r)
		return w.Code
	}
	if code := from(http.MethodPut, `{"yaml":`+quote(hubList)+`}`); code != http.StatusForbidden {
		t.Errorf("a save from elsewhere answered %d, not 403", code)
	}
	if code := from(http.MethodGet, ""); code != http.StatusOK {
		t.Errorf("a read from elsewhere answered %d", code)
	}
	for _, path := range []string{"/_hub/capabilities", "/_hub/capabilities?room=a", "/_hub/capability-list"} {
		if !hubAskPath(path) {
			t.Errorf("a room may not ask %s", path)
		}
	}
	if hubAskPath("/_hub/capability-list/../x") {
		t.Error("a path off the list was passed")
	}
}

func TestTheRoomsCapabilityListRouteNeedsAStore(t *testing.T) {
	front := httptest.NewServer(NewProxy(NewHub(Timings{}), nil, "", nil))
	defer front.Close()
	if code, _ := do(t, http.MethodGet, front.URL+"/_hub/capability-list", "", ""); code != http.StatusNotFound {
		t.Errorf("GET without a store answered %d", code)
	}
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
