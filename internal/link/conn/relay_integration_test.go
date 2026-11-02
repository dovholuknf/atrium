//go:build integration

package conn

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/link/wire"
)

// VERSION SKEW. A hub older than the relay refuses the kind with the sentence
// every hub has sent since the kind switch, and the room reads that as
// ErrRelayOld, which is not held. The fake answers exactly as an old hub.
func TestAnOldHubIsToldApartAndNotHeldFor(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		br := bufio.NewReader(c)
		var h hello
		_ = readJSON(br, &h)
		_ = writeJSON(c, welcome{OK: false, Error: "a connection is control, data, enrol, upgrade or announce"})
	}()
	r := &Room{Name: "m1mini", Dial: plain{addr: ln.Addr().String()}, T: Timings{}.Fill()}
	r.up = true
	_, err = r.Relay(context.Background(), wire.RelayRequest{Op: RelaySay})
	if !errors.Is(err, ErrRelayOld) {
		t.Fatalf("err = %v, want ErrRelayOld", err)
	}
}

// A new hub knows the kind: the unknown-kind refusal must not be what a new
// hub says, or every room would think its hub was old.
func TestTheRelayKindIsKnown(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	go func() { _ = writeJSON(a, hello{V: Version, Kind: relayKind, Room: "x"}) }()
	if _, err := hearHello(b, bufio.NewReader(b)); err != nil {
		t.Fatalf("hearHello refused the relay kind: %v", err)
	}
}

// A new hub's refusal of a kind it does not know names relay among the ones it
// does, so a room never reads a new hub as older than cross-room say.
func TestANewHubsRefusalNamesTheRelay(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	go func() { _ = writeJSON(a, hello{V: Version, Kind: "teleport", Room: "x"}) }()
	var w welcome
	done := make(chan struct{})
	go func() { _ = readJSON(bufio.NewReader(a), &w); close(done) }()
	if _, err := hearHello(b, bufio.NewReader(b)); err == nil {
		t.Fatal("an unknown kind was accepted")
	}
	<-done
	if !strings.Contains(w.Error, "a connection is control") || !strings.Contains(w.Error, relayKind) {
		t.Fatalf("refusal = %q, want the kinds listed with relay among them", w.Error)
	}
}
