package link

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dovholuknf/atrium/internal/edge"
	"github.com/dovholuknf/atrium/internal/hubstore"
)

// docsHub is a hub with a document store, and the handler a browser would reach: the proxy
// wrapped in the edge, as the board's listener is.
type docsHub struct {
	t     *testing.T
	st    *hubstore.Store
	p     *Proxy
	edged http.Handler
}

func newDocsHub(t *testing.T) *docsHub {
	t.Helper()
	st, err := hubstore.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	p := NewProxy(NewHub(Timings{}), nil, "", nil)
	p.SetDocs(st)
	return &docsHub{t: t, st: st, p: p, edged: edge.Named(p)}
}

// req is one request. Defaults to a plain loopback one, which is what the operator's browser
// or shell looks like.
type req struct {
	method, path string
	body         io.Reader
	ctype        string
	header       map[string]string
	host         string
	remote       string
	bare         bool // skip the edge, to prove the route checks for itself
}

func (h *docsHub) do(r req) *httptest.ResponseRecorder {
	h.t.Helper()
	host := r.host
	if host == "" {
		host = "127.0.0.1:7778"
	}
	hr := httptest.NewRequest(r.method, "http://"+host+r.path, r.body)
	hr.RemoteAddr = "127.0.0.1:50000"
	if r.remote != "" {
		hr.RemoteAddr = r.remote
	}
	if r.ctype != "" {
		hr.Header.Set("Content-Type", r.ctype)
	}
	for k, v := range r.header {
		hr.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	if r.bare {
		h.p.ServeHTTP(w, hr)
	} else {
		h.edged.ServeHTTP(w, hr)
	}
	return w
}

func form(fields map[string]string, file, name string) (io.Reader, string) {
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	for k, v := range fields {
		mw.WriteField(k, v)
	}
	if file != "" || name != "" {
		hd := textproto.MIMEHeader{}
		hd.Set("Content-Disposition", `form-data; name="file"; filename="`+name+`"`)
		hd.Set("Content-Type", "application/octet-stream")
		pw, _ := mw.CreatePart(hd)
		pw.Write([]byte(file))
	}
	mw.Close()
	return &b, mw.FormDataContentType()
}

func (h *docsHub) upload(path string, fields map[string]string, file, name string, more ...func(*req)) *httptest.ResponseRecorder {
	h.t.Helper()
	body, ct := form(fields, file, name)
	r := req{method: "POST", path: path, body: body, ctype: ct}
	for _, f := range more {
		f(&r)
	}
	return h.do(r)
}

func asJSON(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("not JSON (%d): %q", w.Code, w.Body.String())
	}
	return m
}

func wantCode(t *testing.T, w *httptest.ResponseRecorder, code int) map[string]any {
	t.Helper()
	if w.Code != code {
		t.Fatalf("status %d, want %d: %s", w.Code, code, w.Body.String())
	}
	if strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		return asJSON(t, w)
	}
	return nil
}

func (h *docsHub) newDoc(title, name, body string) string {
	h.t.Helper()
	w := h.upload("/_hub/docs", map[string]string{"title": title}, body, name)
	m := wantCode(h.t, w, 201)
	return m["slug"].(string)
}

func fromShare(r *req) { r.header = map[string]string{"X-Forwarded-For": "203.0.113.9"} }

func (h *docsHub) latest(slug string) map[string]any {
	h.t.Helper()
	w := h.do(req{method: "GET", path: "/_hub/docs/" + slug})
	m := wantCode(h.t, w, 200)
	vs := m["versions"].([]any)
	return vs[len(vs)-1].(map[string]any)
}

// A hub with no document store answers 404, like every optional route.
func TestDocsRouteIsAbsentWithoutAStore(t *testing.T) {
	p := NewProxy(NewHub(Timings{}), nil, "", nil)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "http://127.0.0.1/_hub/docs", nil)
	r.RemoteAddr = "127.0.0.1:1"
	p.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatalf("status %d", w.Code)
	}
}

