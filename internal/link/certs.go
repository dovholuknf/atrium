package link

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Certificates, and one string to paste.
//
// ── the standard this is held to ─────────────────────────
//
// `atrium2 hub` prints a line. You paste that line into `atrium2 join` on the
// other machine. That is the whole setup. Nobody types a path, nobody copies a
// file, nobody learns what a CSR is.
//
// Everything below exists to make those two commands true, and the cost is
// admitted openly: this mints its own certificate authority, which is a thing
// a program should be slightly embarrassed to do. It is here because it is the
// shortest path to a mutually authenticated connection with no dependency and
// no service to stand up, and because the design is arranged so that swapping
// it for an OpenZiti service deletes this file rather than rewriting the rest.
//
// ── what is actually guaranteed ──────────────────────────
//
//   - The hub proves itself to the room by a certificate whose issuer the room
//     PINNED from the join string. A man in the middle needs the CA's key.
//   - The room proves itself to the hub by a client certificate the hub itself
//     signed, and the name in it is the room's identity. The name in the hello
//     frame is a label and is never trusted. See `hearHello`.
//   - THE ROOM'S PRIVATE KEY NEVER LEAVES THE ROOM. It makes a key, signs a
//     request with it, and the hub signs the request. The hub cannot
//     impersonate a room even if you took its disk.
//   - A join string is good ONCE and for an hour. Pasting it twice fails, which
//     is how you find out it went somewhere it should not have.

const (
	// caLife is long because rotating a private CA that nothing else trusts is
	// a chore with no benefit. The join secret is the short-lived thing.
	caLife = 10 * 365 * 24 * time.Hour
	// tokenLife bounds a pasted string. Long enough to walk to another machine,
	// short enough that one left in a chat log is not a key.
	tokenLife = time.Hour
	// tokenPrefix makes the string recognisable in a terminal and gives a
	// future format somewhere to go.
	tokenPrefix = "atr1_"
)

// Keys is one side's material on disk.
type Keys struct {
	Dir string
}

func (k Keys) path(name string) string { return filepath.Join(k.Dir, name) }

// ── the certificate authority, which only a hub has ──────

