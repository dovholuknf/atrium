package link

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

type pushRoutes struct {
	*pushRig
	proxy *Proxy
}

func newPushRoutes(t *testing.T) *pushRoutes {
	t.Helper()
	rig := newPushRig(t)
	p := NewProxy(NewHub(Timings{}), nil, "", nil)
	p.SetNotify(rig.n)
	p.SetPush(rig.p)
	return &pushRoutes{pushRig: rig, proxy: p}
}

// do sends a request. via is how it got here: "" is the operator's own browser on the machine, "share" is a request
// a proxy forwarded, and "overlay" arrives from another address.
func (pr *pushRoutes) do(via, method, path, body string) (int, string) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	switch via {
	case "":
		r.RemoteAddr = "127.0.0.1:5555"
		r.Host = "127.0.0.1:7777"
	case "share":
		r.RemoteAddr = "127.0.0.1:5555"
		r.Host = "board.example"
		r.Header.Set("X-Forwarded-For", "203.0.113.9")
	case "overlay":
		r.RemoteAddr = "192.0.2.7:5555"
	}
	pr.proxy.ServeHTTP(w, r)
	return w.Code, w.Body.String()
}

func (pr *pushRoutes) turnOn(t *testing.T) {
	t.Helper()
	if code, body := pr.do("", http.MethodPut, "/_hub/push", `{"enabled":true}`); code != 200 {
		t.Fatalf("turning push on: %d %s", code, body)
	}
}

func subBody(pr *pushRoutes, b browser, name string) string {
	return `{"endpoint":"` + pr.endpoint(name) + `","keys":{"p256dh":"` + b64.EncodeToString(b.priv.PublicKey().Bytes()) +
		`","auth":"` + b64.EncodeToString(b.auth) + `"},"label":"` + name + `","origin":"https://board.example"}`
}

// WITH X-Forwarded-For SET, PUT /_hub/push IS 403, and so is every other operator-only route.
func TestPushOperatorRoutesAreLocalOnly(t *testing.T) {
	pr := newPushRoutes(t)
	pr.turnOn(t)
	id := pr.subscribe(t, newBrowser(t), "own")
	for _, via := range []string{"share", "overlay"} {
		for _, c := range []struct{ method, path, body string }{
			{http.MethodPut, "/_hub/push", `{"enabled":false}`},
			{http.MethodPut, "/_hub/push", `{"rotate":true}`},
			{http.MethodPut, "/_hub/push", `{"contact":"https://x.example"}`},
			{http.MethodGet, "/_hub/push", ""},
			{http.MethodPost, "/_hub/push/test", ""},
			{http.MethodDelete, "/_hub/push/subscriptions/" + id, ""},
		} {
			if code, body := pr.do(via, c.method, c.path, c.body); code != http.StatusForbidden {
				t.Errorf("%s %s %s from %s: %d %s, not 403", c.method, c.path, c.body, via, code, body)
			}
		}
	}
	if !pr.p.On() {
		t.Error("a refused PUT switched push off")
	}
	if subs, _ := pr.st.PushSubs(); len(subs) != 1 {
		t.Errorf("a refused request changed the subscriptions: %d", len(subs))
	}
	// A page on another origin cannot write either, even from the machine.
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/_hub/push", strings.NewReader(`{"enabled":false}`))
	r.RemoteAddr, r.Host = "127.0.0.1:5555", "127.0.0.1:7777"
	r.Header.Set("Origin", "https://evil.example")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	pr.proxy.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden || !pr.p.On() {
		t.Errorf("a cross-origin PUT: %d, on=%v", w.Code, pr.p.On())
	}
}

func TestPushKeyIs404WhileOffAndServedWhileOn(t *testing.T) {
	pr := newPushRoutes(t)
	if code, _ := pr.do("share", http.MethodGet, "/_hub/push/key", ""); code != http.StatusNotFound {
		t.Fatalf("key while off: %d", code)
	}
	pr.turnOn(t)
	code, body := pr.do("share", http.MethodGet, "/_hub/push/key", "")
	var got struct{ Key string }
	_ = json.Unmarshal([]byte(body), &got)
	if code != 200 || len(unb64(t, got.Key)) != 65 {
		t.Fatalf("key while on: %d %s", code, body)
	}
	priv, _ := pr.st.Setting(settingPushPrivate)
	if strings.Contains(body, priv) {
		t.Fatal("the key route returned the private key")
	}
	// Off again: 404 again, and the key is kept.
	pr.do("", http.MethodPut, "/_hub/push", `{"enabled":false}`)
	if code, _ := pr.do("share", http.MethodGet, "/_hub/push/key", ""); code != http.StatusNotFound {
		t.Errorf("key after off: %d", code)
	}
	if again, _ := pr.st.Setting(settingPushPrivate); again != priv {
		t.Error("turning push off lost the key")
	}
}

