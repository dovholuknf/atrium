//go:build integration

package link

import (
	"bufio"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

// encodeToken builds a join string by hand, for the shapes nothing can mint any
// more.
func encodeToken(t *testing.T, tok token) string {
	t.Helper()
	body, err := json.Marshal(tok)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(body)
}

func hubKeys(t *testing.T) Keys {
	t.Helper()
	k := Keys{Dir: t.TempDir()}
	if err := k.EnsureCA(Hosts()); err != nil {
		t.Fatalf("could not set up a hub: %v", err)
	}
	return k
}

// A join string says which room it is for, and one that does not is refused
// rather than quietly enrolling something the hub has no row for.
func TestAJoinStringCarriesTheNameTheHubChose(t *testing.T) {
	k := hubKeys(t)

	line, err := k.MintToken("127.0.0.1:7801", "sparta", "a-secret")
	if err != nil {
		t.Fatal(err)
	}
	j, err := ParseToken(line)
	if err != nil {
		t.Fatal(err)
	}
	if j.Name != "sparta" {
		t.Fatalf("the join string named %q", j.Name)
	}
	if j.Secret != "a-secret" {
		t.Fatal("the secret did not survive the round trip")
	}

	if _, err := k.MintToken("127.0.0.1:7801", "", "a-secret"); err == nil {
		t.Fatal("a join string was minted for no room at all")
	}
}

// A string minted before the hub named its rooms has to be refused with a
// sentence, not accepted as an anonymous join.
func TestANamelessJoinStringIsRefused(t *testing.T) {
	// Built by hand, because nothing can mint one any more.
	old := tokenPrefix + encodeToken(t, token{T: "direct", A: "127.0.0.1:7801",
		F: "fingerprint", S: "secret"})
	_, err := ParseToken(old)
	if err == nil {
		t.Fatal("a join string naming no room was accepted")
	}
	if !strings.Contains(err.Error(), "which room") {
		t.Fatalf("the refusal does not say what is missing: %v", err)
	}
}

// THE CERTIFICATE CARRIES THE HUB'S NAME AND NOT THE ROOM'S REQUEST.
//
// This is the hole, at the one line where it lived. A signing request is a
// document the room wrote about itself, and the common name in it used to be
// taken when the hub had nothing better. Anything taken from there is the hub
// vouching for a claim it never checked.
func TestTheSignedNameIsTheHubsAndNotTheOneAskedFor(t *testing.T) {
	k := hubKeys(t)

	_, csr, err := NewCSR("athens")
	if err != nil {
		t.Fatal(err)
	}
	der, err := k.Sign(csr, "sparta")
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	if cert.Subject.CommonName != "sparta" {
		t.Fatalf("the hub signed a certificate for %q", cert.Subject.CommonName)
	}

	// AND WITH NO NAME THERE IS NOTHING TO SIGN. Falling back to the request is
	// what made a room able to name itself.
	if _, err := k.Sign(csr, ""); err == nil {
		t.Fatal("the hub signed a certificate using the name in the request")
	}
}

// Enrolment answers the room with a certificate for the name the SECRET was
// bound to, whatever the room put in its request.
func TestEnrolmentNamesTheRoomFromItsSecret(t *testing.T) {
	k := hubKeys(t)
	d := Direct{Keys: k, Spend: func(secret string) (string, error) {
		if secret != "the-one-for-sparta" {
			return "", errors.New("that join string has been used already, or it expired")
		}
		return "sparta", nil
	}}

	hub, room := net.Pipe()
	defer hub.Close()
	defer room.Close()

	done := make(chan string, 1)
	go func() {
		name, err := d.ServeEnrolment(hub, bufio.NewReader(hub))
		if err != nil {
			done <- "!" + err.Error()
			return
		}
		done <- name
	}()

	_, csr, err := NewCSR("i-am-athens")
	if err != nil {
		t.Fatal(err)
	}
	_ = room.SetDeadline(time.Now().Add(10 * time.Second))
	if err := writeJSON(room, enrolReq{
		Secret: "the-one-for-sparta", Self: "i-am-athens", CSR: csr,
	}); err != nil {
		t.Fatal(err)
	}
	var resp enrolResp
	if err := readJSON(bufio.NewReader(room), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.OK {
		t.Fatalf("enrolment was refused: %s", resp.Error)
	}
	cert, err := x509.ParseCertificate(resp.Cert)
	if err != nil {
		t.Fatal(err)
	}
	if cert.Subject.CommonName != "sparta" {
		t.Fatalf("a room that asked to be athens was enrolled as %q",
			cert.Subject.CommonName)
	}
	if got := <-done; got != "sparta" {
		t.Fatalf("the hub logged the join as %q", got)
	}
}

// A hub with nothing to spend a secret against enrols nobody. That is the
// honest answer for a transport whose identity comes from elsewhere, and it is
// what a hub whose store has halted has to do rather than signing freely.
func TestAHubThatCannotSpendASecretEnrolsNobody(t *testing.T) {
	d := Direct{Keys: hubKeys(t)}

	hub, room := net.Pipe()
	defer hub.Close()
	defer room.Close()

	go func() { _, _ = d.ServeEnrolment(hub, bufio.NewReader(hub)) }()

	_, csr, _ := NewCSR("anyone")
	_ = room.SetDeadline(time.Now().Add(10 * time.Second))
	if err := writeJSON(room, enrolReq{Secret: "anything", CSR: csr}); err != nil {
		t.Fatal(err)
	}
	var resp enrolResp
	if err := readJSON(bufio.NewReader(room), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.OK {
		t.Fatal("a hub with no way to check a secret signed a certificate anyway")
	}
}

// A refused secret never reaches the signer.
func TestASpentSecretEnrolsNothing(t *testing.T) {
	d := Direct{Keys: hubKeys(t), Spend: func(string) (string, error) {
		return "", errors.New("that join string has been used already, or it expired")
	}}

	hub, room := net.Pipe()
	defer hub.Close()
	defer room.Close()

	go func() { _, _ = d.ServeEnrolment(hub, bufio.NewReader(hub)) }()

	_, csr, _ := NewCSR("sparta")
	_ = room.SetDeadline(time.Now().Add(10 * time.Second))
	if err := writeJSON(room, enrolReq{Secret: "already-used", CSR: csr}); err != nil {
		t.Fatal(err)
	}
	var resp enrolResp
	if err := readJSON(bufio.NewReader(room), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.OK {
		t.Fatal("a spent join string still produced a certificate")
	}
	if !strings.Contains(resp.Error, "used already") {
		t.Fatalf("the room was told %q", resp.Error)
	}
}
