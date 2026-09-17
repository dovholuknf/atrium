package link

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"
)

// The direct transport: mutual TLS over TCP, and one port for everything.
//
// ── one port, three kinds of connection ──────────────────
//
// A room that has never joined has no certificate, so it cannot open a mutually
// authenticated connection, so enrolment cannot require one. The obvious
// answer is a second port for joining, and the obvious answer is wrong: a
// second port is a second thing to open in a firewall, a second thing to put in
// the join string, and a second thing to explain.
//
// Instead the listener asks for a client certificate but does not insist
// (`VerifyClientCertIfGiven`), and the FIRST FRAME decides:
//
//	enrol    no certificate. Proves itself with the one-time secret from the
//	         join string, and leaves with a certificate.
//	control  certificate required. The long-lived link.
//	data     certificate required. One HTTP connection for the hub to borrow.
//
// A connection that asks for control or data without a certificate is refused
// before anything reads a byte of its body.
//
// ── what the room pins ───────────────────────────────────
//
// The join string carries the SHA-256 of the hub's authority certificate. The
// room refuses any chain not issued by exactly that authority, which is
// certificate pinning and is stronger than the public web's answer, because
// there is no list of hundreds of authorities any of which would do.

// Direct is the transport both sides use. The hub listens with it, the room
// dials with it, and the fields each side uses are disjoint.
type Direct struct {
	// Addr is host:port. What the hub binds, and what a room dials.
	Addr string
	// Keys is where this side's certificates live.
	Keys Keys
	// Pin is the hub authority's fingerprint, on the room's side only. Empty on
	// a room that has already enrolled, because it then has the CA itself.
	Pin string
	// Spend consumes a join secret and answers the name the hub minted it for.
	// The hub's side only.
	//
	// A FUNCTION RATHER THAN A STORE, because this package must not learn that
	// the hub has a database. What a credential IS belongs to the transport,
	// and what a room is CALLED belongs to the hub, and this is the one line
	// where the two meet. Nil refuses enrolment, which is what a test over a
	// pipe and a hub with no store both want.
	Spend func(secret string) (string, error)
}

// ── the hub's side ───────────────────────────────────────

// Listen brings up the hub's port.
func (d Direct) Listen() (net.Listener, error) {
	cert, err := tls.LoadX509KeyPair(d.Keys.path("hub.crt"), d.Keys.path("hub.key"))
	if err != nil {
		return nil, fmt.Errorf("this hub has no certificate yet: %w", err)
	}
	ca, _, err := d.Keys.CA()
	if err != nil {
		return nil, err
	}
	// THE AUTHORITY IS SENT WITH THE LEAF, and leaving it out is the bug this
	// line exists to prevent.
	//
	// A joining room has only a FINGERPRINT of the authority, out of the join
	// string. A fingerprint cannot verify a signature, so the room can only
	// recognise the authority if the authority is actually in the chain. A hub
	// that sends its leaf alone gives the room nothing to match, and the
	// failure reads as "that is not the hub the join string came from", which
	// is the most alarming possible wording for a configuration mistake.
	cert.Certificate = append(cert.Certificate, ca.Raw)

	pool := x509.NewCertPool()
	pool.AddCert(ca)

	ln, err := net.Listen("tcp", d.Addr)
	if err != nil {
		return nil, err
	}
	return tls.NewListener(ln, &tls.Config{
		Certificates: []tls.Certificate{cert},
		// ASKED FOR, NOT REQUIRED, so enrolment can happen on this port. The
		// hello handler refuses control and data without one.
		ClientAuth: tls.VerifyClientCertIfGiven,
		ClientCAs:  pool,
		MinVersion: tls.VersionTLS13,
	}), nil
}

// identify reads the room name out of the client certificate.
//
// Wired into the package variable in `hub.go`, so the hub's handshake code
// never imports TLS and a transport with no certificates (a test over a pipe)
// simply leaves the claim alone.
func init() {
	identify = func(c net.Conn) string {
		if cert := peerCert(c); cert != nil {
			return cert.Subject.CommonName
		}
		return ""
	}
	// The PUBLIC KEY, not the serial or the fingerprint of the whole
	// certificate. A room that is reissued a certificate keeps its key, so
	// this stays stable across a renewal while still being something only the
	// holder of that key can present.
	peerKey = func(c net.Conn) string {
		cert := peerCert(c)
		if cert == nil {
			return ""
		}
		sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
		return base64.RawURLEncoding.EncodeToString(sum[:])
	}
}

func peerCert(c net.Conn) *x509.Certificate {
	tc, ok := c.(*tls.Conn)
	if !ok {
		return nil
	}
	st := tc.ConnectionState()
	if len(st.PeerCertificates) == 0 {
		return nil
	}
	return st.PeerCertificates[0]
}