// THE SHARE SUBSCRIBES AND UNSUBSCRIBES ITS OWN BROWSER, and the one test push follows the 201.
func TestPushShareSubscribesAndLeaves(t *testing.T) {
	pr := newPushRoutes(t)
	b := newBrowser(t)
	if code, _ := pr.do("share", http.MethodPost, "/_hub/push/subscriptions", subBody(pr, b, "phone")); code != http.StatusConflict {
		t.Fatalf("a subscribe while push is off: %d", code)
	}
	pr.turnOn(t)
	code, body := pr.do("share", http.MethodPost, "/_hub/push/subscriptions", subBody(pr, b, "phone"))
	var got struct{ ID string }
	_ = json.Unmarshal([]byte(body), &got)
	if code != http.StatusCreated || got.ID == "" {
		t.Fatalf("subscribe: %d %s", code, body)
	}
	until(t, "the test push", func() bool { return len(pr.svc.requests()) == 1 })
	var pay map[string]string
	_ = json.Unmarshal(decryptPush(t, pr.svc.requests()[0].body, b.priv, b.auth), &pay)
	if pay["title"] != "atrium test" {
		t.Errorf("the test push said %v", pay)
	}

	// The id is not a secret, so the share cannot remove by it. The endpoint is, so it can remove by that.
	if code, _ := pr.do("share", http.MethodDelete, "/_hub/push/subscriptions/"+got.ID, ""); code != http.StatusForbidden {
		t.Errorf("remove by id from the share: %d", code)
	}
	other := `{"endpoint":"` + pr.endpoint("someone-else") + `"}`
	if code, _ := pr.do("share", http.MethodDelete, "/_hub/push/subscriptions", other); code != http.StatusNotFound {
		t.Errorf("remove of an endpoint nobody holds: %d", code)
	}
	if code, _ := pr.do("share", http.MethodDelete, "/_hub/push/subscriptions", `{}`); code != http.StatusBadRequest {
		t.Errorf("remove with no endpoint: %d", code)
	}
	if subs, _ := pr.st.PushSubs(); len(subs) != 1 {
		t.Fatal("a refused removal removed it")
	}
	own := `{"endpoint":"` + pr.endpoint("phone") + `"}`
	if code, _ := pr.do("share", http.MethodDelete, "/_hub/push/subscriptions", own); code != 200 {
		t.Errorf("remove by endpoint: %d", code)
	}
	if subs, _ := pr.st.PushSubs(); len(subs) != 0 {
		t.Error("it is still there")
	}
}

func TestPushSubscribeRoutesRefuseWhatTheyShould(t *testing.T) {
	pr := newPushRoutes(t)
	pr.turnOn(t)
	pr.p.allow = pushEndpointOK
	b := newBrowser(t)
	bad := `{"endpoint":"https://evilpush.apple.com.example/x","keys":{"p256dh":"` + b64.EncodeToString(b.priv.PublicKey().Bytes()) +
		`","auth":"` + b64.EncodeToString(b.auth) + `"},"label":"x"}`
	code, body := pr.do("share", http.MethodPost, "/_hub/push/subscriptions", bad)
	if code != http.StatusBadRequest || !strings.Contains(body, `"error"`) {
		t.Errorf("an endpoint off the allowlist: %d %s", code, body)
	}
	if code, _ := pr.do("share", http.MethodPost, "/_hub/push/subscriptions", `{`); code != http.StatusBadRequest {
		t.Errorf("bad JSON: %d", code)
	}
	// The ninth, with the cap's own words.
	pr.p.allow = func(string) error { return nil }
	for i := 0; i < hubstore.PushMax; i++ {
		pr.subscribe(t, newBrowser(t), "d"+string(rune('a'+i)))
	}
	code, body = pr.do("share", http.MethodPost, "/_hub/push/subscriptions", subBody(pr, newBrowser(t), "ninth"))
	if code != http.StatusConflict ||
		!strings.Contains(body, "8 devices already get alerts. Remove one in the desktop gear first.") {
		t.Errorf("the ninth: %d %s", code, body)
	}
}