// THE ORIGIN IS DECIDED BY THE HUB FROM THE REQUEST: local for a plain loopback upload, share for
// any proxy header or a non-loopback Host or a non-loopback peer, and a form cannot say otherwise.
func TestUploadOriginIsRecordedByTheHub(t *testing.T) {
	h := newDocsHub(t)
	for name, c := range map[string]struct {
		more []func(*req)
		want string
	}{
		"plain loopback":       {nil, "local"},
		"x-forwarded-for":      {[]func(*req){fromShare}, "share"},
		"forwarded":            {[]func(*req){func(r *req) { r.header = map[string]string{"Forwarded": "for=1.2.3.4"} }}, "share"},
		"x-real-ip":            {[]func(*req){func(r *req) { r.header = map[string]string{"X-Real-Ip": "1.2.3.4"} }}, "share"},
		"public host":          {[]func(*req){func(r *req) { r.host = "abc123.shares.zrok.io"; r.bare = true }}, "share"},
		"peer is not loopback": {[]func(*req){func(r *req) { r.remote = "192.0.2.7:4000" }}, "share"},
	} {
		w := h.upload("/_hub/docs", map[string]string{"title": "origin " + name}, "x "+name, "o.md", c.more...)
		m := wantCode(t, w, 201)
		v := h.latest(m["slug"].(string))
		wantBy := map[string]string{"local": "operator", "share": "share"}[c.want]
		if v["origin"] != c.want || v["by"] != wantBy {
			t.Errorf("%s recorded origin %v by %v, want %s by %s", name, v["origin"], v["by"], c.want, wantBy)
		}
	}
	// A page cannot claim local, or card, or a name, by saying so in the form.
	body, ct := form(map[string]string{"title": "claims", "origin": "local", "by": "clint", "card": "sg4~1"}, "claims", "c.md")
	w := h.do(req{method: "POST", path: "/_hub/docs", body: body, ctype: ct, header: map[string]string{"X-Forwarded-For": "1.2.3.4"}})
	m := wantCode(t, w, 201)
	v := h.latest(m["slug"].(string))
	if v["origin"] != "share" || v["by"] != "share" || v["card"] != nil {
		t.Fatalf("the form was believed: %+v", v)
	}
}

// RAW BYTES ARE ALWAYS octet-stream, nosniff AND AN ATTACHMENT, whatever the document says it is.
func TestRawIsAlwaysAnAttachment(t *testing.T) {
	h := newDocsHub(t)
	for _, c := range []struct{ name, body string }{
		{"page.html", "<script>alert(1)</script>"}, {"pic.svg", "<svg onload=alert(1)/>"},
		{"notes.md", "# hi"}, {"p.png", "\x89PNG\r\n\x1a\nxx"}, {"x.bin", "\x00\x01"},
	} {
		slug := h.newDoc("Doc "+c.name, c.name, c.body)
		w := h.do(req{method: "GET", path: "/_hub/docs/" + slug + "/raw"})
		wantCode(t, w, 200)
		hd := w.Header()
		if hd.Get("Content-Type") != "application/octet-stream" {
			t.Errorf("%s: content type %q", c.name, hd.Get("Content-Type"))
		}
		if hd.Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: no nosniff", c.name)
		}
		if cd := hd.Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment;") {
			t.Errorf("%s: disposition %q", c.name, cd)
		}
		if w.Body.String() != c.body {
			t.Errorf("%s: body %q", c.name, w.Body.String())
		}
	}
}

// CR/LF IN A TITLE GIVES ONE Content-Disposition HEADER, ASCII filename= WITH CR LF " AND \ GONE,
// AND filename*=UTF-8” BESIDE IT.
func TestTitleWithCRLFGivesOneDispositionHeader(t *testing.T) {
	got := attachmentHeader("evil\r\nX-Injected: yes\r\n\"q\\uote\" é.md")
	if strings.ContainsAny(got, "\r\n") {
		t.Fatalf("CR or LF in %q", got)
	}
	if !strings.Contains(got, `filename="`) || !strings.Contains(got, "filename*=UTF-8''") {
		t.Fatalf("not both forms: %q", got)
	}
	ascii := got[strings.Index(got, `filename="`)+len(`filename="`):]
	ascii = ascii[:strings.Index(ascii, `"`)]
	if strings.ContainsAny(ascii, "\\\"\r\n") || strings.ContainsAny(ascii, "é") {
		t.Fatalf("ascii form %q", ascii)
	}
	if !strings.Contains(got, "%C3%A9") {
		t.Fatalf("no percent-encoded UTF-8 in %q", got)
	}
	if strings.Contains(got, "%0D") || strings.Contains(got, "%0A") {
		t.Fatalf("CR or LF survived in the encoded form: %q", got)
	}

	// Through the route: a title a model wrote, stored, served.
	h := newDocsHub(t)
	w := h.upload("/_hub/docs", map[string]string{"title": "Evil\r\nX-Injected: yes"}, "x", "x.md")
	slug := wantCode(t, w, 201)["slug"].(string)
	raw := h.do(req{method: "GET", path: "/_hub/docs/" + slug + "/raw"})
	if n := len(raw.Header().Values("Content-Disposition")); n != 1 {
		t.Fatalf("%d Content-Disposition headers", n)
	}
	if raw.Header().Get("X-Injected") != "" {
		t.Fatal("the title injected a header")
	}
	// And a title that reached the store raw, as an older hub might have left it, is still one header.
	if _, err := h.st.DocGet(slug); err != nil {
		t.Fatal(err)
	}
}

