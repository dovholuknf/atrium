package link

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

// fakePush is a push service. Nothing in these tests reaches a real one: the Push under test dials this, and its
// allowlist is swapped for one that lets this address through.
type fakePush struct {
	srv    *httptest.Server
	mu     sync.Mutex
	status int
	got    []fakeReq
}

type fakeReq struct {
	path string
	hdr  http.Header
	body []byte
}

func newFakePush(t *testing.T) *fakePush {
	t.Helper()
	f := &fakePush{status: http.StatusCreated}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.got = append(f.got, fakeReq{path: r.URL.Path, hdr: r.Header.Clone(), body: b})
		st := f.status
		f.mu.Unlock()
		w.WriteHeader(st)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakePush) setStatus(code int) { f.mu.Lock(); f.status = code; f.mu.Unlock() }

func (f *fakePush) requests() []fakeReq {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeReq(nil), f.got...)
}

// browser is a subscriber's key pair and auth secret.
type browser struct {
	priv *ecdh.PrivateKey
	auth []byte
}

func newBrowser(t *testing.T) browser {
	t.Helper()
	k, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	a := make([]byte, 16)
	_, _ = rand.Read(a)
	return browser{priv: k, auth: a}
}

func (b browser) sub(endpoint, label string) hubstore.PushSub {
	return hubstore.PushSub{Endpoint: endpoint, P256dh: b64.EncodeToString(b.priv.PublicKey().Bytes()),
		Auth: b64.EncodeToString(b.auth), Label: label, Origin: "https://board.example"}
}

type pushRig struct {
	st  *hubstore.Store
	p   *Push
	n   *Notifier
	svc *fakePush
	ns  *fakeStore
	now time.Time
}

func newPushRig(t *testing.T) *pushRig {
	t.Helper()
	st, err := hubstore.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ns := newFakeStore()
	n := NewNotifier(ns)
	rig := &pushRig{st: st, n: n, svc: newFakePush(t), ns: ns, now: time.Unix(1_800_000_000, 0)}
	rig.p = NewPush(st, n)
	rig.p.client = rig.svc.srv.Client()
	rig.p.client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	rig.p.allow = func(string) error { return nil }
	rig.p.now = func() time.Time { return rig.now }
	return rig
}

func (r *pushRig) endpoint(name string) string { return r.svc.srv.URL + "/push/" + name }

