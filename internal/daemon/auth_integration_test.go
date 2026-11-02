//go:build integration

package daemon

import (
	"github.com/dovholuknf/atrium/internal/daemon/publish"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("the board"))
	})
}

// Unconfigured is a pass-through. Turning sharing on before setting a provider
// up must leave the board exactly as it was rather than locking it.
func TestWithNoProviderTheGuardLetsEverythingThrough(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	rec := httptest.NewRecorder()
	d.AuthGuard(okHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("an unconfigured board answered %d", rec.Code)
	}
}

// THE ONE THAT MATTERS MOST. The guard is only ever wrapped around the
// published handler, so the loopback board, every hook, the CLI and the MCP
// server are untouched by construction. This asserts the construction.
func TestTheLocalBoardIsNotWrapped(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	if err := d.SaveAuth(publish.AuthConfig{
		Enabled: true, Issuer: "https://idp.example/realms/x",
		ClientID: "atrium", Redirect: "https://board.example/auth/callback",
		Allow: []string{"someone@example.com"},
	}); err != nil {
		t.Fatal(err)
	}
	// The handler the local listener serves, reached the way the api package
	// hands it over. If this ever comes back wrapped, every hook on the machine
	// starts being redirected to a login page.
	rec := httptest.NewRecorder()
	d.ap.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/health", nil))
	if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusFound {
		t.Fatalf("the LOCAL board asked for a login (%d). every hook, the cli and the "+
			"mcp server talk to this handler", rec.Code)
	}
}

// With a provider configured, a browser is sent to log in.
//
// To RENEW rather than to sign in, which is the same journey with one extra
// question asked first: a browser the provider still knows comes back with a
// session having seen nothing at all.
func TestABrowserIsSentToTheProvider(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	if err := d.SaveAuth(publish.AuthConfig{
		Enabled: true, Issuer: "https://idp.example/realms/x",
		ClientID: "atrium", Redirect: "https://board.example/auth/callback",
		Allow: []string{"someone@example.com"},
	}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	d.AuthGuard(okHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("a browser with no session got %d, wanted a redirect", rec.Code)
	}
	if got := rec.Header().Get("Location"); !strings.HasPrefix(got, publish.AuthPrefix+"renew") {
		t.Fatalf("redirected to %q, wanted the renewal endpoint", got)
	}
}

// AN API CALL IS REFUSED RATHER THAN REDIRECTED. Bouncing one through a login
// page produces an HTML document where JSON was expected, which reads as a
// corrupt response rather than as a missing session.
func TestAnApiCallIsRefusedNotRedirected(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	if err := d.SaveAuth(publish.AuthConfig{
		Enabled: true, Issuer: "https://idp.example/realms/x",
		ClientID: "atrium", Redirect: "https://board.example/auth/callback",
		Allow: []string{"someone@example.com"},
	}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	d.AuthGuard(okHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/tasks", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("an api call with no session got %d, wanted 401", rec.Code)
	}
}

// A valid session gets through, and a tampered one does not.
func TestASessionIsCheckedRatherThanTrusted(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	if err := d.SaveAuth(publish.AuthConfig{
		Enabled: true, Issuer: "https://idp.example/realms/x",
		ClientID: "atrium", Redirect: "https://board.example/auth/callback",
		Allow: []string{"someone@example.com"},
	}); err != nil {
		t.Fatal(err)
	}
	key, err := d.CookieKey()
	if err != nil {
		t.Fatal(err)
	}
	good := publish.SignSession(key, publish.Session{
		Subject: "someone", Email: "someone@example.com", Gen: d.SessionGen(),
	}, time.Now().Add(time.Hour))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: publish.AuthCookie, Value: good})
	rec := httptest.NewRecorder()
	d.AuthGuard(okHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("a valid session got %d", rec.Code)
	}

	// The subject swapped, the signature left alone. This is the whole reason
	// the cookie is signed.
	forged := strings.Replace(good, good[:4], "AAAA", 1)
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: publish.AuthCookie, Value: forged})
	rec = httptest.NewRecorder()
	d.AuthGuard(okHandler()).ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("an edited session cookie was accepted")
	}
}

