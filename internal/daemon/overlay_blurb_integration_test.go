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