// A FOREIGN Origin OR Sec-Fetch-Site: cross-site IS REFUSED ON EVERY WRITE ROUTE, at the edge and
// again in the route, and nothing changes.
func TestForeignOriginIsRefusedOnEveryWriteRoute(t *testing.T) {
	h := newDocsHub(t)
	slug := h.newDoc("Guarded", "g.md", "original")
	h.do(req{method: "POST", path: "/_hub/docs/" + slug + "/delete"})
	h.do(req{method: "POST", path: "/_hub/docs/" + slug + "/restore"})

	routes := []struct {
		name, method, path string
		multipart          bool
		body               string
	}{
		{"upload", "POST", "/_hub/docs", true, ""},
		{"version", "POST", "/_hub/docs/" + slug + "/versions", true, ""},
		{"title", "POST", "/_hub/docs/" + slug + "/title", false, `{"title":"hijacked"}`},
		{"delete", "POST", "/_hub/docs/" + slug + "/delete", false, ""},
		{"restore", "POST", "/_hub/docs/" + slug + "/restore", false, ""},
		{"purge", "POST", "/_hub/docs/" + slug + "/purge", false, ""},
		{"settings", "PUT", "/_hub/docs/settings", false, `{"enabled":false}`},
	}
	attacks := map[string]map[string]string{
		"foreign Origin":           {"Origin": "https://evil.example"},
		"cross-site fetch":         {"Sec-Fetch-Site": "cross-site"},
		"same-site fetch":          {"Sec-Fetch-Site": "same-site"},
		"foreign Origin on a port": {"Origin": "http://127.0.0.1:9999"},
	}
	for _, rt := range routes {
		for an, hd := range attacks {
			for _, bare := range []bool{false, true} {
				var body io.Reader
				ct := "application/json"
				if rt.multipart {
					body, ct = form(map[string]string{"title": "attack"}, "attack bytes", "a.md")
				} else {
					body = strings.NewReader(rt.body)
				}
				w := h.do(req{method: rt.method, path: rt.path, body: body, ctype: ct, header: hd, bare: bare})
				if w.Code != 403 {
					t.Errorf("%s with %s (bare=%v): status %d, want 403: %s", rt.name, an, bare, w.Code, w.Body.String())
				}
			}
		}
	}
	d := asJSON(t, h.do(req{method: "GET", path: "/_hub/docs/" + slug}))
	if d["title"] != "Guarded" || d["deleted"] != nil || len(d["versions"].([]any)) != 1 {
		t.Fatalf("a refused write changed something: %+v", d)
	}
	set := asJSON(t, h.do(req{method: "GET", path: "/_hub/docs/settings"}))
	if set["enabled"] != true {
		t.Fatal("a refused PUT changed the settings")
	}
	if l := asJSON(t, h.do(req{method: "GET", path: "/_hub/docs"})); len(l["docs"].([]any)) != 1 {
		t.Fatalf("a refused upload made a document: %+v", l)
	}

	// The page's own fetch sends same-origin and is let through, and so is a shell with no headers.
	body, ct := form(nil, "ok", "ok.md")
	w := h.do(req{method: "POST", path: "/_hub/docs/" + slug + "/versions", body: body, ctype: ct,
		header: map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "http://127.0.0.1:7778"}})
	wantCode(t, w, 201)
	// A foreign Origin does not stop a READ.
	w = h.do(req{method: "GET", path: "/_hub/docs/" + slug, header: map[string]string{"Origin": "https://evil.example"}})
	wantCode(t, w, 200)
}

// A PURGE WITH X-Forwarded-For SET IS 403, and so is one from a public Host or a peer that is not
// loopback. The bytes are still there.
func TestPurgeThroughAProxyIsRefused(t *testing.T) {
	h := newDocsHub(t)
	slug := h.newDoc("Keep", "k.md", "keep me")
	cases := map[string]func(*req){
		"x-forwarded-for": fromShare,
		"public host":     func(r *req) { r.host = "abc.shares.zrok.io"; r.bare = true },
		"remote peer":     func(r *req) { r.remote = "192.0.2.7:1" },
		"x-forwarded-host": func(r *req) {
			r.header = map[string]string{"X-Forwarded-Host": "abc.shares.zrok.io"}
		},
	}
	for name, f := range cases {
		r := req{method: "POST", path: "/_hub/docs/" + slug + "/purge"}
		f(&r)
		w := h.do(r)
		m := wantCode(t, w, 403)
		if name == "x-forwarded-for" && !strings.Contains(m["error"].(string), "proxy") {
			t.Errorf("no word about the proxy: %v", m["error"])
		}
		if v := h.latest(slug); v["purged"] != false {
			t.Fatalf("%s purged it", name)
		}
	}
	raw := h.do(req{method: "GET", path: "/_hub/docs/" + slug + "/raw"})
	if raw.Body.String() != "keep me" {
		t.Fatalf("bytes %q", raw.Body.String())
	}
	// The operator purges, and the answer after is the contract's.
	wantCode(t, h.do(req{method: "POST", path: "/_hub/docs/" + slug + "/purge?v=1"}), 200)
	w := h.do(req{method: "GET", path: "/_hub/docs/" + slug + "/raw?v=1"})
	m := wantCode(t, w, 410)
	if m["error"] != "the bytes are missing" {
		t.Fatalf("error %v", m["error"])
	}
	if v := h.latest(slug); v["purged"] != true || v["missing"] != false {
		t.Fatalf("version after purge %+v", v)
	}
}

