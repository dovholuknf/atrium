package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A SLASH IS NOT AN ADDRESS CHANGE.
//
// Confirmed by direct API calls: an environment enabled against
// "https://api-v2.zrok.io/" (with the trailing slash the CLI writes) refused a
// request naming "https://api-v2.zrok.io" (without one, which is what the
// binary's own compiled-in default reads back as). `SetZrokEnvironment`
// compared the two byte for byte and refused a change nobody asked for.
func TestSameZrokInstance(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"https://api-v2.zrok.io/", "https://api-v2.zrok.io", true}, // the confirmed bug
		{"https://api-v2.zrok.io", "https://api-v2.zrok.io", true},
		{"https://api-v2.zrok.io///", "https://api-v2.zrok.io", true},
		{"https://API-V2.zrok.io", "https://api-v2.zrok.io", true},      // host case
		{"https://api-v2.zrok.io:443", "https://api-v2.zrok.io", true},  // default https port
		{"http://api-v2.zrok.io:80", "http://api-v2.zrok.io", true},     // default http port
		{"  https://api-v2.zrok.io/  ", "https://api-v2.zrok.io", true}, // surrounding whitespace
		{"", "", true},
		{"https://api-v2.zrok.io", "https://api.zrok.io", false},         // a real move
		{"https://api-v2.zrok.io:8443", "https://api-v2.zrok.io", false}, // a real, non-default port
		{"https://api-v2.zrok.io/v2", "https://api-v2.zrok.io", false},   // a real path difference
		{"not a url with a host", "https://api-v2.zrok.io", false},
		{"", "https://api-v2.zrok.io", false},
	}
	for _, tc := range cases {
		if got := sameZrokInstance(tc.a, tc.b); got != tc.want {
			t.Errorf("sameZrokInstance(%q, %q) = %v, wanted %v", tc.a, tc.b, got, tc.want)
		}
		// Symmetric: which one is "current" and which is "requested" must not
		// change the answer.
		if got := sameZrokInstance(tc.b, tc.a); got != tc.want {
			t.Errorf("sameZrokInstance(%q, %q) = %v, wanted %v (reversed)", tc.b, tc.a, got, tc.want)
		}
	}
}

// enableMachineZrokEnv writes an environment.json that reads as enabled,
// against the same HOME `TestZrokEnvReadsAnEnabledEnvironment` uses, so
// `SetZrokEnvironment`'s "already enabled" branch is the one under test rather
// than the "nothing enabled yet" one.
func enableMachineZrokEnv(t *testing.T, apiEndpoint string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, ".zrok2")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// BOTH FILES, and metadata.json is the one that matters.
	//
	// `zroksdk.Load` stats `metadata.json` to decide whether a root exists at
	// all, and only then reads `environment.json`. `IsEnabled` is then just
	// "did an environment get read". A fixture that wrote only the
	// environment file produced a DEFAULT root with no environment on it, so
	// `IsEnabled` was false and `SetZrokEnvironment` returned before reaching
	// the comparison these tests exist to exercise.
	//
	// The version has to match the SDK's own or `loadMetadata` refuses it.
	meta := `{"v":"v0.4"}`
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(meta), 0o600); err != nil {
		t.Fatal(err)
	}
	body := `{"zrok_token":"tok","api_endpoint":"` + apiEndpoint + `","ziti_identity":"abc"}`
	if err := os.WriteFile(filepath.Join(dir, "environment.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// requireEnabled fails when the fixture did not actually enable anything.
//
// THE POINT OF THIS FUNCTION IS THAT TWO OF THE TESTS BELOW ONCE PASSED
// WITHOUT RUNNING ANY OF THE CODE THEY NAME. They assert that a call returns
// no error, and a `SetZrokEnvironment` that finds no enabled environment
// returns no error by a completely different path. Asserting the precondition
// is what makes the absence of an error mean what the test says it means.
func requireEnabled(t *testing.T, d *Daemon) {
	t.Helper()
	root, err := d.zrokRoot()
	if err != nil {
		t.Fatalf("the fixture's zrok root would not load: %v", err)
	}
	if !root.IsEnabled() {
		t.Fatal("the fixture did not enable an environment, so this test would " +
			"pass without reaching the code it is about")
	}
}

// THE REPRODUCTION, run against the real path: an environment enabled with a
// trailing slash must accept a request for the same instance spelled without
// one. Before the fix this returned the exact refusal quoted in the ticket.
func TestSetZrokEnvironmentIgnoresATrailingSlash(t *testing.T) {
	enableMachineZrokEnv(t, "https://api-v2.zrok.io/")
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	requireEnabled(t, d)

	if err := d.SetZrokEnvironment(false, "https://api-v2.zrok.io"); err != nil {
		t.Fatalf("switching to the same instance, spelled without the trailing slash, was refused: %v", err)
	}
}

// Also true the other way round.
func TestSetZrokEnvironmentIgnoresAMissingTrailingSlash(t *testing.T) {
	enableMachineZrokEnv(t, "https://api-v2.zrok.io")
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	requireEnabled(t, d)

	if err := d.SetZrokEnvironment(false, "https://api-v2.zrok.io/"); err != nil {
		t.Fatalf("switching to the same instance, spelled with a trailing slash, was refused: %v", err)
	}
}

// Normalising must not stop a refusal for an address that is actually
// different, and the message must name both addresses so a one character
// difference is visible rather than invisible.
func TestSetZrokEnvironmentStillRefusesARealMove(t *testing.T) {
	enableMachineZrokEnv(t, "https://api-v2.zrok.io/")
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	requireEnabled(t, d)

	err := d.SetZrokEnvironment(false, "https://api.zrok.io")
	if err == nil {
		t.Fatal("switching to a different instance while enabled was accepted")
	}
	if !strings.Contains(err.Error(), "https://api-v2.zrok.io/") {
		t.Fatalf("the refusal does not name what it is enabled against: %v", err)
	}
	if !strings.Contains(err.Error(), "https://api.zrok.io") {
		t.Fatalf("the refusal does not name what was asked for: %v", err)
	}
}

// An empty endpoint asks for nothing in particular ("keep using whatever this
// root already has"), and must never be refused as a move.
func TestSetZrokEnvironmentAcceptsAnEmptyEndpointWhileEnabled(t *testing.T) {
	enableMachineZrokEnv(t, "https://api-v2.zrok.io/")
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	requireEnabled(t, d)

	if err := d.SetZrokEnvironment(false, ""); err != nil {
		t.Fatalf("an empty endpoint against an enabled environment was refused: %v", err)
	}
}