// DirectAuthenticated reports whether a connection presented a client
// certificate. Wired into `Hub.Authenticated` by whoever builds the hub.
func DirectAuthenticated(c net.Conn) bool {
	tc, ok := c.(*tls.Conn)
	if !ok {
		return false
	}
	return len(tc.ConnectionState().PeerCertificates) > 0
}

// ServeEnrolment answers a joining room on an already-accepted connection.
//
// Called by the hub when the first frame says `enrol`. Returns the room's name
// on success, for the log.
func (d Direct) ServeEnrolment(conn net.Conn, br *bufio.Reader) (string, error) {
	if err := conn.SetDeadline(time.Now().Add(handshakeWait)); err != nil {
		return "", err
	}
	var req enrolReq
	if err := readJSON(br, &req); err != nil {
		return "", err
	}
	if d.Spend == nil {
		err := errors.New("this hub is not set up to enrol rooms")
		_ = writeJSON(conn, enrolResp{Error: err.Error()})
		return "", err
	}
	// THE SECRET SAYS WHICH ROOM THIS IS. Not the frame, not the request, and
	// not the certificate request's common name: all three are things the
	// caller wrote about itself. Spending the secret is both the proof and the
	// answer, in one step, which is what leaves nothing to claim.
	name, err := d.Spend(req.Secret)
	if err != nil {
		_ = writeJSON(conn, enrolResp{Error: err.Error()})
		return "", err
	}
	if self := strings.TrimSpace(req.Self); self != "" && !equalFold(self, name) {
		// Observed beside the override, and the log is where it first shows up.
		log.Printf("[hub] the machine joining as %q calls itself %q", name, self)
	}
	certDER, err := d.Keys.sign(req.CSR, name)
	if err != nil {
		_ = writeJSON(conn, enrolResp{Error: err.Error()})
		return "", err
	}
	ca, _, err := d.Keys.CA()
	if err != nil {
		_ = writeJSON(conn, enrolResp{Error: err.Error()})
		return "", err
	}
	if err := writeJSON(conn, enrolResp{OK: true, Cert: certDER, CA: ca.Raw}); err != nil {
		return "", err
	}
	return name, nil
}

// ── the room's side ──────────────────────────────────────

// Dial opens one connection to the hub.
func (d Direct) Dial(ctx context.Context) (net.Conn, error) {
	cfg, err := d.clientTLS()
	if err != nil {
		return nil, err
	}
	dialer := &tls.Dialer{NetDialer: &net.Dialer{}, Config: cfg}
	conn, err := dialer.DialContext(ctx, "tcp", d.Addr)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// Describe is what to log. The address, and nothing secret.
func (d Direct) Describe() string { return d.Addr }

// clientTLS builds the room's side, with or without a certificate.
func (d Direct) clientTLS() (*tls.Config, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS13}

	// A room that has enrolled trusts the CA it was given. One that has not
	// trusts the fingerprint in the join string and nothing else.
	if ca, err := os.ReadFile(d.Keys.path("ca.crt")); err == nil {
		cert, err := parseCert(ca)
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		pool.AddCert(cert)
		cfg.RootCAs = pool
		// THE NAME IS NOT CHECKED AND THE ISSUER IS.
		//
		// A hub is reached at whatever address the operator typed: an IP, a
		// hostname, a name that resolves differently from two networks. Insisting
		// the certificate match that string would make the setup fail for
		// reasons that have nothing to do with security. What matters is that
		// this is the same hub that issued this room's own certificate, and the
		// verifier below is exactly that question.
		cfg.InsecureSkipVerify = true
		cfg.VerifyPeerCertificate = issuedBy(pool)
	} else if d.Pin != "" {
		cfg.InsecureSkipVerify = true
		cfg.VerifyPeerCertificate = pinnedTo(d.Pin)
	} else {
		return nil, errors.New(
			"this room has neither a hub certificate nor a join string to pin one with")
	}

	if cert, err := tls.LoadX509KeyPair(
		d.Keys.path("room.crt"), d.Keys.path("room.key")); err == nil {
		cfg.Certificates = []tls.Certificate{cert}
	}
	return cfg, nil
}

// issuedBy verifies a chain against a pool, ignoring the name.
func issuedBy(pool *x509.CertPool) func([][]byte, [][]*x509.Certificate) error {
	return func(raw [][]byte, _ [][]*x509.Certificate) error {
		if len(raw) == 0 {
			return errors.New("the hub presented no certificate")
		}
		leaf, err := x509.ParseCertificate(raw[0])
		if err != nil {
			return err
		}
		inter := x509.NewCertPool()
		for _, r := range raw[1:] {
			if c, err := x509.ParseCertificate(r); err == nil {
				inter.AddCert(c)
			}
		}
		_, err = leaf.Verify(x509.VerifyOptions{
			Roots: pool, Intermediates: inter,
			KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		})
		if err != nil {
			return fmt.Errorf("that is not the hub this room joined: %w", err)
		}
		return nil
	}
}