// THE CAPS, THE SWITCH AND AN OVERRIDE ARE THE OPERATOR'S.
func TestSettingsAreOperatorOnly(t *testing.T) {
	h := newDocsHub(t)
	get := func(more ...func(*req)) map[string]any {
		r := req{method: "GET", path: "/_hub/docs/settings"}
		for _, f := range more {
			f(&r)
		}
		return wantCode(t, h.do(r), 200)
	}
	if got := get(); got["operator"] != true || got["enabled"] != true {
		t.Fatalf("operator view %+v", got)
	}
	got := get(fromShare)
	if got["operator"] != false {
		t.Fatalf("a share is told it is the operator: %+v", got)
	}
	caps := got["caps"].(map[string]any)
	if caps["text"] != float64(5<<20) || caps["other"] != float64(20<<20) || caps["total"] != float64(2<<30) || caps["per_card_hour"] != float64(30) {
		t.Fatalf("default caps %+v", caps)
	}
	for _, k := range []string{"usage", "largest"} {
		if got[k] == nil {
			t.Errorf("no %s in settings", k)
		}
	}

	put := func(body string, more ...func(*req)) *httptest.ResponseRecorder {
		r := req{method: "PUT", path: "/_hub/docs/settings", body: strings.NewReader(body), ctype: "application/json"}
		for _, f := range more {
			f(&r)
		}
		return h.do(r)
	}
	m := wantCode(t, put(`{"enabled":false}`, fromShare), 403)
	if !strings.Contains(m["error"].(string), "machine") {
		t.Fatalf("the refusal does not say to run it on the machine: %v", m["error"])
	}
	if get()["enabled"] != true {
		t.Fatal("a refused PUT changed it")
	}
	// A subset, from the machine, answers the same shape as GET.
	out := wantCode(t, put(`{"caps":{"per_card_hour":3}}`), 200)
	if out["caps"].(map[string]any)["per_card_hour"] != float64(3) || out["caps"].(map[string]any)["text"] != float64(5<<20) {
		t.Fatalf("subset put %+v", out)
	}
	wantCode(t, put(`{"caps":{"text":0}}`), 400)
	wantCode(t, put(`{`), 400)

	// Switching publishing off is a 503 with the reason, for a board upload too.
	wantCode(t, put(`{"enabled":false}`), 200)
	m = wantCode(t, h.upload("/_hub/docs", nil, "x", "x.md"), 503)
	if !strings.Contains(m["error"].(string), "turned off") {
		t.Fatalf("reason %v", m["error"])
	}
	wantCode(t, put(`{"enabled":true}`), 200)
}

// THE SECRET RULES APPLY TO A BOARD UPLOAD, 422 WITH THE RULE NAMED, AND ONLY THE OPERATOR OVERRIDES.
func TestBoardUploadsAreCheckedForSecrets(t *testing.T) {
	h := newDocsHub(t)
	pem := "-----BEGIN RSA PRIVATE KEY-----\nMIIB\n-----END RSA PRIVATE KEY-----\n"
	m := wantCode(t, h.upload("/_hub/docs", nil, pem, "notes.md"), 422)
	if m["rule"] != "pem-private-key" {
		t.Fatalf("rule %v", m["rule"])
	}
	m = wantCode(t, h.upload("/_hub/docs", nil, "k=AKIAIOSFODNN7EXAMPLE", "notes.md", fromShare), 422)
	if m["rule"] != "aws-access-key" {
		t.Fatalf("rule %v", m["rule"])
	}
	if l := asJSON(t, h.do(req{method: "GET", path: "/_hub/docs"})); len(l["docs"].([]any)) != 0 {
		t.Fatal("a refused secret was stored")
	}
	// A new version is checked too.
	slug := h.newDoc("Clean", "c.md", "clean")
	wantCode(t, h.upload("/_hub/docs/"+slug+"/versions", nil, "ghp_"+strings.Repeat("a", 36), "c.md"), 422)

	// override from a share is 403, not ignored.
	wantCode(t, h.upload("/_hub/docs", map[string]string{"override": "1"}, pem, "k.md", fromShare), 403)
	// from the machine it goes through and is recorded.
	m = wantCode(t, h.upload("/_hub/docs", map[string]string{"override": "1", "title": "Allowed"}, pem, "k.md"), 201)
	v := h.latest(m["slug"].(string))
	if v["origin"] != "local" {
		t.Fatalf("version %+v", v)
	}
	if st, _ := h.st.DocGet(m["slug"].(string)); !st.Versions[0].Override {
		t.Fatal("the override was not recorded on the version")
	}
	wantCode(t, h.upload("/_hub/docs", map[string]string{"override": "yes"}, "x", "x.md"), 400)
}

