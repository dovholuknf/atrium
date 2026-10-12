//go:build integration

package link

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func (h *docsHub) putFile(name, body string, fields map[string]string, more ...func(*req)) *httptest.ResponseRecorder {
	h.t.Helper()
	return h.upload("/_hub/files", fields, body, name, more...)
}

func TestFilesAPIRouteIsAbsentWithoutAStore(t *testing.T) {
	p := NewProxy(NewHub(Timings{}), nil, "", nil)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "http://127.0.0.1/_hub/files", nil)
	r.RemoteAddr = "127.0.0.1:1"
	p.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatalf("status %d", w.Code)
	}
}

func TestFilesAPIListUploadRawDelete(t *testing.T) {
	h := newDocsHub(t)
	m := wantCode(t, h.putFile("table one.csv", "a,b\n1,2\n", map[string]string{"note": "for sg4", "ttl_days": "2"}), 201)
	if m["name"] != "table one.csv" || m["size"] != float64(8) || m["expires"] == "" {
		t.Fatalf("answer %+v", m)
	}
	wantCode(t, h.putFile("share.txt", "from a share", nil, fromShare), 201)

	l := wantCode(t, h.do(req{method: "GET", path: "/_hub/files"}), 200)
	files := l["files"].([]any)
	if len(files) != 2 {
		t.Fatalf("list %+v", l)
	}
	byName := map[string]map[string]any{}
	for _, f := range files {
		fm := f.(map[string]any)
		byName[fm["name"].(string)] = fm
	}
	if a := byName["table one.csv"]; a["origin"] != "local" || a["by"] != "operator" || a["note"] != "for sg4" {
		t.Fatalf("row %+v", a)
	}
	if a := byName["share.txt"]; a["origin"] != "share" || a["by"] != "share" {
		t.Fatalf("row %+v", a)
	}
	if u := l["usage"].(map[string]any); u["bytes"] != float64(8+12) || u["cap"] == nil {
		t.Fatalf("usage %+v", u)
	}

	w := h.do(req{method: "GET", path: "/_hub/files/" + url.PathEscape("table one.csv") + "/raw"})
	if w.Code != 200 || w.Body.String() != "a,b\n1,2\n" {
		t.Fatalf("raw %d %q", w.Code, w.Body.String())
	}
	wantCode(t, h.do(req{method: "POST", path: "/_hub/files/" + url.PathEscape("table one.csv") + "/delete"}), 200)
	wantCode(t, h.do(req{method: "GET", path: "/_hub/files/" + url.PathEscape("table one.csv") + "/raw"}), 404)
	wantCode(t, h.do(req{method: "POST", path: "/_hub/files/" + url.PathEscape("table one.csv") + "/delete"}), 404)
	if l := wantCode(t, h.do(req{method: "GET", path: "/_hub/files"}), 200); len(l["files"].([]any)) != 1 {
		t.Fatalf("list after delete %+v", l)
	}
	// A bad ttl, no file field, and a route that does not exist.
	wantCode(t, h.putFile("a.txt", "x", map[string]string{"ttl_days": "99"}), 400)
	wantCode(t, h.putFile("a.txt", "x", map[string]string{"ttl_days": "soon"}), 400)
	body, ct := form(map[string]string{"note": "n"}, "", "")
	wantCode(t, h.do(req{method: "POST", path: "/_hub/files", body: body, ctype: ct}), 400)
	wantCode(t, h.do(req{method: "GET", path: "/_hub/files/a/b/c"}), 404)
}

// A FILE IS ALWAYS A DOWNLOAD, never a preview, so a hostile HTML file cannot run in the board's origin.
func TestFilesAPIRawIsAlwaysAnAttachment(t *testing.T) {
	h := newDocsHub(t)
	wantCode(t, h.putFile("page.html", "<script>alert(1)</script>", nil), 201)
	w := h.do(req{method: "GET", path: "/_hub/files/page.html/raw"})
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("Content-Type %q", ct)
	}
	if v := w.Header().Get("X-Content-Type-Options"); v != "nosniff" {
		t.Errorf("X-Content-Type-Options %q", v)
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment;") || !strings.Contains(cd, "page.html") {
		t.Errorf("Content-Disposition %q", cd)
	}
}

