package link

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// THE CERTIFICATE NAMES A ROOM ON AN OVERLAY, AND THE OLD PATH STILL WORKS.
//
// A loopback listener stands in for the overlay. What it stands in for is only
// "a listener that hands back connections and a dialer that returns them", which
// is all `Ziti` and `Zrok` are to the code under test. The real SDKs need a
// controller or an account, so nothing here can reach one.

// rig is a hub on a mixed listener, with the direct transport's enrolment.
type rig struct {
	t    *testing.T
	hub  *Hub
	addr string
	keys Keys
	d    Direct
	stop context.CancelFunc

	mu       sync.Mutex
	secrets  map[string]string
	refuse   bool
	unproven []string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{t: t, keys: hubKeys(t), secrets: map[string]string{}}
	r.d = Direct{Keys: r.keys, Spend: func(secret string) (string, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		name, ok := r.secrets[secret]
		if !ok {
			return "", errors.New("that join string has been used already, or it expired")
		}
		delete(r.secrets, secret)
		return name, nil
	}}
	cfg, err := r.d.ServerTLS()
	if err != nil {
		t.Fatal(err)
	}
	inner, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln := MixedListener(inner, cfg, "zrok")
	r.addr = inner.Addr().String()

	r.hub = NewHub(Timings{Beat: 200 * time.Millisecond, Silence: time.Second, Warm: 1})
	r.hub.Enrol = r.d.ServeEnrolment
	r.hub.Authenticated = OverlayAuthenticated
	r.hub.LegacyRefused = func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.refuse
	}
	r.hub.OnUnproven = func(name, over string) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.unproven = append(r.unproven, name+"@"+over)
	}
	ctx, stop := context.WithCancel(context.Background())
	r.stop = stop
	go func() { _ = r.hub.Serve(ctx, ln) }()
	t.Cleanup(func() { stop(); _ = ln.Close() })
	return r
}

func (r *rig) secretFor(name string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := "secret-for-" + name
	r.secrets[s] = name
	return s
}

func (r *rig) setRefuse(v bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refuse = v
}