// pinnedTo verifies the chain against the fingerprint from the join string.
//
// Used exactly once, during enrolment, before any authority is on disk.
//
// TWO STEPS, AND BOTH ARE NEEDED. Finding a certificate whose hash matches the
// pin proves the authority is present. It does not prove the server holds a key
// that authority signed, and a server that simply attached the real authority
// to its own self-signed leaf would pass step one. So the matched certificate
// becomes the root and the leaf is verified against it.
func pinnedTo(want string) func([][]byte, [][]*x509.Certificate) error {
	return func(raw [][]byte, _ [][]*x509.Certificate) error {
		if len(raw) == 0 {
			return errors.New("the hub presented no certificate")
		}
		var pinned *x509.Certificate
		for _, r := range raw {
			sum := sha256.Sum256(r)
			if base64.RawURLEncoding.EncodeToString(sum[:]) != want {
				continue
			}
			c, err := x509.ParseCertificate(r)
			if err != nil {
				return err
			}
			pinned = c
			break
		}
		if pinned == nil {
			return errors.New(
				"the hub at that address is not the one the join string came from. " +
					"either the string is stale or something else is answering in its place")
		}
		leaf, err := x509.ParseCertificate(raw[0])
		if err != nil {
			return err
		}
		pool := x509.NewCertPool()
		pool.AddCert(pinned)
		if _, err := leaf.Verify(x509.VerifyOptions{
			Roots:     pool,
			KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		}); err != nil {
			return fmt.Errorf(
				"that hub attached the right authority but does not hold a key it signed: %w", err)
		}
		return nil
	}
}

// Enrol runs the join: dial pinned, send a request, save what comes back.
//
// `self` is what this machine calls itself. It is sent and it decides nothing:
// THE NAME COMES BACK IN THE CERTIFICATE. The room is called what the hub
// signed, which is the name the hub minted into the token, and the returned
// string is that name so the caller can say it out loud.
func (d Direct) Enrol(ctx context.Context, self, secret string) (string, error) {
	// The common name here is ignored by the hub, which signs the name it
	// already has. Sent anyway rather than left empty, because a signing
	// request with no subject is a thing several tools refuse to parse.
	key, csr, err := NewCSR(self)
	if err != nil {
		return "", err
	}
	cfg := &tls.Config{
		MinVersion:            tls.VersionTLS13,
		InsecureSkipVerify:    true,
		VerifyPeerCertificate: pinnedTo(d.Pin),
	}
	dialer := &tls.Dialer{NetDialer: &net.Dialer{}, Config: cfg}
	conn, err := dialer.DialContext(ctx, "tcp", d.Addr)
	if err != nil {
		return "", fmt.Errorf("could not reach the hub at %s: %w", d.Addr, err)
	}
	defer conn.Close()

	br := bufio.NewReader(conn)
	if err := conn.SetDeadline(time.Now().Add(handshakeWait)); err != nil {
		return "", err
	}
	if err := writeJSON(conn, hello{V: Version, Kind: "enrol", Room: self}); err != nil {
		return "", err
	}
	var w welcome
	if err := readJSON(br, &w); err != nil {
		return "", err
	}
	if !w.OK {
		return "", errors.New(w.Error)
	}
	if err := writeJSON(conn, enrolReq{Secret: secret, Self: self, CSR: csr}); err != nil {
		return "", err
	}
	var resp enrolResp
	if err := readJSON(br, &resp); err != nil {
		return "", err
	}
	if !resp.OK {
		return "", errors.New(resp.Error)
	}

	// WHAT THIS ROOM IS CALLED IS READ BACK OUT OF THE CERTIFICATE.
	//
	// Not taken from the token, and not from what was asked for. The
	// certificate is what every later connection presents and what the hub
	// reads to decide where a request goes, so it is the only answer that
	// cannot disagree with the hub. A token whose name somebody edited by hand
	// changes nothing here: the signed name wins.
	c, err := x509.ParseCertificate(resp.Cert)
	if err != nil {
		return "", fmt.Errorf("the hub sent a certificate this room cannot read: %w", err)
	}
	name := strings.TrimSpace(c.Subject.CommonName)
	if name == "" {
		return "", errors.New("the hub signed a certificate with no name in it")
	}
	if err := d.Keys.SaveRoom(key, resp.Cert, resp.CA, d.Addr, name); err != nil {
		return "", err
	}
	log.Printf("[join] enrolled as %s", describeCert(c))
	return name, nil
}
