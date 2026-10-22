//go:build integration

package link

import (
	"bufio"
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

type countingListener struct {
	net.Listener
	accepted atomic.Int32
}

func (c *countingListener) Accept() (net.Conn, error) {
	conn, err := c.Listener.Accept()
	if err == nil {
		c.accepted.Add(1)
	}
	return conn, err
}

type muxRig struct {
	hub   *Hub
	tcp   *countingListener
	mux   *muxListener
	room  Direct
	front *httptest.Server
}

// newMuxRig is a hub on direct and a room holding a certificate for "testroom". old serves the hub without the mux,
// as a hub from before it does. dial, when set, is the room's dialer instead of the room's Direct.
func newMuxRig(t *testing.T, old bool, dial func(Direct) Dialer) *muxRig {
	t.Helper()
	hk := hubKeys(t)
	cfg, err := Direct{Keys: hk}.ServerTLS()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	r := &muxRig{tcp: &countingListener{Listener: raw}}
	var ln net.Listener = tls.NewListener(r.tcp, cfg)
	if !old {
		r.mux = newMuxListener(ln)
		ln = r.mux
	}
	rk := Keys{Dir: t.TempDir()}
	key, csr, err := NewCSR("testroom")
	if err != nil {
		t.Fatal(err)
	}
	der, err := hk.sign(csr, "testroom")
	if err != nil {
		t.Fatal(err)
	}
	ca, _, err := hk.CA()
	if err != nil {
		t.Fatal(err)
	}
	if err := rk.SaveRoom(key, der, ca.Raw, raw.Addr().String(), "testroom"); err != nil {
		t.Fatal(err)
	}
	r.hub = NewHub(Timings{Beat: 200 * time.Millisecond, Silence: time.Second, Warm: 2})
	r.hub.Authenticated = DirectAuthenticated
	ctx, stop := context.WithCancel(context.Background())
	go func() { _ = r.hub.Serve(ctx, ln) }()
	r.room = Direct{Addr: raw.Addr().String(), Keys: rk}
	var d Dialer = r.room
	if dial != nil {
		d = dial(r.room)
	}
	room := &Room{
		Name: "not-what-the-certificate-says",
		Dial: d,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("ok"))
		}),
		T: Timings{Beat: 200 * time.Millisecond, Warm: 2, Backoff: 50 * time.Millisecond},
	}
	go func() { _ = room.Run(ctx) }()
	waitFor(t, 5*time.Second, func() bool { return r.hub.Has("testroom") })
	r.front = httptest.NewServer(NewProxy(r.hub, nil, "", nil))
	t.Cleanup(func() {
		r.front.Close()
		stop()
		_ = ln.Close()
	})
	return r
}

func (r *muxRig) get(t *testing.T, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		req, _ := http.NewRequest("GET", r.front.URL+"/v1/tasks", nil)
		req.Header.Set("X-Atrium-Room", "testroom")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("request %d through the hub answered %d", i, resp.StatusCode)
		}
	}
}

func TestADirectRoomRidesOneConnectionNamedByItsCertificate(t *testing.T) {
	r := newMuxRig(t, false, nil)
	r.get(t, 50)
	for i := 0; i < 50; i++ {
		c, err := r.room.Dial(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		_ = c.Close()
	}
	if n := r.tcp.accepted.Load(); n != 1 {
		t.Fatalf("the hub accepted %d TCP connections from one room, want 1", n)
	}
	if n := r.mux.Sessions(); n != 1 {
		t.Fatalf("the hub holds %d sessions, want 1", n)
	}
	if n := muxFor(r.room).Opened(); n != 1 {
		t.Fatalf("the room opened %d sessions, want 1", n)
	}
}

func TestARoomOnAHubWithoutTheMuxDialsAConnectionEach(t *testing.T) {
	r := newMuxRig(t, true, nil)
	r.get(t, 5)
	if n := r.tcp.accepted.Load(); n < 3 {
		t.Fatalf("the old hub accepted %d connections, want one per dial", n)
	}
	if muxFor(r.room).Opened() != 0 || !muxFor(r.room).refusedRecently() {
		t.Fatal("the room did not fall back after the old hub refused the mux")
	}
}

type oldDirect struct{ Direct }

func (d oldDirect) Dial(ctx context.Context) (net.Conn, error) { return d.dialTLS(ctx) }

func TestARoomFromBeforeTheMuxIsServedByANewHub(t *testing.T) {
	r := newMuxRig(t, false, func(d Direct) Dialer { return oldDirect{d} })
	r.get(t, 5)
	if n := r.mux.Sessions(); n != 0 {
		t.Fatalf("the hub holds %d sessions for a room that never asked for one", n)
	}
	if n := r.tcp.accepted.Load(); n < 3 {
		t.Fatalf("the hub accepted %d connections, want one per dial", n)
	}
}

func TestAConnectionWithoutACertificateIsRefusedTheMux(t *testing.T) {
	r := newMuxRig(t, false, nil)
	cfg, err := Direct{Keys: r.room.Keys}.clientTLS()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Certificates = nil
	c, err := tls.Dial("tcp", r.room.Addr, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := writeJSON(c, hello{V: Version, Kind: muxKind}); err != nil {
		t.Fatal(err)
	}
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	var w welcome
	if err := readJSON(bufio.NewReader(c), &w); err != nil || w.OK {
		t.Fatalf("got %+v (%v), want a refusal", w, err)
	}
}

func TestAStreamCannotJoinOrNestAnotherMux(t *testing.T) {
	r := newMuxRig(t, false, nil)
	for _, kind := range []string{"enrol", muxKind} {
		c, err := r.room.Dial(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		br := bufio.NewReader(c)
		if _, err := sayHello(c, br, hello{Kind: kind, Room: "testroom"}); err == nil {
			t.Fatalf("a stream said %q and the hub took it", kind)
		}
		_ = c.Close()
	}
}