// enrol makes a room's keys the way `atrium room join` does, over the "overlay".
func (r *rig) enrol(name, secret string) (Keys, error) {
	rk := Keys{Dir: r.t.TempDir()}
	pin, err := r.keys.Fingerprint()
	if err != nil {
		r.t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = EnrolOver(ctx, plain{addr: r.addr}, rk, pin, name, secret)
	return rk, err
}

func (r *rig) attach(claim string, dial Dialer) {
	r.t.Helper()
	room := &Room{
		Name: claim, Dial: dial, Handler: http.NotFoundHandler(),
		T: Timings{Beat: 200 * time.Millisecond, Warm: 1, Backoff: 50 * time.Millisecond},
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = room.Run(ctx) }()
	r.t.Cleanup(cancel)
}

// hello dials with no room certificate and says one frame, answering the welcome.
func (r *rig) rawHello(overTLS bool, kind, name string) (welcome, error) {
	var w welcome
	c, err := net.Dial("tcp", r.addr)
	if err != nil {
		return w, err
	}
	defer c.Close()
	var conn net.Conn = c
	if overTLS {
		conn, err = handshakeClient(context.Background(), c, &tls.Config{
			MinVersion: tls.VersionTLS13, InsecureSkipVerify: true,
		})
		if err != nil {
			return w, err
		}
	}
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if err := writeJSON(conn, hello{V: Version, Kind: kind, Room: name, Session: "x"}); err != nil {
		return w, err
	}
	return w, readJSON(bufio.NewReader(conn), &w)
}

func (r *rig) attachedAs(name string) (Attached, bool) {
	for _, a := range r.hub.Rooms() {
		if strings.EqualFold(a.Name, name) {
			return a, true
		}
	}
	return Attached{}, false
}

// A room with a certificate for alpha that says beta in its hello attaches as alpha.
func TestOverlayCertificateNamesTheRoomNotTheHello(t *testing.T) {
	r := newRig(t)
	rk, err := r.enrol("alpha", r.secretFor("alpha"))
	if err != nil {
		t.Fatalf("enrolment over the overlay failed: %v", err)
	}
	if !rk.HasRoomCert() {
		t.Fatal("the room kept no certificate")
	}
	r.attach("beta", Proven{Dialer: plain{addr: r.addr}, Keys: rk})
	waitFor(t, 5*time.Second, func() bool { return r.hub.Has("alpha") })
	if r.hub.Has("beta") {
		t.Fatal("the room attached under the name it claimed rather than its certificate's")
	}
	a, _ := r.attachedAs("alpha")
	if !a.Proven {
		t.Fatal("a room the certificate names was listed as unproven")
	}
	if len(r.unproven) != 0 {
		t.Fatalf("a proven room wrote an unproven line: %v", r.unproven)
	}
}

// No certificate on the new path is refused, for control and for data.
func TestOverlayTLSWithoutACertificateIsRefused(t *testing.T) {
	r := newRig(t)
	for _, kind := range []string{"control", "data"} {
		w, err := r.rawHello(true, kind, "alpha")
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if w.OK || !strings.Contains(w.Error, "no credential") {
			t.Fatalf("%s without a certificate was answered %+v", kind, w)
		}
	}
	if r.hub.Has("alpha") {
		t.Fatal("a room with no certificate attached on the new path")
	}
}

// Enrolment over the overlay spends the secret once.
func TestOverlayEnrolmentSpendsTheSecretOnce(t *testing.T) {
	r := newRig(t)
	secret := r.secretFor("alpha")
	if _, err := r.enrol("alpha", secret); err != nil {
		t.Fatal(err)
	}
	if _, err := r.enrol("alpha", secret); err == nil {
		t.Fatal("a join secret was spent twice")
	}
}

// Enrolment cannot run on the old path, where nothing pins the hub.
func TestEnrolmentIsRefusedWithoutTLS(t *testing.T) {
	r := newRig(t)
	w, err := r.rawHello(false, "enrol", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if w.OK || !strings.Contains(w.Error, "TLS") {
		t.Fatalf("enrolment on the old path was answered %+v", w)
	}
}

// An old-path room attaches as today in allow mode, is marked unproven and
// audited, and is turned away in refuse mode with the re-join sentence.
func TestOldPathAttachesUntilTheOperatorRefusesIt(t *testing.T) {
	r := newRig(t)
	r.attach("old", plain{addr: r.addr})
	waitFor(t, 5*time.Second, func() bool { return r.hub.Has("old") })
	a, _ := r.attachedAs("old")
	if a.Proven {
		t.Fatal("a room with no certificate was listed as proven")
	}
	waitFor(t, 5*time.Second, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return len(r.unproven) == 1
	})
	if r.unproven[0] != "old@zrok" {
		t.Fatalf("the unproven line named %v", r.unproven)
	}

	r.setRefuse(true)
	w, err := r.rawHello(false, "control", "late")
	if err != nil {
		t.Fatal(err)
	}
	if w.OK || !strings.Contains(w.Error, "atrium rooms token") || !strings.Contains(w.Error, "late") {
		t.Fatalf("the old path was not turned away with the sentence: %+v", w)
	}

	r.setRefuse(false)
	w, err = r.rawHello(false, "control", "late")
	if err != nil || !w.OK {
		t.Fatalf("flipping back to allow did not let the old path in: %+v %v", w, err)
	}
}

// Refuse mode leaves a room WITH a certificate alone.
func TestRefusingTheOldPathKeepsProvenRooms(t *testing.T) {
	r := newRig(t)
	r.setRefuse(true)
	rk, err := r.enrol("alpha", r.secretFor("alpha"))
	if err != nil {
		t.Fatal(err)
	}
	r.attach("alpha", Proven{Dialer: plain{addr: r.addr}, Keys: rk})
	waitFor(t, 5*time.Second, func() bool { return r.hub.Has("alpha") })
}

// One listener serves both kinds of room at the same time.
func TestOneOverlayListenerServesBothKindsAtOnce(t *testing.T) {
	r := newRig(t)
	rk, err := r.enrol("alpha", r.secretFor("alpha"))
	if err != nil {
		t.Fatal(err)
	}
	r.attach("alpha", Proven{Dialer: plain{addr: r.addr}, Keys: rk})
	r.attach("old", plain{addr: r.addr})
	waitFor(t, 5*time.Second, func() bool { return r.hub.Has("alpha") && r.hub.Has("old") })
	pa, _ := r.attachedAs("alpha")
	po, _ := r.attachedAs("old")
	if !pa.Proven || po.Proven {
		t.Fatalf("proven flags were alpha=%v old=%v", pa.Proven, po.Proven)
	}
}

// THE PEEK LOOKS AT A BYTE AND GIVES IT BACK, for both kinds.
func TestThePeekDoesNotEatTheFirstByte(t *testing.T) {
	inner, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Direct{Keys: hubKeys(t)}.ServerTLS()
	if err != nil {
		t.Fatal(err)
	}
	ln := MixedListener(inner, cfg, "ziti")
	defer ln.Close()

	// The old path: what was written is what is read, from its first byte.
	go func() {
		c, err := net.Dial("tcp", inner.Addr().String())
		if err == nil {
			_, _ = c.Write([]byte("{\"v\":1}\n"))
			defer c.Close()
			time.Sleep(time.Second)
		}
	}()
	conn, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if over, ok := legacyTransport(conn); !ok || over != "ziti" {
		t.Fatalf("a connection with no TLS was %T", conn)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	got := make([]byte, 8)
	if _, err := io.ReadFull(conn, got); err != nil || string(got) != "{\"v\":1}\n" {
		t.Fatalf("the first byte was eaten: %q %v", got, err)
	}

	// The new path: the ClientHello arrives whole, so the handshake completes.
	done := make(chan error, 1)
	go func() {
		c, err := net.Dial("tcp", inner.Addr().String())
		if err != nil {
			done <- err
			return
		}
		tc, err := handshakeClient(context.Background(), c, &tls.Config{
			MinVersion: tls.VersionTLS13, InsecureSkipVerify: true,
		})
		if err == nil {
			_ = tc.Close()
		}
		done <- err
	}()
	conn2, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer conn2.Close()
	if _, ok := conn2.(*tls.Conn); !ok {
		t.Fatalf("a ClientHello was %T", conn2)
	}
	_ = conn2.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, _ = conn2.Read(make([]byte, 1))
	if err := <-done; err != nil {
		t.Fatalf("the handshake did not complete after the peek: %v", err)
	}
}

// A caller that connects and says nothing must not stall the accept loop.
func TestASilentCallerDoesNotBlockTheOthers(t *testing.T) {
	r := newRig(t)
	silent, err := net.Dial("tcp", r.addr)
	if err != nil {
		t.Fatal(err)
	}
	defer silent.Close()
	r.attach("old", plain{addr: r.addr})
	waitFor(t, 5*time.Second, func() bool { return r.hub.Has("old") })
}

// The new form of a join string round-trips, the old form is still understood,
// and half of the new one is a damaged string.
func TestOverlayJoinStringForms(t *testing.T) {
	k := hubKeys(t)
	line, err := k.MintProvenOverlayToken("zrok", "sparta", "", "share1", "a-secret")
	if err != nil {
		t.Fatal(err)
	}
	j, err := ParseToken(line)
	if err != nil {
		t.Fatal(err)
	}
	fp, _ := k.Fingerprint()
	if !j.Proven() || j.Secret != "a-secret" || j.Pin != fp || j.ShareToken != "share1" || j.Name != "sparta" {
		t.Fatalf("the new form did not round trip: %+v", j)
	}
	line, err = k.MintProvenOverlayToken("ziti", "sparta", "svc", "", "a-secret")
	if err != nil {
		t.Fatal(err)
	}
	if j, err = ParseToken(line); err != nil || !j.Proven() || j.Service != "svc" {
		t.Fatalf("the ziti form did not round trip: %+v %v", j, err)
	}

	old, err := MintOverlayToken("zrok", "sparta", "", "share1")
	if err != nil {
		t.Fatal(err)
	}
	if j, err = ParseToken(old); err != nil || j.Proven() {
		t.Fatalf("the old form must parse and be unproven: %+v %v", j, err)
	}

	half := tokenPrefix + encodeToken(t, token{T: "zrok", N: "sparta", K: "share1", S: "a-secret"})
	if _, err := ParseToken(half); err == nil {
		t.Fatal("a string with a secret and no pin was accepted")
	}
	if _, err := k.MintProvenOverlayToken("zrok", "sparta", "", "share1", ""); err == nil {
		t.Fatal("a new-form string was minted with no secret")
	}
}