// 413 FOR A SIXTH MIB OF TEXT AND FOR A BODY PAST THE LARGER CAP, 400 FOR THE UNREADABLE.
func TestSizeCapsAndBadForms(t *testing.T) {
	h := newDocsHub(t)
	five := strings.Repeat("a", 5<<20)
	wantCode(t, h.upload("/_hub/docs", map[string]string{"title": "five"}, five, "five.txt"), 201)
	m := wantCode(t, h.upload("/_hub/docs", map[string]string{"title": "six"}, five+"a", "six.txt"), 413)
	if m["error"] == "" {
		t.Fatal("no sentence")
	}
	// A body past the larger cap plus the form is cut off.
	big := strings.Repeat("b", 20<<20+2<<20)
	wantCode(t, h.upload("/_hub/docs", nil, big, "big.bin"), 413)
	// A not-text file of 21 MiB.
	wantCode(t, h.upload("/_hub/docs", nil, "\x00"+strings.Repeat("c", 20<<20), "c.bin"), 413)
	// Unreadable, none, empty.
	wantCode(t, h.do(req{method: "POST", path: "/_hub/docs", body: strings.NewReader("nope"), ctype: "application/json"}), 400)
	wantCode(t, h.do(req{method: "POST", path: "/_hub/docs", body: strings.NewReader("--x\r\ngarbage"), ctype: "multipart/form-data; boundary=x"}), 400)
	wantCode(t, h.upload("/_hub/docs", map[string]string{"title": "no file"}, "", ""), 400)
	wantCode(t, h.upload("/_hub/docs", map[string]string{"title": "empty"}, "", "e.md"), 400)
	wantCode(t, h.upload("/_hub/docs", map[string]string{"title": "\r\n\t "}, "x", "x.md"), 400)
	// A slug field on the new-document route is not supported.
	m = wantCode(t, h.upload("/_hub/docs", map[string]string{"slug": "five"}, "x", "x.md"), 400)
	if !strings.Contains(m["error"].(string), "versions") {
		t.Fatalf("does not point at the versions route: %v", m["error"])
	}
}

func TestStoreTotalIs507(t *testing.T) {
	h := newDocsHub(t)
	put := func(body string) {
		wantCode(t, h.do(req{method: "PUT", path: "/_hub/docs/settings", body: strings.NewReader(body), ctype: "application/json"}), 200)
	}
	put(`{"caps":{"total":50}}`)
	wantCode(t, h.upload("/_hub/docs", nil, strings.Repeat("a", 40), "a.txt"), 201)
	m := wantCode(t, h.upload("/_hub/docs", nil, strings.Repeat("b", 40), "b.txt"), 507)
	if !strings.Contains(m["error"].(string), "full") {
		t.Fatalf("reason %v", m["error"])
	}
}

// `Ab Ab` TWICE GIVES ab-ab THEN ab-ab-2, THROUGH THE API.
func TestSlugCollisionThroughTheAPI(t *testing.T) {
	h := newDocsHub(t)
	a := h.upload("/_hub/docs", map[string]string{"title": "Ab Ab"}, "one", "a.md")
	b := h.upload("/_hub/docs", map[string]string{"title": "Ab Ab"}, "two", "a.md")
	ma, mb := wantCode(t, a, 201), wantCode(t, b, 201)
	if ma["slug"] != "ab-ab" || mb["slug"] != "ab-ab-2" {
		t.Fatalf("%v %v", ma["slug"], mb["slug"])
	}
	if ma["url"] != "/d/ab-ab" || ma["version_url"] != "/d/ab-ab@1" || ma["version"] != float64(1) {
		t.Fatalf("the contract's answer: %+v", ma)
	}
}

