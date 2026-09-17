package daemon

import (
	"strings"
	"testing"
)

// The sentence at the end of an overlay's blurb told every operator the board
// had no login, including the ones who had set one. This pins the four things
// it can say and, in particular, that a gated way out is not reported as a
// board sitting open.

func TestAPublicShareWithNoLoginSaysSo(t *testing.T) {
	got := whoMayOpenIt(AuthConfig{}, false)
	if !strings.Contains(got, "no login") {
		t.Fatalf("a public share with nothing in front of it should say so, said %q", got)
	}
}

func TestAGatedWayOutIsNotReportedAsOpen(t *testing.T) {
	got := whoMayOpenIt(AuthConfig{}, true)
	if strings.Contains(got, "This board has no login") {
		t.Fatalf("a private share and a ziti service already decide who gets in, said %q", got)
	}
	if !strings.Contains(got, "who may open it") {
		t.Fatalf("it should still point at the fields below it, said %q", got)
	}
}

func TestAPasswordIsReportedWithItsName(t *testing.T) {
	c := AuthConfig{Enabled: true, Basic: true, User: "clint",
		PassHash: "NOTTHEPASSWORD", PassSalt: "NOTTHESALT"}
	got := whoMayOpenIt(c, false)
	if !strings.Contains(got, `"clint"`) {
		t.Fatalf("the name is the half atrium can say, said %q", got)
	}
	// THE PASSWORD IS NOT IN THERE. It cannot be, since only a hash is
	// stored, and this fails loudly if that ever stops being true.
	if strings.Contains(got, "NOTTHE") {
		t.Fatalf("nothing about the stored secret belongs in a blurb, said %q", got)
	}
}

func TestALoginTurnedOnWithNothingBehindItSaysThat(t *testing.T) {
	got := whoMayOpenIt(AuthConfig{Enabled: true}, false)
	if !strings.Contains(got, "nothing is configured") {
		t.Fatalf("an empty login is its own state, said %q", got)
	}
}

func TestAProviderIsNamed(t *testing.T) {
	c := AuthConfig{Enabled: true, Issuer: "https://keycloak.example/realms/x"}
	got := whoMayOpenIt(c, false)
	if !strings.Contains(got, c.Issuer) {
		t.Fatalf("somebody has to know where they are being sent, said %q", got)
	}
}
