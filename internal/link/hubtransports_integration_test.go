//go:build integration

package link

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeBinds struct {
	mu   sync.Mutex
	lns  []net.Listener
	fail error
	ids  []string
}

func (f *fakeBinds) listen(service, identity string) (net.Listener, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ids = append(f.ids, service+"|"+identity)
	if f.fail != nil {
		return nil, f.fail
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err == nil {
		f.lns = append(f.lns, ln)
	}
	return ln, err
}

func (f *fakeBinds) binds() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.lns)
}

func (f *fakeBinds) last() net.Listener {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lns[len(f.lns)-1]
}

func waitUntil(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond) // wait: poll step under one deadline
	}
	t.Fatalf("timed out waiting for %s", what)
}

func accepts(ln net.Listener) bool {
	c, err := net.DialTimeout("tcp", ln.Addr().String(), 200*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

type transportRig struct {
	t      *testing.T
	p      *Proxy
	hub    *Hub
	tr     *HubTransports
	st     *fakeSettings
	front  *httptest.Server
	board  *fakeBinds
	ci     *fakeBinds
	zrok   *fakeBinds
	closed []string
	mu     sync.Mutex
}

func newTransportRig(t *testing.T, seeds map[string]TransportSeed) *transportRig {
	t.Helper()
	x := &transportRig{t: t, st: &fakeSettings{m: map[string]string{}}, board: &fakeBinds{}, ci: &fakeBinds{}, zrok: &fakeBinds{}}
	x.hub = NewHub(Timings{})
	x.p = NewProxy(x.hub, nil, "", nil)
	x.tr = &HubTransports{
		Store: x.st, Hub: x.hub, IdentityFallback: "C:/ids/hub.json",
		Wait: func(ctx context.Context, d time.Duration) bool { return waitTimer(ctx, time.Millisecond) },
		CloseIdentity: func(id string) {
			x.mu.Lock()
			x.closed = append(x.closed, id)
			x.mu.Unlock()
		},
	}
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	x.tr.Add(TransportItem{Name: TransportBoardZiti, Label: "the board over ziti", Kind: "board-ziti",
		DefaultService: "atrium-board", Listen: x.board.listen, Handler: ok}, seeds[TransportBoardZiti])
	x.tr.Add(TransportItem{Name: TransportCIHook, Label: "the CI hook over ziti", Kind: CIListenerKind,
		DefaultService: "ziti-hook", Listen: x.ci.listen, Handler: ok}, seeds[TransportCIHook])
	x.tr.Add(TransportItem{Name: TransportRoomZrok, Label: "the room link over zrok", Kind: "zrok",
		NoIdentity: true, Listen: x.zrok.listen, Handler: ok}, seeds[TransportRoomZrok])
	x.p.SetTransports(x.tr)
	x.front = httptest.NewServer(x.p)
	t.Cleanup(x.front.Close)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	x.tr.Start(ctx)
	return x
}

func (x *transportRig) view(name string) map[string]any {
	x.t.Helper()
	_, got := do(x.t, http.MethodGet, x.front.URL+"/_hub/transports", "", "")
	for _, v := range got["transports"].([]any) {
		m := v.(map[string]any)
		if m["name"] == name {
			return m
		}
	}
	x.t.Fatalf("no transport %q in %v", name, got)
	return nil
}

func (x *transportRig) put(body string) (int, map[string]any) {
	return do(x.t, http.MethodPut, x.front.URL+"/_hub/transports", "application/json", body)
}

func TestTheBoardsZitiAndTheCIHookSwitchOffAndOnWithNoRestart(t *testing.T) {
	x := newTransportRig(t, nil)
	if x.board.binds() != 0 || x.ci.binds() != 0 {
		t.Fatal("a transport nobody turned on was bound")
	}
	for _, c := range []struct {
		name string
		f    *fakeBinds
	}{{TransportBoardZiti, x.board}, {TransportCIHook, x.ci}} {
		if code, got := x.put(`{"name":"` + c.name + `","on":true,"identity":"C:/ids/a.json"}`); code != http.StatusOK {
			t.Fatalf("%s on answered %d %v", c.name, code, got)
		}
		waitUntil(t, c.name+" bound", func() bool { return c.f.binds() == 1 })
		waitUntil(t, c.name+" state", func() bool { return x.view(c.name)["state"] == "bound" })
		first := c.f.last()
		if !accepts(first) {
			t.Fatalf("%s is bound and does not accept", c.name)
		}

		if code, got := x.put(`{"name":"` + c.name + `","on":false}`); code != http.StatusOK {
			t.Fatalf("%s off answered %d %v", c.name, code, got)
		}
		if accepts(first) {
			t.Fatalf("%s still accepts after it was turned off", c.name)
		}
		if v := x.view(c.name); v["state"] != "off" || v["on"] != false {
			t.Fatalf("%s reads %v after off", c.name, v)
		}

		if code, got := x.put(`{"name":"` + c.name + `","on":true}`); code != http.StatusOK {
			t.Fatalf("%s on again answered %d %v", c.name, code, got)
		}
		waitUntil(t, c.name+" rebound", func() bool { return c.f.binds() == 2 })
		if !accepts(c.f.last()) || c.f.last() == first {
			t.Fatalf("%s was not bound afresh", c.name)
		}
		waitUntil(t, c.name+" state again", func() bool { return x.view(c.name)["state"] == "bound" })
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	if len(x.closed) != 1 || x.closed[0] != "C:/ids/a.json" {
		t.Fatalf("the ziti context was closed for %v", x.closed)
	}
}

func TestTheContextStaysWhileAnotherTransportUsesTheIdentity(t *testing.T) {
	x := newTransportRig(t, nil)
	x.put(`{"name":"board-ziti","on":true}`)
	x.put(`{"name":"ci-hook","on":true}`)
	waitUntil(t, "both bound", func() bool { return x.board.binds() == 1 && x.ci.binds() == 1 })
	x.put(`{"name":"board-ziti","on":false}`)
	x.mu.Lock()
	n := len(x.closed)
	x.mu.Unlock()
	if n != 0 {
		t.Fatalf("the context was closed while the CI hook still uses it")
	}
	x.put(`{"name":"ci-hook","on":false}`)
	x.mu.Lock()
	defer x.mu.Unlock()
	if len(x.closed) != 1 || x.closed[0] != "C:/ids/hub.json" {
		t.Fatalf("closed %v, want the hub's identity once", x.closed)
	}
}

func TestAFlagSeedsAndTheStoredSettingAppliesWithNone(t *testing.T) {
	on := true
	x := newTransportRig(t, map[string]TransportSeed{TransportCIHook: {On: &on, Service: "hook-from-flag"}})
	waitUntil(t, "the seeded hook bound", func() bool { return x.ci.binds() == 1 })
	if !strings.HasPrefix(x.ci.ids[0], "hook-from-flag|") {
		t.Fatalf("bound %v, want the flag's service", x.ci.ids)
	}
	v := x.view(TransportCIHook)
	if seeded, _ := v["seeded"].([]any); len(seeded) != 2 {
		t.Fatalf("the view does not say what the flag set: %v", v["seeded"])
	}
	x.put(`{"name":"ci-hook","on":false}`)
	if v := x.view(TransportCIHook); v["seeded"] != nil {
		t.Fatalf("a flag is still shown as winning after the operator turned it off: %v", v["seeded"])
	}
	if x.st.m["hub_transport.ci-hook.on"] != "off" {
		t.Fatalf("the choice was not stored: %v", x.st.m)
	}

	stored := &transportRig{t: t, st: &fakeSettings{m: map[string]string{
		"hub_transport.board-ziti.on": "on", "hub_transport.board-ziti.service": "stored-board",
	}}, board: &fakeBinds{}}
	tr := &HubTransports{Store: stored.st, Wait: func(ctx context.Context, d time.Duration) bool { return waitTimer(ctx, time.Millisecond) }}
	tr.Add(TransportItem{Name: TransportBoardZiti, Kind: "board-ziti", DefaultService: "atrium-board",
		Listen: stored.board.listen, Handler: http.NotFoundHandler()}, TransportSeed{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tr.Start(ctx)
	waitUntil(t, "the stored board bound", func() bool { return stored.board.binds() == 1 })
	if !strings.HasPrefix(stored.board.ids[0], "stored-board|") {
		t.Fatalf("bound %v, want the stored service", stored.board.ids)
	}
}

func TestARefusingControllerShowsFailingAndBacksOffToACap(t *testing.T) {
	var waits []time.Duration
	var mu sync.Mutex
	f := &fakeBinds{fail: errors.New("the controller refused the connection")}
	tr := &HubTransports{Store: &fakeSettings{m: map[string]string{}}, First: 30 * time.Second, Cap: 10 * time.Minute,
		IdentityFallback: "C:/ids/hub.json",
		Wait: func(ctx context.Context, d time.Duration) bool {
			mu.Lock()
			waits = append(waits, d)
			mu.Unlock()
			return waitTimer(ctx, time.Millisecond)
		}}
	tr.Add(TransportItem{Name: TransportBoardZiti, Kind: "board-ziti", DefaultService: "atrium-board",
		Listen: f.listen, Handler: http.NotFoundHandler()}, TransportSeed{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tr.Start(ctx)
	if _, err := tr.Set(TransportBoardZiti, boolp(true), nil, nil); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "eight waits", func() bool { mu.Lock(); defer mu.Unlock(); return len(waits) >= 8 })
	v := tr.View()[0]
	if v.State != "failing" || !strings.Contains(v.LastError, "refused") {
		t.Fatalf("view %+v", v)
	}
	mu.Lock()
	defer mu.Unlock()
	if waits[0] != 30*time.Second || waits[5] != 10*time.Minute || waits[7] != 10*time.Minute {
		t.Fatalf("waits %v", waits)
	}
}

func boolp(b bool) *bool { return &b }

func TestTurningOffTheRoomLinksZitiIsRefusedWhileARoomIsOnlyOnIt(t *testing.T) {
	h := NewHub(Timings{})
	h.rooms[keyOf("m1mini")] = &attached{name: "m1mini", via: "ziti",
		paths: []PathState{{Kind: "direct", State: PathDown}, {Kind: "ziti", State: PathOn}}}
	h.rooms[keyOf("sg3")] = &attached{name: "sg3", via: "ziti",
		paths: []PathState{{Kind: "direct", State: PathUp}, {Kind: "ziti", State: PathOn}}}
	got := h.OnlyVia("ziti")
	if len(got) != 1 || got[0] != "m1mini" {
		t.Fatalf("only on ziti: %v", got)
	}

	f := &fakeBinds{}
	tr := &HubTransports{Store: &fakeSettings{m: map[string]string{}}, Hub: h, IdentityFallback: "C:/ids/hub.json",
		CloseIdentity: func(string) {},
		Wait:          func(ctx context.Context, d time.Duration) bool { return waitTimer(ctx, time.Millisecond) }}
	tr.Add(TransportItem{Name: TransportRoomZiti, Kind: "ziti", DefaultService: "atrium-hub", Listen: f.listen,
		Serve: func(ctx context.Context, ln net.Listener) error { <-ctx.Done(); return ln.Close() },
		Refuse: func() string {
			if rs := h.OnlyVia("ziti"); len(rs) > 0 {
				return "refused: " + strings.Join(rs, ", ") + " reaches this hub only over ziti"
			}
			return ""
		}}, TransportSeed{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tr.Start(ctx)
	if _, err := tr.Set(TransportRoomZiti, boolp(true), nil, nil); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "bound", func() bool { return f.binds() == 1 })
	_, err := tr.Set(TransportRoomZiti, boolp(false), nil, nil)
	if err == nil || !strings.Contains(err.Error(), "m1mini") || !strings.HasPrefix(err.Error(), "refused") {
		t.Fatalf("turning off answered %v", err)
	}
	if !accepts(f.last()) {
		t.Fatal("a refused turn off closed the listener")
	}
	delete(h.rooms, keyOf("m1mini"))
	if _, err := tr.Set(TransportRoomZiti, boolp(false), nil, nil); err != nil {
		t.Fatalf("with no room only on ziti it answered %v", err)
	}
}

func TestOnlyTheLocalOperatorSwitchesATransport(t *testing.T) {
	x := newTransportRig(t, nil)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/_hub/transports", strings.NewReader(`{"name":"ci-hook","on":true}`))
	r.RemoteAddr = "192.0.2.7:5555"
	x.p.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("a PUT from elsewhere answered %d", w.Code)
	}
	if x.ci.binds() != 0 {
		t.Fatal("a refused PUT bound a listener")
	}
	if code, _ := x.put(`{"name":"nope","on":true}`); code != http.StatusNotFound {
		t.Fatalf("an unknown transport answered %d", code)
	}
}

func TestTheZrokRoomLinkSwitchesWithNoIdentityAndTouchesNoZitiContext(t *testing.T) {
	x := newTransportRig(t, map[string]TransportSeed{TransportRoomZrok: {On: boolp(true)}})
	waitUntil(t, "zrok bound", func() bool { return x.zrok.binds() == 1 })
	v := x.view(TransportRoomZrok)
	if v["no_service"] != true || v["seeded"] == nil {
		t.Fatalf("the zrok row reads %v, want no_service and a seeded on", v)
	}
	if code, got := x.put(`{"name":"room-zrok","on":false}`); code != http.StatusOK {
		t.Fatalf("zrok off answered %d %v", code, got)
	}
	if x.view(TransportRoomZrok)["state"] != "off" {
		t.Fatal("zrok did not turn off")
	}
	if code, _ := x.put(`{"name":"room-zrok","on":true,"service":"x"}`); code != http.StatusBadRequest {
		t.Fatalf("a service for a zrok share answered %d, want 400", code)
	}
	if code, got := x.put(`{"name":"room-zrok","on":true}`); code != http.StatusOK {
		t.Fatalf("zrok on answered %d %v", code, got)
	}
	waitUntil(t, "zrok rebound", func() bool { return x.zrok.binds() == 2 })
	x.mu.Lock()
	defer x.mu.Unlock()
	if len(x.closed) != 0 {
		t.Fatalf("a ziti context was closed for a zrok share: %v", x.closed)
	}
}