// OLD VERSIONS KEEP THEIR BYTES AFTER A NEW ONE, and a new version keeps the title.
func TestOldVersionsKeepTheirBytesThroughTheAPI(t *testing.T) {
	h := newDocsHub(t)
	slug := h.newDoc("Report", "r.md", "draft one")
	m := wantCode(t, h.upload("/_hub/docs/"+slug+"/versions", map[string]string{"title": "ignored"}, "draft two", "r.md"), 201)
	if m["version"] != float64(2) || m["version_url"] != "/d/"+slug+"@2" {
		t.Fatalf("%+v", m)
	}
	for v, want := range map[string]string{"1": "draft one", "2": "draft two", "": "draft two"} {
		w := h.do(req{method: "GET", path: "/_hub/docs/" + slug + "/raw?v=" + v})
		if wantCode(t, w, 200); w.Body.String() != want {
			t.Errorf("v=%q gave %q", v, w.Body.String())
		}
	}
	d := asJSON(t, h.do(req{method: "GET", path: "/_hub/docs/" + slug}))
	if d["title"] != "Report" || len(d["versions"].([]any)) != 2 {
		t.Fatalf("%+v", d)
	}
	wantCode(t, h.do(req{method: "GET", path: "/_hub/docs/" + slug + "/raw?v=9"}), 404)
	wantCode(t, h.do(req{method: "GET", path: "/_hub/docs/" + slug + "/raw?v=x"}), 400)
	wantCode(t, h.upload("/_hub/docs/nope/versions", nil, "x", "x.md"), 404)
}

// The version and summary shapes @ui parses.
func TestDocShapesMatchTheContract(t *testing.T) {
	h := newDocsHub(t)
	slug := h.newDoc("Usage 2026-09-29", "usage.md", "# usage")
	v := h.latest(slug)
	for _, k := range []string{"n", "at", "by", "origin", "size", "kind", "mime", "name", "sha", "missing", "purged"} {
		if _, ok := v[k]; !ok {
			t.Errorf("version has no %q: %+v", k, v)
		}
	}
	if _, ok := v["card"]; ok {
		t.Error("card is present on a non-card origin")
	}
	if v["kind"] != "markdown" || v["mime"] != "text/markdown" || v["name"] != "usage.md" || v["by"] != "operator" ||
		v["size"] != float64(7) || len(v["sha"].(string)) != 64 {
		t.Fatalf("version %+v", v)
	}
	l := asJSON(t, h.do(req{method: "GET", path: "/_hub/docs"}))
	rows := l["docs"].([]any)
	row := rows[0].(map[string]any)
	for _, k := range []string{"slug", "title", "created", "updated", "versions", "latest", "deleted"} {
		if _, ok := row[k]; !ok {
			t.Errorf("summary has no %q: %+v", k, row)
		}
	}
	if row["deleted"] != nil || row["slug"] != "usage-2026-09-29" {
		t.Fatalf("summary %+v", row)
	}
	u := l["usage"].(map[string]any)
	if u["bytes"] != float64(7) || u["cap"] != float64(2<<30) {
		t.Fatalf("usage %+v", u)
	}
}

// LIST, FILTER, TOMBSTONE AND RESTORE OVER HTTP.
func TestListDeleteRestore(t *testing.T) {
	h := newDocsHub(t)
	a := h.newDoc("Usage report", "a.md", "1")
	b := h.newDoc("Eval notes", "b.md", "2")
	ids := func(q string) []string {
		var out []string
		for _, d := range asJSON(t, h.do(req{method: "GET", path: "/_hub/docs" + q}))["docs"].([]any) {
			out = append(out, d.(map[string]any)["slug"].(string))
		}
		return out
	}
	if got := ids(""); len(got) != 2 || got[0] != b {
		t.Fatalf("order %v", got)
	}
	if got := ids("?q=USAGE"); len(got) != 1 || got[0] != a {
		t.Fatalf("filter %v", got)
	}
	// delete from a share is allowed, and it is a tombstone.
	wantCode(t, h.do(req{method: "POST", path: "/_hub/docs/" + a + "/delete", header: map[string]string{"X-Forwarded-For": "1.2.3.4"}}), 200)
	if got := ids(""); len(got) != 1 {
		t.Fatalf("a tombstone is listed: %v", got)
	}
	if got := ids("?deleted=1"); len(got) != 1 || got[0] != a {
		t.Fatalf("restore list %v", got)
	}
	d := asJSON(t, h.do(req{method: "GET", path: "/_hub/docs/" + a}))
	del := d["deleted"].(map[string]any)
	if del["by"] != "share" || del["at"] == "" {
		t.Fatalf("tombstone %+v", del)
	}
	wantCode(t, h.do(req{method: "GET", path: "/_hub/docs/" + a + "/raw"}), 410)
	wantCode(t, h.do(req{method: "POST", path: "/_hub/docs/" + a + "/restore", header: map[string]string{"X-Forwarded-For": "1.2.3.4"}}), 200)
	if got := ids(""); len(got) != 2 {
		t.Fatalf("not restored: %v", got)
	}
	// Rename keeps the slug.
	wantCode(t, h.do(req{method: "POST", path: "/_hub/docs/" + a + "/title", body: strings.NewReader(`{"title":"Renamed"}`), ctype: "application/json"}), 200)
	if d := asJSON(t, h.do(req{method: "GET", path: "/_hub/docs/" + a})); d["title"] != "Renamed" || d["slug"] != a {
		t.Fatalf("%+v", d)
	}
	wantCode(t, h.do(req{method: "POST", path: "/_hub/docs/" + a + "/title", body: strings.NewReader(`{"title":"  "}`), ctype: "application/json"}), 400)
	wantCode(t, h.do(req{method: "POST", path: "/_hub/docs/nope/delete"}), 404)
	wantCode(t, h.do(req{method: "GET", path: "/_hub/docs/nope"}), 404)
	wantCode(t, h.do(req{method: "GET", path: "/_hub/docs/Bad_Slug"}), 404)
	wantCode(t, h.do(req{method: "GET", path: "/_hub/docs/" + a + "/nonsense"}), 404)
	wantCode(t, h.do(req{method: "GET", path: "/_hub/docs/" + a + "/delete"}), 405)
}

