package main

import (
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/link"
)

// The board has no login, so it must never bind anywhere a room, or anyone
// else, can reach it. These are the guard that makes that an enforced invariant
// rather than an assumed one.
func TestTheBoardRefusesANonLoopbackAddr(t *testing.T) {
	for _, addr := range []string{
		"0.0.0.0:7800",      // every interface, said out loud
		"192.168.1.10:7800", // a LAN address
		"10.0.0.5:7800",
		"[::]:7800", // every interface, v6
	} {
		if got, err := loopbackBoard(addr); err == nil {
			t.Errorf("loopbackBoard(%q) = %q, want a refusal: the board has no login", addr, got)
		}
	}
}

// The default and the loopback shapes must keep working, and an empty host has
// to be pinned to loopback rather than left binding every interface.
func TestTheBoardKeepsLoopback(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{":7800", "127.0.0.1:7800"},          // the default, coerced to loopback
		{"127.0.0.1:7800", "127.0.0.1:7800"}, // already loopback
		{"[::1]:7800", "[::1]:7800"},         // v6 loopback
		{"localhost:7800", "localhost:7800"}, // the name for this machine
	} {
		got, err := loopbackBoard(c.in)
		if err != nil {
			t.Errorf("loopbackBoard(%q) errored: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("loopbackBoard(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The room link is the surface that MAY bind wide, because it is authenticated
// by the transport. A wide bind is safe; it just needs an address a remote room
// can dial, which is exactly what --link-advertise supplies.
func TestTheLinkAcceptsAWideBindWithAdvertise(t *testing.T) {
	got, err := advertiseFor("0.0.0.0:7801", "10.1.2.3:7801")
	if err != nil {
		t.Fatalf("advertiseFor wide bind with advertise errored: %v", err)
	}
	if got != "10.1.2.3:7801" {
		t.Errorf("advertiseFor = %q, want the advertise address 10.1.2.3:7801", got)
	}
}

// A wide bind with no advertise is the silent failure this exists to stop: a
// loopback token that looks right and cannot be dialled by any remote room. It
// must fail fast at mint time instead.
func TestAWideLinkWithoutAdvertiseIsRefused(t *testing.T) {
	for _, bind := range []string{"0.0.0.0:7801", "[::]:7801"} {
		if got, err := advertiseFor(bind, ""); err == nil {
			t.Errorf("advertiseFor(%q, \"\") = %q, want a refusal asking for --link-advertise", bind, got)
		}
	}
}

// The loopback default still derives its own address, so the two-accounts case
// needs no flag.
func TestTheLinkDerivesLoopbackWithoutAdvertise(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{":7801", "127.0.0.1:7801"},                // the default
		{"127.0.0.1:7801", "127.0.0.1:7801"},       // loopback stays itself
		{"192.168.1.10:7801", "192.168.1.10:7801"}, // a specific interface is dialable as-is
	} {
		got, err := advertiseFor(c.in, "")
		if err != nil {
			t.Errorf("advertiseFor(%q, \"\") errored: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("advertiseFor(%q, \"\") = %q, want %q", c.in, got, c.want)
		}
	}
}

// The advertise address is what a room actually dials, so it must be the thing
// minted into the join string. This is the end to end of --link-advertise: a
// wide bind, an advertise host, and a token a remote room can read the address
// back out of.
func TestTheMintedTokenCarriesTheAdvertiseAddress(t *testing.T) {
	keys := link.Keys{Dir: t.TempDir()}
	if err := keys.EnsureCA(link.Hosts("10.1.2.3")); err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}
	adv, err := advertiseFor("0.0.0.0:7801", "10.1.2.3:7801")
	if err != nil {
		t.Fatalf("advertiseFor: %v", err)
	}
	line, err := keys.MintToken(adv, "sgg", "a-secret")
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	j, err := link.ParseToken(line)
	if err != nil {
		t.Fatalf("ParseToken: %v", err)
	}
	if j.Addr != "10.1.2.3:7801" {
		t.Errorf("token carries addr %q, want the advertise address 10.1.2.3:7801", j.Addr)
	}
	if j.Name != "sgg" {
		t.Errorf("token carries name %q, want sgg", j.Name)
	}
}

// An explicit --link-advertise wins over any bind, wide or loopback, so an
// operator can always say exactly what a room dials.
func TestAdvertiseOverrideAlwaysWins(t *testing.T) {
	for _, bind := range []string{":7801", "127.0.0.1:7801", "0.0.0.0:7801"} {
		got, err := advertiseFor(bind, "hub.example:9999")
		if err != nil {
			t.Errorf("advertiseFor(%q, override) errored: %v", bind, err)
			continue
		}
		if got != "hub.example:9999" {
			t.Errorf("advertiseFor(%q, override) = %q, want the override", bind, got)
		}
	}
}

// A refusal has to say what to do about it, not just that something is wrong.
func TestTheWideBindRefusalNamesTheFlag(t *testing.T) {
	_, err := advertiseFor("0.0.0.0:7801", "")
	if err == nil {
		t.Fatal("expected a refusal")
	}
	if !strings.Contains(err.Error(), "--link-advertise") {
		t.Errorf("refusal %q does not name --link-advertise", err.Error())
	}
}
