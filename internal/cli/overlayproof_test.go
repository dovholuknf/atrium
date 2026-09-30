package cli

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link"
)

// runLegacy runs `rooms legacy` against a store in dir. The board address points
// at a port nothing listens on, so the nudge cannot reach a real hub.
func runLegacy(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	c := roomLegacyCmd("")
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&out)
	c.SetArgs(append(args, "--dir", dir, "--board-addr", "127.0.0.1:1"))
	err := c.Execute()
	return out.String(), err
}

func TestRoomsLegacyVerbSetsShowsAndFlipsBack(t *testing.T) {
	dir := t.TempDir()

	out, err := runLegacy(t, dir)
	if err != nil || !strings.HasPrefix(out, "allow") {
		t.Fatalf("default: %q, %v, want allow", out, err)
	}
	if out, err = runLegacy(t, dir, "refuse"); err != nil || !strings.HasPrefix(out, "refuse") {
		t.Fatalf("set refuse: %q, %v", out, err)
	}
	if out, err = runLegacy(t, dir); err != nil || !strings.HasPrefix(out, "refuse") {
		t.Fatalf("show after refuse: %q, %v", out, err)
	}
	store, err := hubstore.Open(filepath.Join(dir, "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	refused, err := store.OverlayLegacyRefused()
	store.Close()
	if err != nil || !refused {
		t.Fatalf("the store says refused=%v, %v, want true", refused, err)
	}
	if out, err = runLegacy(t, dir, "allow"); err != nil || !strings.HasPrefix(out, "allow") {
		t.Fatalf("flip back: %q, %v", out, err)
	}
	if out, err = runLegacy(t, dir); err != nil || !strings.HasPrefix(out, "allow") {
		t.Fatalf("show after allow: %q, %v", out, err)
	}
}

func TestRoomsLegacyVerbRejectsJunk(t *testing.T) {
	dir := t.TempDir()
	if _, err := runLegacy(t, dir, "sometimes"); err == nil {
		t.Fatal("a value that is neither allow nor refuse was accepted")
	}
	if out, _ := runLegacy(t, dir); !strings.HasPrefix(out, "allow") {
		t.Fatalf("junk changed the setting: %q", out)
	}
}

// writeStrayRoomCert puts a valid room.crt and room.key in keys.Dir, the way an
// earlier direct enrolment to some other hub would have left them.
func writeStrayRoomCert(t *testing.T, keys link.Keys) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "stray"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kb, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(keys.Dir, "room.crt"),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(keys.Dir, "room.key"),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}), 0o600); err != nil {
		t.Fatal(err)
	}
}

func provenZitiJoin(t *testing.T) link.Join {
	t.Helper()
	hub := link.Keys{Dir: t.TempDir()}
	if err := hub.EnsureCA(nil); err != nil {
		t.Fatal(err)
	}
	tok, err := hub.MintProvenOverlayToken("ziti", "vm1", "atrium-hub", "", "secret")
	if err != nil {
		t.Fatal(err)
	}
	j, err := link.ParseToken(tok)
	if err != nil {
		t.Fatal(err)
	}
	return j
}

// An old-form join never wraps, whatever is in the keys directory. A stray
// room.crt from a direct enrolment, maybe to another hub, must not start a TLS
// handshake against the wrong CA and drop the room.
func TestRoomDialerNeverWrapsAnOldFormJoin(t *testing.T) {
	keys := link.Keys{Dir: t.TempDir()}
	writeStrayRoomCert(t, keys)
	if !keys.HasRoomCert() {
		t.Fatal("the stray certificate was not readable, so the test proves nothing")
	}
	old, err := link.MintOverlayToken("ziti", "vm1", "atrium-hub", "")
	if err != nil {
		t.Fatal(err)
	}
	j, err := link.ParseToken(old)
	if err != nil {
		t.Fatal(err)
	}
	d, err := roomDialer(j, keys, "vm1.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, wrapped := d.(link.Proven); wrapped {
		t.Fatal("an old-form overlay join with a stray room.crt was wrapped in TLS")
	}
}

func TestRoomDialerWrapsANewFormJoinOnlyWithACertificate(t *testing.T) {
	j := provenZitiJoin(t)
	if !j.Proven() {
		t.Fatal("a new-form join did not parse as proven")
	}
	keys := link.Keys{Dir: t.TempDir()}

	d, err := roomDialer(j, keys, "vm1.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, wrapped := d.(link.Proven); wrapped {
		t.Fatal("a room with no certificate yet was wrapped, and it has nothing to present")
	}

	writeStrayRoomCert(t, keys)
	d, err = roomDialer(j, keys, "vm1.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, wrapped := d.(link.Proven); !wrapped {
		t.Fatal("a new-form join with a certificate was not wrapped")
	}
}

func TestJoinStringForZitiMintsTheProvenForm(t *testing.T) {
	keys := link.Keys{Dir: t.TempDir()}
	store, err := hubstore.Open(filepath.Join(keys.Dir, "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	r, err := store.Add("vm1", hubstore.TransportZiti)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := joinStringFor(keys, store, r, "", "", "ziti", "atrium-hub"); err == nil {
		t.Fatal("minted with no certificate authority")
	}
	if err := keys.EnsureCA(nil); err != nil {
		t.Fatal(err)
	}
	line, err := joinStringFor(keys, store, r, "", "", "ziti", "atrium-hub")
	if err != nil {
		t.Fatal(err)
	}
	j, err := link.ParseToken(line)
	if err != nil {
		t.Fatal(err)
	}
	if j.Transport != "ziti" || j.Service != "atrium-hub" || !j.Proven() {
		t.Fatalf("minted %+v, want ziti, the service, and a proven join", j)
	}
}

// The board is served over an overlay to browsers, which hold no client
// certificate. Only the room-link listener may peek and wrap, so the wrapper
// is named nowhere outside the link package and the file that builds the hub's
// room listener.
func TestOnlyTheRoomLinkListenerIsWrapped(t *testing.T) {
	roots := []string{".", "../daemon", "../api"}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			if root == "." && name == "transport.go" {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(root, name))
			if err != nil {
				t.Fatal(err)
			}
			for _, banned := range []string{"MixedListener(", "overlayListen("} {
				if strings.Contains(string(raw), banned) {
					t.Errorf("%s/%s names %s, which belongs to the room-link listener only",
						root, name, banned)
				}
			}
		}
	}
}