func (r *pushRig) subscribe(t *testing.T, b browser, name string) string {
	t.Helper()
	id, _, err := r.p.Subscribe(b.sub(r.endpoint(name), name))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func permNotice(card string) Notice {
	return Notice{Name: "the card", Reason: ReasonPermission, Card: "r1~" + card, Room: "r1"}
}

// THE WHOLE PATH: a notice goes out encrypted to the browser, with the headers the design names and a JWT the hub's
// key signed, and the browser's key decrypts exactly four plain fields.
func TestPushSendDecryptsAndSigns(t *testing.T) {
	rig := newPushRig(t)
	b := newBrowser(t)
	rig.subscribe(t, b, "phone")
	if err := rig.p.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("n", 90)
	res := pushSink{rig.p}.Send(context.Background(), Notice{Name: long, Reason: ReasonPermission, Card: "r1~c9", Room: "r1"})
	if !res.OK {
		t.Fatalf("send failed: %+v", res)
	}
	reqs := rig.svc.requests()
	if len(reqs) != 1 {
		t.Fatalf("%d pushes", len(reqs))
	}
	h := reqs[0].hdr
	if h.Get("Content-Encoding") != "aes128gcm" || h.Get("TTL") != "3600" || h.Get("Urgency") != "high" {
		t.Errorf("headers %v", h)
	}
	if want := pushTopic("r1~c9"); h.Get("Topic") != want || len(want) != 32 {
		t.Errorf("topic %q", h.Get("Topic"))
	}
	pub, _ := rig.p.PublicKey()
	jwt, k, ok := strings.Cut(strings.TrimPrefix(h.Get("Authorization"), "vapid t="), ", k=")
	if !ok || k != pub {
		t.Fatalf("authorization %q", h.Get("Authorization"))
	}
	u := rig.svc.srv.URL
	if err := verifyVAPID(jwt, k, u, pushDefaultSub, rig.now); err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := json.Unmarshal(decryptPush(t, reqs[0].body, b.priv, b.auth), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 || got["title"] != strings.Repeat("n", 60) || got["body"] != "wants permission" ||
		got["tag"] != "r1~c9" || got["path"] != "/m/#term=r1%7Ec9" && got["path"] != "/m/#term=r1~c9" {
		t.Errorf("payload %v", got)
	}
	// Whatever carries the hub's private key stays off the wire.
	priv, _ := rig.st.Setting(settingPushPrivate)
	if priv == "" {
		t.Fatal("no key was stored")
	}
	for _, s := range []string{h.Get("Authorization"), string(reqs[0].body)} {
		if strings.Contains(s, priv) {
			t.Fatal("the private key is on the wire")
		}
	}
}

func TestPushBodyPhrasesAndUrgency(t *testing.T) {
	rig := newPushRig(t)
	b := newBrowser(t)
	rig.subscribe(t, b, "phone")
	_ = rig.p.SetEnabled(true)
	for _, c := range []struct{ reason, body, urgency string }{
		{ReasonPermission, "wants permission", "high"},
		{ReasonQuestion, "asked you something", "normal"},
		{ReasonInput, "is waiting for you", "normal"},
		{ReasonFinished, "finished its turn", "normal"},
	} {
		rig.now = rig.now.Add(10 * time.Minute)
		before := len(rig.svc.requests())
		pushSink{rig.p}.Send(context.Background(), Notice{Name: "c", Reason: c.reason, Card: "r~" + c.reason, Room: "r"})
		reqs := rig.svc.requests()
		if len(reqs) != before+1 {
			t.Fatalf("%s: no push", c.reason)
		}
		last := reqs[len(reqs)-1]
		var got map[string]string
		_ = json.Unmarshal(decryptPush(t, last.body, b.priv, b.auth), &got)
		if got["body"] != c.body || last.hdr.Get("Urgency") != c.urgency {
			t.Errorf("%s: body %q urgency %q", c.reason, got["body"], last.hdr.Get("Urgency"))
		}
	}
}

func TestPushGoneDeletesTheRow(t *testing.T) {
	for _, code := range []int{http.StatusNotFound, http.StatusGone} {
		rig := newPushRig(t)
		rig.subscribe(t, newBrowser(t), "phone")
		_ = rig.p.SetEnabled(true)
		rig.svc.setStatus(code)
		pushSink{rig.p}.Send(context.Background(), permNotice("c1"))
		if subs, _ := rig.st.PushSubs(); len(subs) != 0 {
			t.Errorf("%d did not delete the subscription", code)
		}
	}
}

func TestPushThreeFailuresSwitchOneSubscriptionOff(t *testing.T) {
	rig := newPushRig(t)
	bad, good := newBrowser(t), newBrowser(t)
	rig.subscribe(t, bad, "bad")
	rig.subscribe(t, good, "good")
	_ = rig.p.SetEnabled(true)
	// Only the "bad" path fails.
	rig.svc.mu.Lock()
	rig.svc.status = http.StatusInternalServerError
	rig.svc.mu.Unlock()
	rig.p.client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/bad") {
			return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(""))}, nil
		}
		return rig.svc.srv.Client().Transport.RoundTrip(r)
	}), CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	rig.svc.setStatus(http.StatusCreated)

	for i := 0; i < 4; i++ {
		rig.now = rig.now.Add(10 * time.Minute)
		pushSink{rig.p}.Send(context.Background(), permNotice("c"+string(rune('a'+i))))
	}
	subs, _ := rig.st.PushSubs()
	var badSub, goodSub hubstore.PushSub
	for _, s := range subs {
		if s.Label == "bad" {
			badSub = s
		} else {
			goodSub = s
		}
	}
	if badSub.DisabledReason == "" || badSub.Failures != 3 {
		t.Errorf("the failing subscription: %+v", badSub)
	}
	if goodSub.DisabledReason != "" || goodSub.Failures != 0 {
		t.Errorf("the working subscription was touched: %+v", goodSub)
	}
	// Three sends reached it, and the fourth did not: it was switched off.
	toGood := 0
	for _, r := range rig.svc.requests() {
		if strings.HasSuffix(r.path, "/good") {
			toGood++
		}
	}
	if toGood != 4 {
		t.Errorf("%d pushes reached the working subscription, want 4", toGood)
	}
	if strings.Contains(badSub.DisabledReason, "127.0.0.1") || strings.Contains(badSub.DisabledReason, "/push/") {
		t.Errorf("the reason carries the endpoint: %q", badSub.DisabledReason)
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPushSuccessResetsTheCount(t *testing.T) {
	rig := newPushRig(t)
	rig.subscribe(t, newBrowser(t), "phone")
	_ = rig.p.SetEnabled(true)
	rig.svc.setStatus(500)
	for i := 0; i < 2; i++ {
		rig.now = rig.now.Add(10 * time.Minute)
		pushSink{rig.p}.Send(context.Background(), permNotice("c"))
	}
	rig.svc.setStatus(201)
	pushSink{rig.p}.Send(context.Background(), permNotice("c"))
	rig.svc.setStatus(500)
	pushSink{rig.p}.Send(context.Background(), permNotice("c"))
	subs, _ := rig.st.PushSubs()
	if subs[0].DisabledReason != "" || subs[0].Failures != 1 {
		t.Errorf("%+v", subs[0])
	}
}

// THE ALLOWLIST IS CHECKED AT SUBSCRIBE AND AGAIN AT EVERY SEND.
func TestPushAllowlistAtSubscribeAndAtSend(t *testing.T) {
	rig := newPushRig(t)
	rig.p.allow = pushEndpointOK
	b := newBrowser(t)
	if _, _, err := rig.p.Subscribe(b.sub("https://evil.example/x", "x")); err == nil {
		t.Fatal("an endpoint off the allowlist was stored")
	}
	if _, _, err := rig.p.Subscribe(b.sub("https://web.push.apple.com/abc", "ok")); err != nil {
		t.Fatal(err)
	}
	// A row stored while the allowlist was looser, sent to once it is tighter.
	rig.p.allow = func(string) error { return nil }
	rig.subscribe(t, b, "local")
	_ = rig.p.SetEnabled(true)
	rig.p.allow = pushEndpointOK
	pushSink{rig.p}.Send(context.Background(), permNotice("c"))
	if n := len(rig.svc.requests()); n != 0 {
		t.Fatalf("%d pushes went to a host the allowlist refuses", n)
	}
}

func TestPushSubscribeRefusesBadKeys(t *testing.T) {
	rig := newPushRig(t)
	b := newBrowser(t)
	s := b.sub(rig.endpoint("x"), "x")
	s.P256dh = "AAAA"
	if _, _, err := rig.p.Subscribe(s); err == nil {
		t.Error("a short p256dh was accepted")
	}
	s = b.sub(rig.endpoint("x"), "x")
	s.Auth = "AAAA"
	if _, _, err := rig.p.Subscribe(s); err == nil {
		t.Error("a short auth was accepted")
	}
}

func TestPushCapIsEightAndNeverEvicts(t *testing.T) {
	rig := newPushRig(t)
	var first string
	for i := 0; i < hubstore.PushMax; i++ {
		id := rig.subscribe(t, newBrowser(t), "d"+string(rune('a'+i)))
		if i == 0 {
			first = id
		}
	}
	_, _, err := rig.p.Subscribe(newBrowser(t).sub(rig.endpoint("ninth"), "ninth"))
	if err == nil || err.Error() != "8 devices already get alerts. Remove one in the desktop gear first." {
		t.Fatalf("the ninth: %v", err)
	}
	subs, _ := rig.st.PushSubs()
	if len(subs) != 8 || subs[0].ID != first {
		t.Fatalf("%d subscriptions, oldest kept: %v", len(subs), subs[0].ID == first)
	}
	// The same browser again is not a new device, and takes no slot.
	if _, added, err := rig.p.Subscribe(newBrowser(t).sub(rig.endpoint("da"), "renamed")); err != nil || added {
		t.Errorf("a repeat subscribe: added=%v err=%v", added, err)
	}
}

func TestPushNewDeviceIsAnnounced(t *testing.T) {
	rig := newPushRig(t)
	var lines []string
	rig.p.announce = func(id, label, origin string) { lines = append(lines, label+"|"+origin) }
	b := newBrowser(t)
	rig.subscribe(t, b, "pixel")
	rig.subscribe(t, b, "pixel")
	if len(lines) != 1 || lines[0] != "pixel|https://board.example" {
		t.Errorf("announced %v", lines)
	}
}

// MORE THAN FIVE IN TWO MINUTES TO ONE SUBSCRIPTION IS ONE SUMMARY, until two quiet minutes pass.
func TestPushBurstBecomesASummary(t *testing.T) {
	rig := newPushRig(t)
	b := newBrowser(t)
	rig.subscribe(t, b, "phone")
	_ = rig.p.SetEnabled(true)
	send := func(card string) map[string]string {
		before := len(rig.svc.requests())
		pushSink{rig.p}.Send(context.Background(), permNotice(card))
		reqs := rig.svc.requests()
		if len(reqs) != before+1 {
			t.Fatalf("no push for %s", card)
		}
		var got map[string]string
		_ = json.Unmarshal(decryptPush(t, reqs[len(reqs)-1].body, b.priv, b.auth), &got)
		return got
	}
	for i := 0; i < 5; i++ {
		rig.now = rig.now.Add(10 * time.Second)
		if got := send("c" + string(rune('a'+i))); got["tag"] == pushSummaryTag {
			t.Fatalf("push %d was already a summary", i+1)
		}
	}
	rig.now = rig.now.Add(10 * time.Second)
	if got := send("c6"); got["tag"] != pushSummaryTag || got["title"] != "6 cards want you" {
		t.Fatalf("the sixth: %v", got)
	}
	rig.now = rig.now.Add(100 * time.Second)
	if got := send("c7"); got["tag"] != pushSummaryTag || got["title"] != "7 cards want you" {
		t.Fatalf("the seventh, still inside two quiet minutes of the sixth: %v", got)
	}
	rig.now = rig.now.Add(3 * time.Minute)
	if got := send("c8"); got["tag"] == pushSummaryTag || got["title"] != "the card" {
		t.Fatalf("after two quiet minutes: %v", got)
	}
}

// THE COMMAND SINK'S FAILURES AND PUSH'S ARE COUNTED APART.
func TestPushAndCommandCountFailuresApart(t *testing.T) {
	st := newFakeStore()
	n := NewNotifier(st)
	cmd := &recSink{fail: true}
	n.SetSink(cmd)
	pushed := &recSink{}
	pushOn := true
	n.AddSink("push", pushed, func() bool { return pushOn })
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	n.Start(ctx)
	if err := n.Configure(true, []string{"x"}); err != nil {
		// commandFound looks for the program: the test binary is one.
		t.Skip(err)
	}
	_ = n.Arm()
	for i := 0; i < 4; i++ {
		n.enqueue(Notice{Name: "n", Reason: ReasonPermission, Card: "r~c", Room: "r"})
	}
	until(t, "the command sink to switch off", func() bool { return !n.Status().Enabled })
	until(t, "push to take all four", func() bool { return pushed.count() == 4 })
	if f := n.SinkFailures("push"); f != 0 {
		t.Errorf("push counted %d failures for the command's", f)
	}
	// And the other way round: push failing does not touch the command's count.
	st2 := newFakeStore()
	n2 := NewNotifier(st2)
	ok := &recSink{}
	n2.SetSink(ok)
	bad := &recSink{fail: true}
	n2.AddSink("push", bad, func() bool { return true })
	n2.Start(ctx)
	_ = n2.Configure(true, []string{"x"})
	for i := 0; i < 5; i++ {
		n2.enqueue(Notice{Name: "n", Reason: ReasonPermission, Card: "r~c", Room: "r"})
	}
	until(t, "five pushes", func() bool { return bad.count() == 5 })
	if s := n2.Status(); !s.Enabled || s.Failures != 0 {
		t.Errorf("push failures reached the command: %+v", s)
	}
	if f := n2.SinkFailures("push"); f != 5 {
		t.Errorf("push counted %d failures, want 5", f)
	}
}

// TURNING PUSH ON SEEDS THE TRIGGER, so what already waits does not flood the phone, and a card that changes after
// does notify. The command is off throughout.
func TestPushArmsTheTriggerWithoutTheCommand(t *testing.T) {
	st := newFakeStore()
	st.cache["r"] = []CardState{permCard("old", "T1")}
	n := NewNotifier(st)
	pushed := &recSink{}
	on := false
	n.AddSink("push", pushed, func() bool { return on })
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	n.Start(ctx)
	n.Announced("r", []CardState{permCard("old", "T1")})
	if st.records != 0 {
		t.Fatal("the trigger ran while nothing was listening")
	}
	if err := n.Arm(); err != nil {
		t.Fatal(err)
	}
	on = true
	n.Announced("r", []CardState{permCard("old", "T1"), permCard("new", "T2")})
	until(t, "the new card to be pushed", func() bool { return pushed.count() == 1 })
	settle()
	if pushed.count() != 1 || pushed.got[0].Card != "r~new" {
		t.Errorf("pushed %+v", pushed.got)
	}
}