// EnsureCA makes the hub's authority and its own certificate, once.
//
// Idempotent, because `atrium2 hub` is a command somebody runs repeatedly and
// re-minting a CA would orphan every room that already joined.
func (k Keys) EnsureCA(hosts []string) error {
	if err := os.MkdirAll(k.Dir, 0o700); err != nil {
		return err
	}
	if _, err := os.Stat(k.path("ca.crt")); err == nil {
		// Already have one. The server certificate is refreshed against the
		// current host list anyway, since a machine's name or address can
		// change and a hub that cannot prove its own address is unreachable.
		return k.ensureServer(hosts)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial(),
		Subject:               pkix.Name{CommonName: "atrium hub authority"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(caLife),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		// One level. A room certificate signs nothing.
		MaxPathLen:     0,
		MaxPathLenZero: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return err
	}
	if err := k.writeCert("ca.crt", der); err != nil {
		return err
	}
	if err := k.writeKey("ca.key", key); err != nil {
		return err
	}
	return k.ensureServer(hosts)
}

// ensureServer mints the hub's own certificate against the addresses it can be
// reached at.
func (k Keys) ensureServer(hosts []string) error {
	ca, caKey, err := k.CA()
	if err != nil {
		return err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial(),
		Subject:      pkix.Name{CommonName: "atrium hub"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(caLife),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else if h != "" {
			tmpl.DNSNames = append(tmpl.DNSNames, h)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		return err
	}
	if err := k.writeCert("hub.crt", der); err != nil {
		return err
	}
	return k.writeKey("hub.key", key)
}

// CA reads the authority back.
func (k Keys) CA() (*x509.Certificate, *ecdsa.PrivateKey, error) {
	certPEM, err := os.ReadFile(k.path("ca.crt"))
	if err != nil {
		return nil, nil, err
	}
	keyPEM, err := os.ReadFile(k.path("ca.key"))
	if err != nil {
		return nil, nil, err
	}
	cert, err := parseCert(certPEM)
	if err != nil {
		return nil, nil, err
	}
	blk, _ := pem.Decode(keyPEM)
	if blk == nil {
		return nil, nil, errors.New("the authority key is not readable")
	}
	key, err := x509.ParseECPrivateKey(blk.Bytes)
	if err != nil {
		return nil, nil, err
	}
	return cert, key, nil
}

// Fingerprint is the SHA-256 of the authority certificate, which is what a room
// pins. Of the DER, not the PEM: whitespace must not change an identity.
func (k Keys) Fingerprint() (string, error) {
	cert, _, err := k.CA()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(cert.Raw)
	return base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

// ── the join string ──────────────────────────────────────

// token is what gets pasted. Short field names because this is printed in a
// terminal and read by a person deciding whether it looks like a secret.
//
// ONE STRING FOR ALL THREE TRANSPORTS, so `atrium2 join <thing>` is the only
// command anybody learns. Which transport it is arrives in the string rather
// than in a flag the person pasting would have to be told about separately.
type token struct {
	// T is the transport: empty or "direct", "ziti", "zrok".
	T string `json:"t,omitempty"`
	A string `json:"a,omitempty"` // direct: address, host:port
	F string `json:"f,omitempty"` // direct: CA fingerprint
	S string `json:"s,omitempty"` // direct: one-time secret
	V string `json:"v,omitempty"` // ziti: the service name
	K string `json:"k,omitempty"` // zrok: the private share token
}

// Join is a parsed join string, in the terms the caller needs.
type Join struct {
	Transport string
	// Direct.
	Addr, Pin, Secret string
	// Ziti.
	Service string
	// Zrok.
	ShareToken string
}

// pending is a join secret waiting to be spent.
//
// ON DISK, NOT IN MEMORY, and that is the whole reason this file exists rather
// than a map. The hub is the half being restarted constantly. A token minted,
// printed, and then invalidated because somebody restarted the hub before
// pasting it is a setup that fails for a reason nobody would guess.
type pending struct {
	Hash    string    `json:"hash"`
	Expires time.Time `json:"expires"`
}

// MintToken produces the line a human pastes.
func (k Keys) MintToken(addr string) (string, error) {
	fp, err := k.Fingerprint()
	if err != nil {
		return "", err
	}
	var raw [24]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	secret := base64.RawURLEncoding.EncodeToString(raw[:])

	// THE SECRET IS HASHED BEFORE IT IS STORED, the same way a password is. A
	// hub's state directory is not a place a working credential belongs, and
	// the hub never needs the original: it only ever compares.
	sum := sha256.Sum256([]byte(secret))
	list, _ := k.pendings()
	list = append(keepLive(list), pending{
		Hash:    base64.RawURLEncoding.EncodeToString(sum[:]),
		Expires: time.Now().Add(tokenLife),
	})
	if err := k.savePendings(list); err != nil {
		return "", err
	}

	body, err := json.Marshal(token{T: "direct", A: addr, F: fp, S: secret})
	if err != nil {
		return "", err
	}
	return tokenPrefix + base64.RawURLEncoding.EncodeToString(body), nil
}

// MintOverlayToken makes a join string for a transport that carries its own
// identity.
//
// NO SECRET AND NO FINGERPRINT, because there is nothing for them to do. Under
// ziti a policy decided who may dial before any of this ran, and under zrok the
// share token IS the credential. Minting a second one here would be ceremony
// that looks like security.
func MintOverlayToken(kind, service, shareToken string) (string, error) {
	t := token{T: kind}
	switch kind {
	case "ziti":
		if strings.TrimSpace(service) == "" {
			return "", errors.New("a ziti join string needs a service name")
		}
		t.V = service
	case "zrok":
		if strings.TrimSpace(shareToken) == "" {
			return "", errors.New("a zrok join string needs a share token")
		}
		t.K = shareToken
	default:
		return "", errors.New("no transport called " + kind)
	}
	body, err := json.Marshal(t)
	if err != nil {
		return "", err
	}
	return tokenPrefix + base64.RawURLEncoding.EncodeToString(body), nil
}

// ParseToken reads one back, with a sentence rather than a decoder error when
// somebody pastes half of it.
func ParseToken(s string) (Join, error) {
	var j Join
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, tokenPrefix) {
		return j, errors.New(
			"that does not look like a join string. it starts with " + tokenPrefix +
				" and comes from `atrium2 hub` on the machine running the hub")
	}
	body, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(s, tokenPrefix))
	if err != nil {
		return j, errors.New("that join string is damaged. copy the whole line")
	}
	var t token
	if err := json.Unmarshal(body, &t); err != nil {
		return j, errors.New("that join string is damaged. copy the whole line")
	}
	j.Transport = t.T
	if j.Transport == "" {
		// Written before there was more than one. Read as direct rather than
		// refused, so a string minted by an older hub still works.
		j.Transport = "direct"
	}
	j.Addr, j.Pin, j.Secret = t.A, t.F, t.S
	j.Service, j.ShareToken = t.V, t.K

	switch j.Transport {
	case "direct":
		if j.Addr == "" || j.Pin == "" || j.Secret == "" {
			return j, errors.New("that join string is missing part of itself")
		}
	case "ziti":
		if j.Service == "" {
			return j, errors.New("that ziti join string names no service")
		}
	case "zrok":
		if j.ShareToken == "" {
			return j, errors.New("that zrok join string carries no share token")
		}
	default:
		return j, errors.New("that join string is for a transport this build does not have: " +
			j.Transport)
	}
	return j, nil
}

// spendLock serialises the read-modify-write on `pending.json`.
//
// WITHOUT IT, "GOOD ONCE" IS NOT TRUE. Enrolment runs in a goroutine per
// connection, so two can interleave: both read the same list, the first writes
// it back without secret X, and the second writes back ITS copy, which still
// has X in it. A spent secret is resurrected on disk and works a second time.
//
// A process-wide mutex rather than a file lock, because exactly one hub ever
// owns a state directory. If that stops being true this needs to become a lock
// on the file.
var spendLock sync.Mutex

// spend consumes a secret, once.
func (k Keys) spend(secret string) error {
	spendLock.Lock()
	defer spendLock.Unlock()

	sum := sha256.Sum256([]byte(secret))
	want := base64.RawURLEncoding.EncodeToString(sum[:])

	list, err := k.pendings()
	if err != nil {
		return errors.New("this hub has no join strings outstanding")
	}
	live := keepLive(list)
	for i, p := range live {
		// Constant time, because this compares a secret and the cost of doing
		// it properly is one function call.
		if subtle.ConstantTimeCompare([]byte(p.Hash), []byte(want)) == 1 {
			live = append(live[:i], live[i+1:]...)
			return k.savePendings(live)
		}
	}
	// ONLY WRITTEN WHEN SOMETHING EXPIRED. Rewriting the file on every failed
	// attempt would let anybody who can open a connection force disk writes in
	// a loop, and enrolment is deliberately reachable without a credential.
	if len(live) != len(list) {
		_ = k.savePendings(live)
	}
	return errors.New("that join string has been used already, or it expired. " +
		"run `atrium2 hub token` for a fresh one")
}

func keepLive(list []pending) []pending {
	out := list[:0]
	for _, p := range list {
		if time.Now().Before(p.Expires) {
			out = append(out, p)
		}
	}
	return out
}

func (k Keys) pendings() ([]pending, error) {
	raw, err := os.ReadFile(k.path("pending.json"))
	if err != nil {
		return nil, err
	}
	var out []pending
	return out, json.Unmarshal(raw, &out)
}

func (k Keys) savePendings(list []pending) error {
	raw, err := json.Marshal(list)
	if err != nil {
		return err
	}
	return os.WriteFile(k.path("pending.json"), raw, 0o600)
}

// ── enrolment ────────────────────────────────────────────

// enrolReq is what a joining room sends.
type enrolReq struct {
	Secret string `json:"secret"`
	Room   string `json:"room"`
	// CSR is DER. The key that made it stays on the room.
	CSR []byte `json:"csr"`
}

// enrolResp is what it gets back.
type enrolResp struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	Cert  []byte `json:"cert,omitempty"`
	CA    []byte `json:"ca,omitempty"`
}

// NewCSR makes a room's key and a signing request for it.
func NewCSR(room string) (*ecdsa.PrivateKey, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: room},
	}, key)
	return key, der, err
}

