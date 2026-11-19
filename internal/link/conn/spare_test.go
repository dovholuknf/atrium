package conn

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"io"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/link/wire"
)

type noDialer struct{ t *testing.T }

func (d noDialer) Dial(context.Context) (net.Conn, error) {
	d.t.Error("a relay dialled with a spare on offer")
	return nil, net.ErrClosed
}
func (noDialer) Describe() string { return "none" }

// answerRelay is a hub that welcomes one relay and answers its request with op.
func answerRelay(b net.Conn, got chan<- string) {
	defer b.Close()
	dec := json.NewDecoder(b)
	var hi hello
	if dec.Decode(&hi) != nil {
		return
	}
	_ = json.NewEncoder(b).Encode(welcome{OK: true})
	var req wire.RelayRequest
	_ = dec.Decode(&req)
	got <- req.Op
	_ = json.NewEncoder(b).Encode(map[string]any{"ok": true})
}

func offerSpare(r *Room, c net.Conn) {
	r.spare, r.spareAt, r.spareTaken = c, time.Now(), make(chan struct{}, 1)
}

func TestARelaySaysItsHelloOnTheSpare(t *testing.T) {
	a, b := net.Pipe()
	r := &Room{Name: "sgg", up: true, Dial: noDialer{t}, T: DefaultTimings()}
	offerSpare(r, a)
	got := make(chan string, 1)
	go answerRelay(b, got)
	if _, err := r.Relay(context.Background(), wire.RelayRequest{Op: RelayHub}); err != nil {
		t.Fatal(err)
	}
	if op := <-got; op != RelayHub {
		t.Fatalf("the hub read op %q, want %q", op, RelayHub)
	}
	if r.spare != nil {
		t.Fatal("the spare was still on offer after a relay took it")
	}
}

func TestASpareTheHubHungUpOnFallsBackToAFreshDial(t *testing.T) {
	dead, hub := net.Pipe()
	hub.Close()
	a, b := net.Pipe()
	r := &Room{Name: "sgg", up: true, Dial: pipeDialer{a}, T: DefaultTimings()}
	offerSpare(r, dead)
	got := make(chan string, 1)
	go answerRelay(b, got)
	if _, err := r.Relay(context.Background(), wire.RelayRequest{Op: RelaySay}); err != nil {
		t.Fatalf("a dead spare failed the relay: %v", err)
	}
	if op := <-got; op != RelaySay {
		t.Fatalf("the fresh dial carried op %q, want %q", op, RelaySay)
	}
}

func TestAnExpiredSpareIsNotUsed(t *testing.T) {
	a, _ := net.Pipe()
	r := &Room{}
	offerSpare(r, a)
	r.spareAt = time.Now().Add(-spareLife)
	if r.takeSpare() != nil {
		t.Fatal("a spare past spareLife was handed out")
	}
}

type chanDialer chan net.Conn

func (d chanDialer) Dial(context.Context) (net.Conn, error) {
	a, _ := net.Pipe()
	d <- a
	return a, nil
}
func (chanDialer) Describe() string { return "chan" }

func TestAPushKeepsASpareAndReplacesATakenOne(t *testing.T) {
	dials := make(chanDialer, 2)
	r := &Room{Dial: dials, T: DefaultTimings()}
	r.gitMoved.Store(time.Now().UnixNano())
	r.keepSpare()
	r.keepSpare()
	first := waitDial(t, dials)
	var c net.Conn
	for deadline := time.Now().Add(5 * time.Second); c == nil && time.Now().Before(deadline); {
		c = r.takeSpare()
	}
	if c != first {
		t.Fatal("the spare dialled was not the one handed out")
	}
	waitDial(t, dials)
	r.gitMoved.Store(0)
	for deadline := time.Now().Add(5 * time.Second); r.takeSpare() == nil && time.Now().Before(deadline); {
	}
}

func waitDial(t *testing.T, dials chanDialer) net.Conn {
	t.Helper()
	select {
	case c := <-dials:
		return c
	case <-time.After(5 * time.Second):
		t.Fatal("no spare was dialled during a push")
		return nil
	}
}

func selfSigned(t *testing.T, cn string) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: cn},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func hubWaitFor(t *testing.T, clientCerts []tls.Certificate) time.Duration {
	t.Helper()
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	srv := tls.Server(b, &tls.Config{Certificates: []tls.Certificate{selfSigned(t, "hub")},
		ClientAuth: tls.RequestClientCert, MinVersion: tls.VersionTLS13})
	go func() {
		_ = tls.Client(a, &tls.Config{InsecureSkipVerify: true, Certificates: clientCerts}).Handshake()
	}()
	return helloWait(srv)
}

func TestTheHubWaitsLongerForAHelloOnlyFromAProvenRoom(t *testing.T) {
	if w := hubWaitFor(t, []tls.Certificate{selfSigned(t, "sgg")}); w != wire.SpareWait {
		t.Fatalf("a connection with a room certificate waits %v for its hello, want %v", w, wire.SpareWait)
	}
	if w := hubWaitFor(t, nil); w != handshakeWait {
		t.Fatalf("a connection with no certificate waits %v for its hello, want %v", w, handshakeWait)
	}
	a, _ := net.Pipe()
	if w := helloWait(a); w != handshakeWait {
		t.Fatalf("a plain connection waits %v for its hello, want %v", w, handshakeWait)
	}
}

func TestManySmallPushWritesStartASpare(t *testing.T) {
	dials := make(chanDialer, 2)
	r := &Room{Dial: dials, T: DefaultTimings()}
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	go func() { _, _ = io.Copy(io.Discard, b) }()
	push := movingConn{&buffered{Conn: a, br: bufio.NewReader(a)}, r, new(int64)}
	piece := make([]byte, 4<<10)
	for sent := 0; sent < spareFrom-len(piece); sent += len(piece) {
		if _, err := push.Write(piece); err != nil {
			t.Fatal(err)
		}
	}
	if r.spareRun.Load() {
		t.Fatal("a spare started before the connection had written a push's worth")
	}
	if _, err := push.Write(piece); err != nil {
		t.Fatal(err)
	}
	waitDial(t, dials)
	r.gitMoved.Store(0)
	for deadline := time.Now().Add(5 * time.Second); r.takeSpare() == nil && time.Now().Before(deadline); {
	}
}
