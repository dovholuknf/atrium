//go:build integration

package daemon

import (
	"github.com/dovholuknf/atrium/internal/daemon/publish"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func basicConfig(t *testing.T, user, pass string) publish.AuthConfig {
	t.Helper()
	c := publish.AuthConfig{Enabled: true, Basic: true, User: user}
	if err := publish.SetBasicPassword(&c, pass); err != nil {
		t.Fatal(err)
	}
	return c
}

// THE PASSWORD IS NEVER STORED. A database somebody takes has to be worth
// nothing without the guessing.
func TestThePasswordIsNotKept(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	c := basicConfig(t, "clint", "correct horse battery staple")
	if strings.Contains(c.PassHash, "correct") || c.PassHash == "correct horse battery staple" {
		t.Fatalf("the password is in the hash: %q", c.PassHash)
	}
	if c.PassSalt == "" {
		t.Fatal("no salt, so two boards with one password store one hash")
	}
	if !c.HasPassword() {
		t.Fatal("a password was set and the config says otherwise")
	}
}

// A FRESH SALT EVERY TIME, or a stolen table is worth precomputing against.
func TestTwoBoardsWithOnePasswordDoNotShareAHash(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	a := basicConfig(t, "clint", "the same password")
	b := basicConfig(t, "clint", "the same password")
	if a.PassSalt == b.PassSalt {
		t.Fatal("the salt is reused")
	}
	if a.PassHash == b.PassHash {
		t.Fatal("one password gives one hash, which is the thing a salt exists to stop")
	}
}

func TestTheRightPasswordIsAccepted(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	c := basicConfig(t, "clint", "hunter2")
	if !c.Checks("clint", "hunter2") {
		t.Fatal("the configured password was refused")
	}
}

func TestAWrongPasswordOrNameIsRefused(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	c := basicConfig(t, "clint", "hunter2")
	for _, try := range []struct{ user, pass string }{
		{"clint", "hunter3"},
		{"clint", ""},
		{"someone", "hunter2"},
		{"", "hunter2"},
		{"", ""},
	} {
		if c.Checks(try.user, try.pass) {
			t.Fatalf("accepted %q with %q", try.user, try.pass)
		}
	}
}

// Off means off. A configuration carrying a password that is not switched on
// must not let anybody in, or turning the login off would do nothing.
func TestAPasswordIsNotCheckedWhenBasicIsOff(t *testing.T) {
	t.Parallel()
	c := basicConfig(t, "clint", "hunter2")
	c.Basic = false
	if c.Checks("clint", "hunter2") {
		t.Fatal("the password worked with the password login switched off")
	}
}

// A NAME AND A PASSWORD IS A COMPLETE CONFIGURATION. Demanding an issuer from
// somebody who configured a password refuses the simple case for missing the
// complicated one.
func TestAPasswordAloneIsAReadyConfiguration(t *testing.T) {
	t.Parallel()
	if err := basicConfig(t, "clint", "hunter2").Ready(); err != nil {
		t.Fatalf("refused a password-only login: %v", err)
	}
}

// And an incomplete one is refused rather than quietly letting everybody in.
func TestAnIncompletePasswordLoginIsRefused(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	var noUser publish.AuthConfig
	noUser = publish.AuthConfig{Enabled: true, Basic: true}
	if err := publish.SetBasicPassword(&noUser, "hunter2"); err != nil {
		t.Fatal(err)
	}
	if err := noUser.Ready(); err == nil {
		t.Fatal("a password with no name was accepted")
	}
	noPass := publish.AuthConfig{Enabled: true, Basic: true, User: "clint"}
	if err := noPass.Ready(); err == nil {
		t.Fatal("a name with no password was accepted")
	}
	if err := publish.SetBasicPassword(&noPass, "   "); err == nil {
		t.Fatal("a password of spaces was accepted")
	}
}

// THE GUARD, end to end over a real request.
func TestTheGuardAsksForAPasswordAndThenLetsYouIn(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	if err := d.SaveAuth(basicConfig(t, "clint", "hunter2")); err != nil {
		t.Fatal(err)
	}
	guarded := d.AuthGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("the board"))
	}))

	// Nothing presented. A browser has to be TOLD to ask, or it shows the
	// error page instead of a prompt.
	rec := httptest.NewRecorder()
	guarded.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("an anonymous request answered %d", rec.Code)
	}
	if !strings.HasPrefix(rec.Header().Get("WWW-Authenticate"), "Basic ") {
		t.Fatalf("nothing asked the browser for a password: %q",
			rec.Header().Get("WWW-Authenticate"))
	}

	// The wrong one.
	rec = httptest.NewRecorder()
	bad := httptest.NewRequest(http.MethodGet, "/", nil)
	bad.SetBasicAuth("clint", "hunter3")
	guarded.ServeHTTP(rec, bad)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("a wrong password answered %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "the board") {
		t.Fatal("a wrong password reached the board")
	}

	// The right one, and a session so the browser is not asked again on every
	// poll and every image.
	rec = httptest.NewRecorder()
	good := httptest.NewRequest(http.MethodGet, "/", nil)
	good.SetBasicAuth("clint", "hunter2")
	guarded.ServeHTTP(rec, good)
	if rec.Code != http.StatusOK {
		t.Fatalf("the right password answered %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "the board") {
		t.Fatal("the right password did not reach the board")
	}
	if rec.Header().Get("Set-Cookie") == "" {
		t.Fatal("no session was issued, so every request pays for the hash again")
	}
}

// THE LOOPBACK BOARD IS NOT GUARDED. This wraps the PUBLISHED handler only,
// and a login that arrived on the local one would lock somebody out of their
// own machine.
func TestAnUnconfiguredLoginLetsEverythingThrough(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	guarded := d.AuthGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("the board"))
	}))
	rec := httptest.NewRecorder()
	guarded.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("an unconfigured board answered %d", rec.Code)
	}
}