// THE LIST NEVER CARRIES AN ENDPOINT, A KEY OR THE PRIVATE KEY.
func TestPushListHoldsNoSecrets(t *testing.T) {
	pr := newPushRoutes(t)
	pr.turnOn(t)
	b := newBrowser(t)
	pr.subscribe(t, b, "phone")
	code, body := pr.do("", http.MethodGet, "/_hub/push", "")
	if code != 200 {
		t.Fatal(code, body)
	}
	priv, _ := pr.st.Setting(settingPushPrivate)
	for name, secret := range map[string]string{
		"the private key": priv,
		"the endpoint":    pr.endpoint("phone"),
		"its path":        "/push/phone",
		"the p256dh":      b64.EncodeToString(b.priv.PublicKey().Bytes()),
		"the auth secret": b64.EncodeToString(b.auth),
	} {
		if strings.Contains(body, secret) {
			t.Errorf("the list carries %s", name)
		}
	}
	var got struct {
		Enabled       bool
		Contact       string
		Max           int
		Subscriptions []pushSubView
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || got.Contact != pushDefaultSub || got.Max != 8 || len(got.Subscriptions) != 1 ||
		got.Subscriptions[0].Label != "phone" || got.Subscriptions[0].Service == "" {
		t.Errorf("%+v", got)
	}
}

func TestPushContactAndRotate(t *testing.T) {
	pr := newPushRoutes(t)
	pr.turnOn(t)
	pr.subscribe(t, newBrowser(t), "a")
	before, _ := pr.p.PublicKey()
	if code, _ := pr.do("", http.MethodPut, "/_hub/push", `{"contact":"not a url"}`); code != http.StatusBadRequest {
		t.Errorf("a bad contact: %d", code)
	}
	if code, _ := pr.do("", http.MethodPut, "/_hub/push", `{"contact":"https://example.invalid/me"}`); code != 200 {
		t.Errorf("a good contact: %d", code)
	}
	if pr.p.Contact() != "https://example.invalid/me" {
		t.Error("the contact was not kept")
	}
	if code, _ := pr.do("", http.MethodPut, "/_hub/push", `{"rotate":true}`); code != 200 {
		t.Fatalf("rotate: %d", code)
	}
	after, _ := pr.p.PublicKey()
	if before == after {
		t.Error("the key did not change")
	}
	if subs, _ := pr.st.PushSubs(); len(subs) != 0 {
		t.Error("subscriptions made against the old key were kept")
	}
	// A restart reads the new key from the store.
	again := NewPush(pr.st, nil)
	again.enabled = true
	if k, _ := again.PublicKey(); k != after {
		t.Error("the stored key is not the rotated one")
	}
}

func TestPushTestRouteSendsToEverySubscription(t *testing.T) {
	pr := newPushRoutes(t)
	if code, _ := pr.do("", http.MethodPost, "/_hub/push/test", ""); code != http.StatusConflict {
		t.Errorf("a test while off: %d", code)
	}
	pr.turnOn(t)
	pr.subscribe(t, newBrowser(t), "a")
	pr.subscribe(t, newBrowser(t), "b")
	code, body := pr.do("", http.MethodPost, "/_hub/push/test", "")
	if code != 200 || !strings.Contains(body, `"sent":2`) || !strings.Contains(body, `"ok":2`) {
		t.Errorf("%d %s", code, body)
	}
	if n := len(pr.svc.requests()); n != 2 {
		t.Errorf("%d pushes", n)
	}
}

func TestPushRemoveByIDFromTheOperator(t *testing.T) {
	pr := newPushRoutes(t)
	pr.turnOn(t)
	id := pr.subscribe(t, newBrowser(t), "a")
	if code, _ := pr.do("", http.MethodDelete, "/_hub/push/subscriptions/nope", ""); code != http.StatusNotFound {
		t.Errorf("an id nobody holds: %d", code)
	}
	if code, _ := pr.do("", http.MethodDelete, "/_hub/push/subscriptions/"+id, ""); code != 200 {
		t.Errorf("remove: %d", code)
	}
	if subs, _ := pr.st.PushSubs(); len(subs) != 0 {
		t.Error("still there")
	}
	// The store survived the 404 above: a refusal is an answer, not a halt.
	if halted, _ := pr.st.Halted(); halted {
		t.Error("an unknown id halted the store")
	}
}

func TestNoPushWiredIs404(t *testing.T) {
	p := NewProxy(NewHub(Timings{}), nil, "", nil)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/_hub/push/key", nil)
	r.RemoteAddr = "127.0.0.1:1"
	p.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("%d", w.Code)
	}
}
