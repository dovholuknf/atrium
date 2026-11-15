//go:build integration

package daemon

import (
	"github.com/dovholuknf/atrium/internal/daemon/publish"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func guardedBoard(d *Daemon) http.Handler {
	return d.AuthGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("the board"))
	}))
}

func oidcConfig(secret string, allow ...string) publish.AuthConfig {
	if len(allow) == 0 {
		allow = []string{"a@example.com"}
	}
	return publish.AuthConfig{
		Enabled: true, Issuer: "https://idp.example/realms/x", ClientID: "atrium", ClientSecret: secret,
		Allow: allow,
	}
}

// markPublicShareRunning pretends a public zrok share is up.
func markPublicShareRunning(t *testing.T, d *Daemon) {
	t.Helper()
	if err := d.SaveOverlayConfig(publish.SettingOverlayZrok, publish.ZrokConfig{Mode: "public"}); err != nil {
		t.Fatal(err)
	}
	n := d.Nat(publish.OverlayZrok)
	n.Mu.Lock()
	n.Srv = &http.Server{}
	n.Mu.Unlock()
	t.Cleanup(func() {
		n.Mu.Lock()
		n.Srv = nil
		n.Mu.Unlock()
	})
}

// POINT 1. Turning the login off under a running public share is refused.
func TestTheLoginCannotBeTurnedOffUnderAPublicShare(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	if err := d.SaveAuth(oidcConfig("s1")); err != nil {
		t.Fatal(err)
	}
	markPublicShareRunning(t, d)

	if err := d.SaveAuth(publish.AuthConfig{}); err == nil {
		t.Fatal("the login was turned off under a running public share")
	}
	off := oidcConfig("s1")
	off.Enabled = false
	if err := d.SaveAuth(off); err == nil {
		t.Fatal("the login was disabled under a running public share")
	}
	if !d.AuthConfig().Enabled {
		t.Fatal("a refused save still changed the stored login")
	}
	if err := d.SaveAuth(oidcConfig("s2")); err != nil {
		t.Fatalf("a working login was refused: %v", err)
	}
}

func TestTheLoginCanBeTurnedOffWhenNoPublicShareRuns(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	if err := d.SaveAuth(oidcConfig("s1")); err != nil {
		t.Fatal(err)
	}
	if err := d.SaveAuth(publish.AuthConfig{}); err != nil {
		t.Fatalf("turning the login off with no share was refused: %v", err)
	}
}

// A stored sign-in that cannot work, such as a password-only one from before passwords were removed, refuses.
func TestAStoredSignInThatCannotWorkFailsClosed(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	if err := d.st.SetSetting(publish.SettingAuth, `{"enabled":true,"basic":true,"user":"clint"}`); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	guardedBoard(d).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("an unusable sign-in answered %d, wanted 503", rec.Code)
	}
}

func TestTheCallbackIsTheAddressTheRequestCameInOn(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = "board.example"
	r.Header.Set("X-Forwarded-Proto", "https")
	if got := publish.RedirectFor(publish.AuthConfig{}, r); got != "https://board.example"+publish.AuthPrefix+"callback" {
		t.Fatalf("callback %q", got)
	}
	r.Header.Set("X-Forwarded-Host", "atrium-x.share.example")
	if got := publish.RedirectFor(publish.AuthConfig{}, r); got != "https://atrium-x.share.example"+publish.AuthPrefix+"callback" {
		t.Fatalf("callback behind a proxy %q", got)
	}
	if got := publish.RedirectFor(publish.AuthConfig{Redirect: "https://fixed/cb"}, r); got != "https://fixed/cb" {
		t.Fatalf("a configured callback was not used: %q", got)
	}
}

// POINT 3. A new client secret ends sessions issued before it.
func TestASecretChangeEndsEarlierSessions(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	if err := d.SaveAuth(oidcConfig("s1")); err != nil {
		t.Fatal(err)
	}
	before := d.SessionGen()
	if err := d.SaveAuth(oidcConfig("s2")); err != nil {
		t.Fatal(err)
	}
	if d.SessionGen() == before {
		t.Fatal("a session issued before a secret change still works")
	}
}

// Saving the same login again does not log everybody out.
func TestSavingTheSameLoginKeepsSessions(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	cfg := oidcConfig("s1")
	if err := d.SaveAuth(cfg); err != nil {
		t.Fatal(err)
	}
	before := d.SessionGen()
	if err := d.SaveAuth(cfg); err != nil {
		t.Fatal(err)
	}
	if d.SessionGen() != before {
		t.Fatal("an unchanged save bumped the generation")
	}
}

// Removing somebody from the allowlist ends their session, and the guard
// re-checks the subject, so a leftover cookie is not enough.
func TestAnAllowlistRemovalEndsThatSession(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	oidc := func(allow ...string) publish.AuthConfig {
		return publish.AuthConfig{
			Enabled: true, Issuer: "https://idp.example/realms/x", ClientID: "atrium",
			Redirect: "https://board.example/auth/callback", Allow: allow,
		}
	}
	if err := d.SaveAuth(oidc("a@example.com", "b@example.com")); err != nil {
		t.Fatal(err)
	}
	key, _ := d.CookieKey()
	mine := func(sub, mail string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(&http.Cookie{Name: publish.AuthCookie, Value: publish.SignSession(key,
			publish.Session{Subject: sub, Email: mail, Gen: d.SessionGen()}, time.Now().Add(time.Hour))})
		return r
	}
	h := guardedBoard(d)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, mine("sub-b", "b@example.com"))
	if rec.Code != http.StatusOK {
		t.Fatalf("an allowed session answered %d", rec.Code)
	}

	// The cookie issued under the old generation, kept for after the change.
	stale := mine("sub-b", "b@example.com")
	if err := d.SaveAuth(oidc("a@example.com")); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, stale)
	if rec.Code == http.StatusOK {
		t.Fatal("a removed user's old session still works")
	}
	// Even a cookie carrying the new generation fails the subject re-check.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, mine("sub-b", "b@example.com"))
	if rec.Code == http.StatusOK {
		t.Fatal("the guard did not re-check the subject against the allowlist")
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, mine("sub-a", "a@example.com"))
	if rec.Code != http.StatusOK {
		t.Fatalf("somebody still allowed answered %d", rec.Code)
	}
}

func TestLogoutIsAPost(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	if err := d.SaveAuth(oidcConfig("s1")); err != nil {
		t.Fatal(err)
	}
	h := guardedBoard(d)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, publish.AuthPrefix+"logout", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("a GET logout answered %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, publish.AuthPrefix+"logout", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("logout answered %d", rec.Code)
	}
}
