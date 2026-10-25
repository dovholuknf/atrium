package transport

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"math/big"
	"net"
	"testing"
	"time"
)

func pinnedHub(t *testing.T) (net.Listener, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "hub"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true,
		KeyUsage:    x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.(*tls.Conn).Handshake()
		}
	}()
	sum := sha256.Sum256(der)
	return ln, base64.RawURLEncoding.EncodeToString(sum[:])
}

// A moved hub's hostname whose cached answer is dead is asked of DNS again.
func TestADirectHostnameIsResolvedAfreshWhenItsCachedAnswerIsDead(t *testing.T) {
	ln, pin := pinnedHub(t)
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	var asked string
	d := Direct{Addr: net.JoinHostPort("atrium-hub.invalid", port), Keys: Keys{Dir: t.TempDir()}, Pin: pin,
		Lookup: func(_ context.Context, host string) ([]string, error) {
			asked = host
			return []string{"127.0.0.1"}, nil
		}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := d.Dial(ctx)
	if err != nil {
		t.Fatalf("the fresh answer was not dialled: %v", err)
	}
	_ = c.Close()
	if asked != "atrium-hub.invalid" {
		t.Fatalf("asked DNS for %q", asked)
	}
}

func TestADirectIPIsNeverLookedUp(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	d := Direct{Addr: addr, Keys: Keys{Dir: t.TempDir()}, Pin: "x", Lookup: func(context.Context, string) ([]string, error) {
		t.Error("an IP address was looked up")
		return nil, nil
	}}
	if _, err := d.Dial(context.Background()); err == nil {
		t.Fatal("a closed port answered")
	}
}