// A DOCUMENT WITH A PURGED VERSION IS RESTORED BY THE OPERATOR ONLY.
func TestRestoreAfterPurgeIsOperatorOnly(t *testing.T) {
	h := newDocsHub(t)
	slug := h.newDoc("Purgeable", "p.md", "bytes")
	wantCode(t, h.do(req{method: "POST", path: "/_hub/docs/" + slug + "/purge"}), 200)
	wantCode(t, h.do(req{method: "POST", path: "/_hub/docs/" + slug + "/delete"}), 200)
	wantCode(t, h.do(req{method: "POST", path: "/_hub/docs/" + slug + "/restore", header: map[string]string{"X-Forwarded-For": "1.2.3.4"}}), 403)
	if d := asJSON(t, h.do(req{method: "GET", path: "/_hub/docs/" + slug})); d["deleted"] == nil {
		t.Fatal("a refused restore restored it")
	}
	wantCode(t, h.do(req{method: "POST", path: "/_hub/docs/" + slug + "/restore"}), 200)
}

// A MISSING BLOB IS SAID, NOT FATAL, OVER HTTP.
func TestMissingBlobOverHTTP(t *testing.T) {
	h := newDocsHub(t)
	slug := h.newDoc("Lost", "l.md", "lost bytes")
	f, v, err := h.st.DocOpen(slug, 1)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := os.Remove(filepath.Join(h.st.DocsDir(), v.SHA)); err != nil {
		t.Fatal(err)
	}
	if got := h.latest(slug); got["missing"] != true {
		t.Fatalf("%+v", got)
	}
	m := wantCode(t, h.do(req{method: "GET", path: "/_hub/docs/" + slug + "/raw"}), 410)
	if m["error"] != "the bytes are missing" {
		t.Fatalf("%v", m["error"])
	}
	wantCode(t, h.do(req{method: "GET", path: "/_hub/docs"}), 200)
}

// /d/<slug> AND /d/<slug>@<n> ANSWER WITH THE /m SHELL, and the hub does not look the slug up, so a
// deleted or unknown one gets the shell too. Wrong shapes are 404.
func TestDocURLsAnswerWithThePhoneShell(t *testing.T) {
	board := fstest.MapFS{
		"index.html":   &fstest.MapFile{Data: []byte("<!doctype html>the board")},
		"m/index.html": &fstest.MapFile{Data: []byte("<!doctype html>the phone page")},
	}
	h := NewProxy(NewHub(Timings{}), board, "", nil)
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1"+path, nil))
		return w
	}
	for _, ok := range []string{"/d/usage-2026-09-29", "/d/usage-2026-09-29@2", "/d/a", "/d/a-b-2@10", "/d/gone-doc"} {
		if w := get(ok); w.Code != 200 || !strings.Contains(w.Body.String(), "the phone page") {
			t.Errorf("%s answered %d %q", ok, w.Code, w.Body.String())
		}
	}
	for _, bad := range []string{"/d/", "/d/Bad_Slug", "/d/x@0", "/d/x@", "/d/x@abc", "/d/x@01", "/d/a@1@2", "/d/x/y", "/d/@1", "/d/x@-1", "/d/" + strings.Repeat("a", 61)} {
		if w := get(bad); w.Code != 404 {
			t.Errorf("%s answered %d, want 404", bad, w.Code)
		}
	}
	if slug, n, ok := ParseDocPath("/d/my-doc@12"); !ok || slug != "my-doc" || n != 12 {
		t.Fatalf("parsed %q %d %v", slug, n, ok)
	}
	// The phone's document list is the shell at exactly /m/docs, and nothing under it.
	if w := get("/m/docs"); w.Code != 200 || !strings.Contains(w.Body.String(), "the phone page") {
		t.Errorf("/m/docs answered %d %q", w.Code, w.Body.String())
	}
	if w := get("/m/docs/x"); strings.Contains(w.Body.String(), "the phone page") {
		t.Errorf("/m/docs/x got the shell")
	}
	// A write to /d/ is not the shell.
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "http://127.0.0.1/d/x", nil))
	if strings.Contains(w.Body.String(), "the phone page") {
		t.Fatal("a POST got the shell")
	}
}

