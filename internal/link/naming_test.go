package link

import (
	"bufio"
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// WHO DECIDES WHAT A ROOM IS CALLED.
//
// The hub does, and the name travels in the join token. Everything here is one
// way that was not true before: a room named itself at enrolment and the hub
// signed whatever it asked for, so one secret authorised any name. These tests
// are named for the ways that comes back.

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
	der, err := k.sign(csr, "sparta")
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
	if _, err := k.sign(csr, ""); err == nil {
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

// AND A ROOM WHOSE RECORD GOES WHILE IT IS ATTACHED IS LET GO.
//
// The hub's store is a file, and `atrium2 hub room rm --force` is another
// process writing to it. Checking only at attach would leave the hub proxying
// to a room it has no record of: not on the list, not nameable, and not
// something anything else in the design knows how to describe.
func TestARoomWhoseRecordGoesIsLetGo(t *testing.T) {
	h := NewHub(Timings{Beat: 20 * time.Millisecond, Silence: time.Hour})

	var gone bool
	var mu sync.Mutex
	h.Attaching = func(string, string, string) error {
		mu.Lock()
		defer mu.Unlock()
		if gone {
			return errors.New("this hub has no room called sparta")
		}
		return nil
	}

	hub, room := net.Pipe()
	defer room.Close()
	// The room's side has to be read continuously or the pipe blocks the hub.
	go func() {
		buf := make([]byte, 256)
		for {
			if _, err := room.Read(buf); err != nil {
				return
			}
		}
	}()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go h.control(ctx, "sparta", hello{}, hub, bufio.NewReader(hub))

	waitFor(t, 5*time.Second, func() bool { return h.Has("sparta") })

	mu.Lock()
	gone = true
	mu.Unlock()

	waitFor(t, 5*time.Second, func() bool { return !h.Has("sparta") })
}

// A ROOM THE HUB HAS NO RECORD OF DOES NOT ATTACH, even holding papers this hub
// signed. A room that was forced out keeps its certificate, and a hub that let
// it back in would be proxying to something it cannot name, settle, or mark for
// deletion.
func TestARoomWithNoRecordIsTurnedAway(t *testing.T) {
	h := NewHub(Timings{})
	h.Attaching = func(name, _, _ string) error {
		if name == "sparta" {
			return nil
		}
		return errors.New("this hub has no room called " + name)
	}

	hub, room := net.Pipe()
	defer hub.Close()
	defer room.Close()

	go h.control(t.Context(), "athens", hello{}, hub, bufio.NewReader(hub))

	_ = room.SetDeadline(time.Now().Add(10 * time.Second))
	var w welcome
	if err := readJSON(bufio.NewReader(room), &w); err != nil {
		t.Fatal(err)
	}
	if w.OK {
		t.Fatal("a room the hub has no record of was adopted")
	}
	if !strings.Contains(w.Error, "no room called athens") {
		t.Fatalf("the room was told %q", w.Error)
	}
	if h.Has("athens") {
		t.Fatal("the refused room is in the hub's list anyway")
	}
}