// sign turns a room's request into a certificate.
//
// THE NAME IS TAKEN FROM THE REQUEST AND NOTHING ELSE IS. Whatever else a CSR
// carries, only the common name and the public key survive into the
// certificate, because everything else would be a field a room chose about
// itself and the hub then treated as true.
func (k Keys) sign(csrDER []byte, room string) ([]byte, error) {
	csr, err := x509.ParseCertificateRequest(csrDER)
	if err != nil {
		return nil, errors.New("that signing request could not be read")
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, errors.New("that signing request is not signed by its own key")
	}
	ca, caKey, err := k.CA()
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(room)
	if name == "" {
		name = strings.TrimSpace(csr.Subject.CommonName)
	}
	if name == "" {
		return nil, errors.New("a room needs a name")
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial(),
		Subject:      pkix.Name{CommonName: name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(caLife),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	return x509.CreateCertificate(rand.Reader, tmpl, ca, csr.PublicKey, caKey)
}

// SaveRoom writes what a room got back from enrolment.
func (k Keys) SaveRoom(key *ecdsa.PrivateKey, certDER, caDER []byte, hub, room string) error {
	if err := os.MkdirAll(k.Dir, 0o700); err != nil {
		return err
	}
	if err := k.writeCert("room.crt", certDER); err != nil {
		return err
	}
	if err := k.writeCert("ca.crt", caDER); err != nil {
		return err
	}
	if err := k.writeKey("room.key", key); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(map[string]string{"hub": hub, "room": room}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(k.path("room.json"), raw, 0o600)
}

// SaveOverlayRoom writes what a room needs for a transport that carries its own
// identity. There is no certificate to keep, only where the hub is.
func (k Keys) SaveOverlayRoom(j Join, room, identity string) error {
	if err := os.MkdirAll(k.Dir, 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(map[string]string{
		"transport": j.Transport, "room": room,
		"service": j.Service, "share": j.ShareToken,
		"identity": identity, "hub": j.Addr,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(k.path("room.json"), raw, 0o600)
}

// Saved is everything a room wrote down when it joined.
type Saved struct {
	Transport string
	Room      string
	Hub       string
	Service   string
	Share     string
	Identity  string
}

// Joined reads back what a room saved, so `atrium2 room` needs no arguments.
func (k Keys) Joined() (Saved, error) {
	var s Saved
	raw, err := os.ReadFile(k.path("room.json"))
	if err != nil {
		return s, errors.New(
			"this machine has not joined a hub yet. run `atrium2 join <join string>`")
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		return s, err
	}
	s = Saved{
		Transport: m["transport"], Room: m["room"], Hub: m["hub"],
		Service: m["service"], Share: m["share"], Identity: m["identity"],
	}
	if s.Transport == "" {
		// Written before there was more than one.
		s.Transport = "direct"
	}
	return s, nil
}

// ── file helpers ─────────────────────────────────────────

func (k Keys) writeCert(name string, der []byte) error {
	return os.WriteFile(k.path(name),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
}

func (k Keys) writeKey(name string, key *ecdsa.PrivateKey) error {
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	// 0600 on a private key. Windows ignores the mode and inherits the
	// directory's ACL, which is why the directory is made 0700 above and why
	// the state directory is per user.
	return os.WriteFile(k.path(name),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}), 0o600)
}

func parseCert(raw []byte) (*x509.Certificate, error) {
	blk, _ := pem.Decode(raw)
	if blk == nil {
		return nil, errors.New("not a certificate")
	}
	return x509.ParseCertificate(blk.Bytes)
}

func serial() *big.Int {
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return big.NewInt(time.Now().UnixNano())
	}
	return n
}

// Hosts is every name a hub should be reachable by, for its certificate.
//
// Loopback always, plus the machine's own name, so the same hub works for a
// room on this box and a room on the network without anybody choosing.
func Hosts(extra ...string) []string {
	out := []string{"127.0.0.1", "::1", "localhost"}
	if h, err := os.Hostname(); err == nil && h != "" {
		out = append(out, h)
	}
	for _, e := range extra {
		if e = strings.TrimSpace(e); e != "" {
			out = append(out, e)
		}
	}
	return out
}

func describeCert(c *x509.Certificate) string {
	if c == nil {
		return ""
	}
	return fmt.Sprintf("%s (until %s)", c.Subject.CommonName, c.NotAfter.Format("2006-01-02"))
}