// A DELETE FROM A SHARE SAYS SHARE, one from the machine says operator, and the audit log has a
// doc-deleted line either way.
func TestDeleteRecordsWhoAndIsAudited(t *testing.T) {
	h := newDocsHub(t)
	fa := &fakeAudit{}
	h.p.SetAuditLog(fa)
	a := h.newDoc("From the machine", "a.md", "1")
	b := h.newDoc("From the share", "b.md", "2")
	wantCode(t, h.do(req{method: "POST", path: "/_hub/docs/" + a + "/delete"}), 200)
	wantCode(t, h.do(req{method: "POST", path: "/_hub/docs/" + b + "/delete", header: map[string]string{"X-Forwarded-For": "1.2.3.4"}}), 200)
	for slug, by := range map[string]string{a: "operator", b: "share"} {
		d := asJSON(t, h.do(req{method: "GET", path: "/_hub/docs/" + slug}))
		if got := d["deleted"].(map[string]any)["by"]; got != by {
			t.Errorf("%s deleted by %v, want %s", slug, got, by)
		}
	}
	var lines []string
	for _, r := range fa.records {
		if r.kind == "doc-deleted" {
			lines = append(lines, r.detail)
		}
	}
	if len(lines) != 2 || lines[0] != a+" by operator" || lines[1] != b+" by share" {
		t.Fatalf("audit lines %v", lines)
	}
}

// A NEW VERSION TO A TOMBSTONED SLUG IS 410, and a restore lets it through.
func TestAVersionToATombstonedDocumentIs410(t *testing.T) {
	h := newDocsHub(t)
	slug := h.newDoc("Tomb", "t.md", "one")
	wantCode(t, h.do(req{method: "POST", path: "/_hub/docs/" + slug + "/delete"}), 200)
	m := wantCode(t, h.upload("/_hub/docs/"+slug+"/versions", nil, "two", "t.md"), 410)
	if !strings.Contains(m["error"].(string), "restore") {
		t.Fatalf("does not say what to do: %v", m["error"])
	}
	if d := asJSON(t, h.do(req{method: "GET", path: "/_hub/docs/" + slug})); len(d["versions"].([]any)) != 1 {
		t.Fatal("a version was added to a tombstone")
	}
	wantCode(t, h.do(req{method: "POST", path: "/_hub/docs/" + slug + "/restore"}), 200)
	wantCode(t, h.upload("/_hub/docs/"+slug+"/versions", nil, "two", "t.md"), 201)
}

// A PURGE'S AUDIT LINE NAMES THE OTHER DOCUMENTS THE SAME BYTES REACHED.
func TestPurgeAuditNamesWhatTheSharedBytesReached(t *testing.T) {
	h := newDocsHub(t)
	fa := &fakeAudit{}
	h.p.SetAuditLog(fa)
	a := h.newDoc("Alpha", "a.md", "identical")
	b := h.newDoc("Bravo", "b.md", "identical")
	m := wantCode(t, h.do(req{method: "POST", path: "/_hub/docs/" + a + "/purge"}), 200)
	also, _ := m["also"].([]any)
	if len(also) != 1 || also[0] != b+"@1" || m["purged"] != float64(1) {
		t.Fatalf("answer %+v", m)
	}
	var got string
	for _, r := range fa.records {
		if r.kind == "doc-purged" {
			got = r.detail
		}
	}
	if !strings.Contains(got, a) || !strings.Contains(got, b+"@1") {
		t.Fatalf("audit line %q", got)
	}
	// A purge that reached nothing else says so with an empty list, not null.
	c := h.newDoc("Charlie", "c.md", "alone")
	m = wantCode(t, h.do(req{method: "POST", path: "/_hub/docs/" + c + "/purge"}), 200)
	if l, ok := m["also"].([]any); !ok || len(l) != 0 {
		t.Fatalf("also %+v", m["also"])
	}
}
