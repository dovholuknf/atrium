package main

import (
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/link"
)

// A full direct token, the shape ParseToken hands back for `--transport direct`.
func directToken() link.Join {
	return link.Join{Transport: "direct", Name: "room-a", Addr: "hub:7443", Pin: "fp", Secret: "s3cret"}
}

func TestResolveNoFlagsKeepsToken(t *testing.T) {
	j := directToken()
	out, ident, jwt, err := joinFlags{}.resolve(j)
	if err != nil {
		t.Fatalf("no flags should not error: %v", err)
	}
	if out.Transport != "direct" || out.Addr != "hub:7443" || jwt != "" || ident != "" {
		t.Fatalf("token should pass through unchanged, got %+v ident=%q jwt=%q", out, ident, jwt)
	}
}

func TestResolveRefusesTwoTransports(t *testing.T) {
	_, _, _, err := joinFlags{mtls: "hub:7443", zrokPrivate: "tok"}.resolve(directToken())
	if err == nil || !strings.Contains(err.Error(), "give one transport") {
		t.Fatalf("two transport flags should be refused, got %v", err)
	}
}

func TestResolveMtlsOverridesAddress(t *testing.T) {
	out, _, _, err := joinFlags{mtls: "https://elsewhere:9000/"}.resolve(directToken())
	if err != nil {
		t.Fatalf("mtls with a full direct token should resolve: %v", err)
	}
	if out.Transport != "direct" {
		t.Fatalf("mtls is the direct transport, got %q", out.Transport)
	}
	if out.Addr != "elsewhere:9000" {
		t.Fatalf("--mtls should override the address and strip the scheme, got %q", out.Addr)
	}
	if out.Pin != "fp" || out.Secret != "s3cret" {
		t.Fatalf("--mtls must keep the token's pin and secret, got %+v", out)
	}
}

func TestResolveMtlsNeedsDirectAnchor(t *testing.T) {
	// A token with a name but no secret or pin is not a direct join token, and no
	// URL can supply what the hub has to have signed.
	_, _, _, err := joinFlags{mtls: "hub:7443"}.resolve(link.Join{Transport: "direct", Name: "r"})
	if err == nil || !strings.Contains(err.Error(), "direct join token") {
		t.Fatalf("mtls without secret/pin should be refused, got %v", err)
	}
}

func TestResolveMtlsConflictsWithOverlayToken(t *testing.T) {
	_, _, _, err := joinFlags{mtls: "hub:7443"}.resolve(link.Join{Transport: "zrok", Name: "r", ShareToken: "k"})
	if err == nil || !strings.Contains(err.Error(), "one transport") {
		t.Fatalf("mtls against a zrok token should conflict, got %v", err)
	}
}

func TestResolveZrokPrivateSetsShareToken(t *testing.T) {
	// The token names the room; the flag supplies the share access token.
	out, _, _, err := joinFlags{zrokPrivate: "share-abc"}.resolve(link.Join{Transport: "zrok", Name: "r", ShareToken: "old"})
	if err != nil {
		t.Fatalf("zrok-private should resolve: %v", err)
	}
	if out.Transport != "zrok" || out.ShareToken != "share-abc" {
		t.Fatalf("zrok-private should set the share token, got %+v", out)
	}
}

func TestResolveOpenzitiIdentityFile(t *testing.T) {
	out, ident, jwt, err := joinFlags{openziti: "C:/ids/room.json", service: "boards"}.
		resolve(link.Join{Transport: "ziti", Name: "r", Service: "atrium"})
	if err != nil {
		t.Fatalf("openziti with a .json should resolve: %v", err)
	}
	if out.Transport != "ziti" || out.Service != "boards" {
		t.Fatalf("--service should win over the token, got %+v", out)
	}
	if ident != "C:/ids/room.json" || jwt != "" {
		t.Fatalf("a .json is an identity, not a jwt: ident=%q jwt=%q", ident, jwt)
	}
}

func TestResolveOpenzitiServiceDefault(t *testing.T) {
	out, _, _, err := joinFlags{openziti: "room.json"}.resolve(link.Join{Transport: "ziti", Name: "r"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if out.Service != "atrium" {
		t.Fatalf("service should default to atrium, got %q", out.Service)
	}
}

func TestResolveOpenzitiJwtIsFlaggedForEnrolment(t *testing.T) {
	_, ident, jwt, err := joinFlags{openziti: "eyJhbG0"}.resolve(link.Join{Transport: "ziti", Name: "r"})
	if err != nil {
		t.Fatalf("a jwt should resolve and be returned for enrolment: %v", err)
	}
	if jwt != "eyJhbG0" || ident != "" {
		t.Fatalf("a non-.json is a jwt to enrol: ident=%q jwt=%q", ident, jwt)
	}
}

func TestResolveOpenzitiConflictsWithDirectToken(t *testing.T) {
	_, _, _, err := joinFlags{openziti: "room.json"}.resolve(directToken())
	if err == nil || !strings.Contains(err.Error(), "one transport") {
		t.Fatalf("openziti against a direct token should conflict, got %v", err)
	}
}

func TestHostPortStripsScheme(t *testing.T) {
	for in, want := range map[string]string{
		"hub:7443":         "hub:7443",
		"https://hub:7443": "hub:7443",
		"tcp://1.2.3.4:9/": "1.2.3.4:9",
		"  spaced:1  ":     "spaced:1",
		"":                 "",
	} {
		if got := hostPort(in); got != want {
			t.Errorf("hostPort(%q) = %q, want %q", in, got, want)
		}
	}
}