// An expired session is not a session.
func TestAnExpiredSessionIsRefused(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	key, err := d.CookieKey()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := publish.ReadSession(key, publish.SignSession(key, publish.Session{Subject: "someone"}, time.Now().Add(-time.Minute))); ok {
		t.Fatal("a session that expired a minute ago was accepted")
	}
}

// The signing key survives a read, or everybody is logged out on every restart.
func TestTheCookieKeyIsStable(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	first, err := d.CookieKey()
	if err != nil {
		t.Fatal(err)
	}
	second, err := d.CookieKey()
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("the cookie key changed between reads, so every restart logs everybody out")
	}
}

// EMPTY ALLOW LIST MEANS NOBODY, and it is refused at save time rather than
// discovered at sign-in. The tempting default is "anybody the provider
// authenticated", which on a provider with open registration is the whole
// internet with an extra step.
func TestAConfigurationThatCouldNotWorkIsRefused(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	for _, tc := range []struct {
		what string
		cfg  publish.AuthConfig
	}{
		{"no issuer", publish.AuthConfig{Enabled: true, ClientID: "a", Redirect: "b", Allow: []string{"c"}}},
		{"no client", publish.AuthConfig{Enabled: true, Issuer: "a", Redirect: "b", Allow: []string{"c"}}},
		{"no redirect", publish.AuthConfig{Enabled: true, Issuer: "a", ClientID: "b", Allow: []string{"c"}}},
		{"nobody allowed", publish.AuthConfig{Enabled: true, Issuer: "a", ClientID: "b", Redirect: "c"}},
	} {
		if err := d.SaveAuth(tc.cfg); err == nil {
			t.Fatalf("a configuration with %s was accepted", tc.what)
		}
	}
	// And turning it off needs none of them.
	if err := d.SaveAuth(publish.AuthConfig{}); err != nil {
		t.Fatalf("turning the login off was refused: %v", err)
	}
}

// STARTING A LOGIN IS UNAUTHENTICATED, so anybody who can reach the published
// board can ask it to hold a state for five minutes. Without a bound that is a
// way to spend the daemon's memory from outside with no credential at all.
func TestAFloodOfLoginsIsBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	var logins publish.LoginStates
	var refused error
	for i := 0; i < publish.PendingMost+10; i++ {
		if _, _, err := logins.StartLogin("/", false); err != nil {
			refused = err
			break
		}
	}
	if refused == nil {
		t.Fatal("logins were started without limit, so the map grows on request")
	}
	logins.Lock()
	held := len(logins.At)
	logins.Unlock()
	if held > publish.PendingMost {
		t.Fatalf("%d states are held, above the cap of %d", held, publish.PendingMost)
	}
}

// WHERE SOMEBODY LANDS AFTER A LOGIN IS UNDER AN ATTACKER'S CONTROL. It rides
// through a redirect to the provider and back, so a link that sends somebody
// to a page on another domain, having just signed in, is the oldest phishing
// primitive there is.
func TestALoginCannotSendSomebodyOffThisBoard(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ back, want string }{
		{"/", "/"},
		{"/?card=7", "/?card=7"},
		{"", "/"},
		{"//elsewhere.example", "/"},
		{"/\\elsewhere.example", "/"},
		{"https://elsewhere.example", "/"},
		{"javascript:alert(1)", "/"},
	} {
		if got := publish.BackTo(tc.back); got != tc.want {
			t.Fatalf("backTo(%q) = %q, wanted %q", tc.back, got, tc.want)
		}
	}
}

// The cookie key and the auth configuration never leave this machine. The key
// mints sessions for any subject and the configuration holds a client secret.
func TestTheAuthSecretsAreNeverExported(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	if err := d.SaveAuth(publish.AuthConfig{
		Enabled: true, Issuer: "https://idp.example/realms/x",
		ClientID: "atrium", ClientSecret: "a-client-secret-value",
		Redirect: "https://board.example/auth/callback",
		Allow:    []string{"someone@example.com"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.CookieKey(); err != nil {
		t.Fatal(err)
	}
	_, raw := exportOf(t, d)
	if strings.Contains(raw, "a-client-secret-value") {
		t.Fatal("the oidc client secret was exported")
	}
	for _, key := range []string{publish.SettingAuth, publish.SettingAuthKey} {
		if strings.Contains(raw, key) {
			t.Fatalf("%q appears in the export", key)
		}
	}
}