func TestFilesAPIRefusesAForeignOrigin(t *testing.T) {
	h := newDocsHub(t)
	wantCode(t, h.putFile("keep.txt", "keep", nil), 201)
	routes := []struct {
		name, method, path string
		multipart          bool
		body               string
	}{
		{"upload", "POST", "/_hub/files", true, ""},
		{"delete", "POST", "/_hub/files/keep.txt/delete", false, ""},
		{"settings", "PUT", "/_hub/files/settings", false, `{"caps":{"file":1}}`},
	}
	attacks := map[string]map[string]string{
		"foreign Origin":   {"Origin": "https://evil.example"},
		"cross-site fetch": {"Sec-Fetch-Site": "cross-site"},
		"same-site fetch":  {"Sec-Fetch-Site": "same-site"},
	}
	for _, rt := range routes {
		for an, hd := range attacks {
			for _, bare := range []bool{false, true} {
				var body *strings.Reader
				ct := "application/json"
				var w *httptest.ResponseRecorder
				if rt.multipart {
					rb, c := form(map[string]string{"note": "attack"}, "attack bytes", "a.txt")
					w = h.do(req{method: rt.method, path: rt.path, body: rb, ctype: c, header: hd, bare: bare})
				} else {
					body = strings.NewReader(rt.body)
					w = h.do(req{method: rt.method, path: rt.path, body: body, ctype: ct, header: hd, bare: bare})
				}
				if w.Code != 403 {
					t.Errorf("%s with %s (bare=%v): status %d, want 403: %s", rt.name, an, bare, w.Code, w.Body.String())
				}
			}
		}
	}
	l := wantCode(t, h.do(req{method: "GET", path: "/_hub/files"}), 200)
	if len(l["files"].([]any)) != 1 {
		t.Fatalf("a refused write changed the drop: %+v", l)
	}
	set := wantCode(t, h.do(req{method: "GET", path: "/_hub/files/settings"}), 200)
	if set["caps"].(map[string]any)["file"] != float64(32<<20) {
		t.Fatalf("a refused PUT changed the settings: %+v", set)
	}
}

func TestFilesAPISettingsAreOperatorOnly(t *testing.T) {
	h := newDocsHub(t)
	put := func(body string, more ...func(*req)) *httptest.ResponseRecorder {
		r := req{method: "PUT", path: "/_hub/files/settings", body: strings.NewReader(body), ctype: "application/json"}
		for _, f := range more {
			f(&r)
		}
		return h.do(r)
	}
	set := wantCode(t, h.do(req{method: "GET", path: "/_hub/files/settings"}), 200)
	if set["operator"] != true {
		t.Fatalf("the machine's user is not the operator: %+v", set)
	}
	set = wantCode(t, h.do(req{method: "GET", path: "/_hub/files/settings", header: map[string]string{"X-Forwarded-For": "1.2.3.4"}}), 200)
	if set["operator"] != false {
		t.Fatalf("a share is the operator: %+v", set)
	}

	wantCode(t, put(`{"caps":{"file":1000}}`, fromShare), 403)
	wantCode(t, put(`{"caps":{"file":1000}}`, func(r *req) { r.host = "abc.shares.zrok.io" }), 403)
	set = wantCode(t, h.do(req{method: "GET", path: "/_hub/files/settings"}), 200)
	if set["caps"].(map[string]any)["file"] != float64(32<<20) {
		t.Fatalf("a refused PUT changed the cap: %+v", set)
	}

	got := wantCode(t, put(`{"caps":{"file":1000,"ttl_days":3}}`), 200)
	caps := got["caps"].(map[string]any)
	if caps["file"] != float64(1000) || caps["ttl_days"] != float64(3) || caps["total"] != float64(512<<20) {
		t.Fatalf("caps %+v", caps)
	}
	wantCode(t, put(`{"caps":{"file":-1}}`), 400)
	wantCode(t, put(`not json`), 400)
	// The new cap is the one in force.
	w := h.putFile("big.bin", strings.Repeat("x", 2000), nil)
	wantCode(t, w, 413)
}

func TestFilesAPIUploadRefusesASecret(t *testing.T) {
	h := newDocsHub(t)
	m := wantCode(t, h.putFile(".env", "A=1", nil), 422)
	if m["rule"] != "secret-file-name" {
		t.Fatalf("%+v", m)
	}
	m = wantCode(t, h.putFile("notes.txt", "-----BEGIN PRIVATE KEY-----\nMIIE\n-----END PRIVATE KEY-----\n", nil), 422)
	if m["rule"] != "pem-private-key" {
		t.Fatalf("%+v", m)
	}
	wantCode(t, h.putFile("empty.txt", "", nil), 400)
	if l := wantCode(t, h.do(req{method: "GET", path: "/_hub/files"}), 200); len(l["files"].([]any)) != 0 {
		t.Fatalf("a refused upload stored %+v", l)
	}
}
