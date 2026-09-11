package daemon

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A name and a password in front of a published board.
//
// The OIDC flow beside it is the better answer and it needs a provider, a
// client registered with it, and a redirect matching the published address
// exactly. Somebody putting a board on a share for an afternoon has none of
// those, and what they reach for instead is publishing with no login at all.

func basicConfig(t *testing.T, user, pass string) AuthConfig {
	t.Helper()
	c := AuthConfig{Enabled: true, Basic: true, User: user}
	if err := setBasicPassword(&c, pass); err != nil {
		t.Fatal(err)
	}
	return c
}

// THE PASSWORD IS NEVER STORED. A database somebody takes has to be worth
// nothing without the guessing.
func TestThePasswordIsNotKept(t *testing.T) {
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
	c := basicConfig(t, "clint", "hunter2")
	if !c.checks("clint", "hunter2") {
		t.Fatal("the configured password was refused")
	}
}

func TestAWrongPasswordOrNameIsRefused(t *testing.T) {
	c := basicConfig(t, "clint", "hunter2")
	for _, try := range []struct{ user, pass string }{
		{"clint", "hunter3"},
		{"clint", ""},
		{"someone", "hunter2"},
		{"", "hunter2"},
		{"", ""},
	} {
		if c.checks(try.user, try.pass) {
			t.Fatalf("accepted %q with %q", try.user, try.pass)
		}
	}
}

// Off means off. A configuration carrying a password that is not switched on
// must not let anybody in, or turning the login off would do nothing.
func TestAPasswordIsNotCheckedWhenBasicIsOff(t *testing.T) {
	c := basicConfig(t, "clint", "hunter2")
	c.Basic = false
	if c.checks("clint", "hunter2") {
		t.Fatal("the password worked with the password login switched off")
	}
}

// A NAME AND A PASSWORD IS A COMPLETE CONFIGURATION. Demanding an issuer from
// somebody who configured a password refuses the simple case for missing the
// complicated one.
func TestAPasswordAloneIsAReadyConfiguration(t *testing.T) {
	if err := basicConfig(t, "clint", "hunter2").ready(); err != nil {
		t.Fatalf("refused a password-only login: %v", err)
	}
}

// And an incomplete one is refused rather than quietly letting everybody in.
func TestAnIncompletePasswordLoginIsRefused(t *testing.T) {
	var noUser AuthConfig
	noUser = AuthConfig{Enabled: true, Basic: true}
	if err := setBasicPassword(&noUser, "hunter2"); err != nil {
		t.Fatal(err)
	}
	if err := noUser.ready(); err == nil {
		t.Fatal("a password with no name was accepted")
	}
	noPass := AuthConfig{Enabled: true, Basic: true, User: "clint"}
	if err := noPass.ready(); err == nil {
		t.Fatal("a name with no password was accepted")
	}
	if err := setBasicPassword(&noPass, "   "); err == nil {
		t.Fatal("a password of spaces was accepted")
	}
}

// THE GUARD, end to end over a real request.
func TestTheGuardAsksForAPasswordAndThenLetsYouIn(t *testing.T) {
	d := testDaemon(t)
	if err := d.SaveAuth(basicConfig(t, "clint", "hunter2")); err != nil {
		t.Fatal(err)
	}
	guarded := d.authGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	d := testDaemon(t)
	guarded := d.authGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("the board"))
	}))
	rec := httptest.NewRecorder()
	guarded.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("an unconfigured board answered %d", rec.Code)
	}
}
