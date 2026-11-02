//go:build integration

package daemon

import (
	"github.com/dovholuknf/atrium/internal/daemon/publish"
	"strings"
	"testing"
)

func TestAPublicShareWithNoLoginSaysSo(t *testing.T) {
	t.Parallel()
	got := publish.WhoMayOpenIt(publish.AuthConfig{}, false)
	if !strings.Contains(got, "no login") {
		t.Fatalf("a public share with nothing in front of it should say so, said %q", got)
	}
}

func TestAGatedWayOutIsNotReportedAsOpen(t *testing.T) {
	t.Parallel()
	got := publish.WhoMayOpenIt(publish.AuthConfig{}, true)
	if strings.Contains(got, "This board has no login") {
		t.Fatalf("a private share and a ziti service already decide who gets in, said %q", got)
	}
	if !strings.Contains(got, "who may open it") {
		t.Fatalf("it should still point at the fields below it, said %q", got)
	}
}

func TestAPasswordIsReportedWithItsName(t *testing.T) {
	t.Parallel()
	c := publish.AuthConfig{Enabled: true, Basic: true, User: "clint",
		PassHash: "NOTTHEPASSWORD", PassSalt: "NOTTHESALT"}
	got := publish.WhoMayOpenIt(c, false)
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
	t.Parallel()
	got := publish.WhoMayOpenIt(publish.AuthConfig{Enabled: true}, false)
	if !strings.Contains(got, "nothing is configured") {
		t.Fatalf("an empty login is its own state, said %q", got)
	}
}

func TestAProviderIsNamed(t *testing.T) {
	t.Parallel()
	c := publish.AuthConfig{Enabled: true, Issuer: "https://keycloak.example/realms/x"}
	got := publish.WhoMayOpenIt(c, false)
	if !strings.Contains(got, c.Issuer) {
		t.Fatalf("somebody has to know where they are being sent, said %q", got)
	}
}
