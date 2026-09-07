package daemon

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// The OIDC round trip, against a provider that checks its side of it.
//
// The tests here need a real provider rather than a fake handler, because what
// is being asserted is what atrium SENDS. A test that only reads atrium's own
// variables would pass with the verifier left out of the exchange, which is the
// exact way PKCE gets shipped as decoration.

// fakeIDP is an OIDC provider small enough to read, and strict enough to fail
// atrium when atrium is wrong.
type fakeIDP struct {
	srv *httptest.Server
	key *rsa.PrivateKey

	mu sync.Mutex
	// challenge is what the test saw atrium send to the authorize endpoint,
	// handed back here so the token endpoint can check the verifier against it
	// the way a provider does.
	challenge string
	// sub and email go into the id token.
	sub, email string
	// refusals recorded for the test to read.
	exchanges int
	lastForm  url.Values
}

func newFakeIDP(t *testing.T) *fakeIDP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	idp := &fakeIDP{key: key, sub: "sub-1", email: "someone@example.com"}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 idp.srv.URL,
			"authorization_endpoint": idp.srv.URL + "/authorize",
			"token_endpoint":         idp.srv.URL + "/token",
			"jwks_uri":               idp.srv.URL + "/jwks",
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kid": "k1", "kty": "RSA",
			"n": base64.RawURLEncoding.EncodeToString(idp.key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(idp.key.E)).Bytes()),
		}}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		idp.mu.Lock()
		idp.exchanges++
		idp.lastForm = r.PostForm
		want := idp.challenge
		sub, email := idp.sub, idp.email
		idp.mu.Unlock()

		// THE PROVIDER'S HALF OF PKCE. A code is only spendable by whoever
		// holds the verifier behind the challenge the authorize request
		// carried, and a provider that skips this check is a provider that
		// would let atrium ship PKCE that does nothing.
		got := r.PostForm.Get("code_verifier")
		if want != "" && (got == "" || pkceChallenge(got) != want) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":` +
				`"the code_verifier does not match the code_challenge"}`))
			return
		}
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
			"iss": idp.srv.URL, "aud": "atrium", "sub": sub, "email": email,
			"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
		})
		tok.Header["kid"] = "k1"
		signed, err := tok.SignedString(idp.key)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id_token": signed})
	})

	idp.srv = httptest.NewServer(mux)
	t.Cleanup(idp.srv.Close)
	return idp
}

// on configures a daemon to use this provider, and returns it.
func (idp *fakeIDP) on(t *testing.T) *Daemon {
	t.Helper()
	d := testDaemon(t)
	if err := d.SaveAuth(AuthConfig{
		Enabled: true, Issuer: idp.srv.URL, ClientID: "atrium",
		Redirect: "https://board.example/auth/callback",
		Allow:    []string{idp.email},
	}); err != nil {
		t.Fatal(err)
	}
	return d
}

// authorizeOf drives the guard until it hands over a provider URL, and returns
// the query atrium put on it.
func authorizeOf(t *testing.T, d *Daemon, path string) url.Values {
	t.Helper()
	rec := httptest.NewRecorder()
	d.authGuard(okHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("%s answered %d (%s), wanted a redirect", path, rec.Code, rec.Body.String())
	}
	u, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	// One hop through atrium's own endpoint, since the guard sends a browser
	// to renew rather than straight out to the provider.
	if strings.HasPrefix(u.Path, authPrefix) {
		return authorizeOf(t, d, u.RequestURI())
	}
	return u.Query()
}

// PKCE IS ON EVERY LOGIN AND THERE IS NO SWITCH FOR IT. A challenge atrium can
// be configured out of is a challenge that is off on the board that needed it.
func TestALoginCarriesAPkceChallenge(t *testing.T) {
	idp := newFakeIDP(t)
	d := idp.on(t)
	q := authorizeOf(t, d, "/")

	if q.Get("code_challenge") == "" {
		t.Fatal("the authorize request carried no code challenge")
	}
	if got := q.Get("code_challenge_method"); got != "S256" {
		t.Fatalf("the challenge method is %q, and anything but S256 sends the secret "+
			"in the same redirect an attacker is already watching", got)
	}
	if q.Get("state") == "" {
		t.Fatal("the authorize request carried no state")
	}
}

// THE VERIFIER IS NEVER IN THE REDIRECT. Sending it to the browser puts it in
// history, in a referer and in every proxy on the way, which is every place the
// code it protects already is.
func TestTheVerifierNeverGoesToTheBrowser(t *testing.T) {
	idp := newFakeIDP(t)
	d := idp.on(t)

	rec := httptest.NewRecorder()
	d.authGuard(okHandler()).ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, authPrefix+"login", nil))
	sent := rec.Header().Get("Location") + " " + rec.Body.String()

	pending.Lock()
	var verifier string
	for _, l := range pending.at {
		verifier = l.verifier
	}
	pending.Unlock()
	if verifier == "" {
		t.Fatal("no login was recorded, so this test proves nothing")
	}
	if strings.Contains(sent, verifier) {
		t.Fatal("the verifier was sent to the browser, so it is in the history and " +
			"the referer along with the code it is meant to protect")
	}
}

// The whole round trip, against a provider that refuses an exchange without a
// matching verifier. This is the test that fails if PKCE is decoration.
func TestASignInSpendsItsCodeWithTheVerifier(t *testing.T) {
	idp := newFakeIDP(t)
	d := idp.on(t)
	q := authorizeOf(t, d, "/?card=7")

	// The provider now knows what it was shown, and will hold atrium to it.
	idp.mu.Lock()
	idp.challenge = q.Get("code_challenge")
	idp.mu.Unlock()

	rec := httptest.NewRecorder()
	d.authGuard(okHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		authPrefix+"callback?state="+url.QueryEscape(q.Get("state"))+"&code=a-code", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("the callback answered %d: %s", rec.Code, rec.Body.String())
	}

	idp.mu.Lock()
	form := idp.lastForm
	idp.mu.Unlock()
	if form.Get("code_verifier") == "" {
		t.Fatal("the token exchange carried no code_verifier, so a stolen code is " +
			"spendable by whoever stole it")
	}
	if pkceChallenge(form.Get("code_verifier")) != q.Get("code_challenge") {
		t.Fatal("the verifier sent does not match the challenge shown")
	}

	// And what came back is a session this board will accept.
	key, err := d.cookieKey()
	if err != nil {
		t.Fatal(err)
	}
	var cookie string
	for _, c := range rec.Result().Cookies() {
		if c.Name == authCookie {
			cookie = c.Value
		}
	}
	if cookie == "" {
		t.Fatal("a completed sign-in set no session cookie")
	}
	if sub, ok := readSession(key, cookie); !ok || sub != idp.sub {
		t.Fatalf("the session says %q, ok=%v", sub, ok)
	}
	// And it puts somebody back where they were rather than on the front page.
	if got := rec.Header().Get("Location"); got != "/?card=7" {
		t.Fatalf("after signing in the browser went to %q", got)
	}
}

// A RENEWAL ASKS AND DOES NOT INTERRUPT. `prompt=none` is the whole of it: the
// provider answers from the session it already has, or refuses, and either way
// nobody is shown a form they did not ask for.
func TestARenewalAsksTheProviderSilently(t *testing.T) {
	idp := newFakeIDP(t)
	d := idp.on(t)
	if got := authorizeOf(t, d, "/").Get("prompt"); got != "none" {
		t.Fatalf("a renewal asked with prompt=%q, so it would show a login form to "+
			"somebody who was in the middle of something", got)
	}
	// A sign-in asked for on purpose is not silent, or nobody could ever get
	// past a provider that has forgotten them.
	rec := httptest.NewRecorder()
	d.authGuard(okHandler()).ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, authPrefix+"login", nil))
	u, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Get("prompt") != "" {
		t.Fatal("an explicit sign-in was sent as prompt=none, so a provider that has " +
			"forgotten this browser refuses it and nobody can ever sign in")
	}
}

// A DECLINED RENEWAL IS NOT AN ERROR, and this is the one that decides whether
// the feature is usable. `prompt=none` against a provider that has forgotten
// this browser answers `login_required`, which is the expected answer. Showing
// it as an error page means every expired session lands on the word "refused"
// with no way forward.
func TestADeclinedRenewalEndsAtALoginAndNotAnError(t *testing.T) {
	idp := newFakeIDP(t)
	d := idp.on(t)
	q := authorizeOf(t, d, "/?card=7")

	rec := httptest.NewRecorder()
	d.authGuard(okHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		authPrefix+"callback?error=login_required&state="+
			url.QueryEscape(q.Get("state")), nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("a declined renewal answered %d (%s), wanted somebody to be sent to "+
			"sign in", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, authPrefix+"login") {
		t.Fatalf("a declined renewal sent the browser to %q", loc)
	}
	// And it remembers where they were going, or renewing costs somebody their
	// place every time the provider says no.
	if !strings.Contains(loc, url.QueryEscape("/?card=7")) {
		t.Fatalf("a declined renewal forgot where somebody was: %q", loc)
	}
}

// A SIGN-IN SOMEBODY ASKED FOR, refused by the provider, still says so. The
// silent path swallows a refusal on purpose, and swallowing this one too would
// turn a misconfigured client id into an endless bounce with nothing on screen.
func TestARefusedSignInIsStillReported(t *testing.T) {
	idp := newFakeIDP(t)
	d := idp.on(t)

	rec := httptest.NewRecorder()
	d.authGuard(okHandler()).ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, authPrefix+"login", nil))
	u, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}

	rec = httptest.NewRecorder()
	d.authGuard(okHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		authPrefix+"callback?error=unauthorized_client&state="+
			url.QueryEscape(u.Query().Get("state")), nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a refused sign-in answered %d, wanted the provider's refusal said "+
			"out loud", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "unauthorized_client") {
		t.Fatalf("the refusal did not name what the provider said: %q", rec.Body.String())
	}
}

// A callback carrying an error and a state nobody issued is refused rather than
// treated as a renewal. Otherwise anybody can turn the error path into a
// redirect by inventing a state.
func TestAnErrorWithAnUnknownStateIsRefused(t *testing.T) {
	idp := newFakeIDP(t)
	d := idp.on(t)
	rec := httptest.NewRecorder()
	d.authGuard(okHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		authPrefix+"callback?error=login_required&state=never-issued", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a callback with a state nobody issued answered %d", rec.Code)
	}
}

// The provider's signature is the entire reason to believe any of this, and a
// key that is not the provider's does not become one by being well formed.
func TestATokenSignedBySomebodyElseIsRefused(t *testing.T) {
	idp := newFakeIDP(t)
	d := idp.on(t)
	q := authorizeOf(t, d, "/")

	// The same claims, signed with a key the provider does not publish. This
	// is what an attacker who can answer atrium's token request has.
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss": idp.srv.URL, "aud": "atrium", "sub": idp.sub, "email": idp.email,
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	tok.Header["kid"] = "k1"
	signed, err := tok.SignedString(other)
	if err != nil {
		t.Fatal(err)
	}

	disc, err := discoverFor(t.Context(), idp.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.claimsOf(t.Context(), disc, d.authConfig(), signed); err == nil {
		t.Fatal("a token signed with a key the provider does not publish was accepted")
	}
	takeLogin(q.Get("state"))
}

// A sanity check on the fake, so a green suite above means what it says. If the
// provider would accept an exchange with no verifier, none of the PKCE tests
// prove anything.
func TestTheFakeProviderRefusesAnExchangeWithoutTheVerifier(t *testing.T) {
	idp := newFakeIDP(t)
	idp.challenge = pkceChallenge("a-verifier-atrium-does-not-have")

	form := url.Values{"grant_type": {"authorization_code"}, "code": {"a-code"}}
	res, err := http.PostForm(idp.srv.URL+"/token", form)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode < 400 {
		t.Fatal("the fake provider accepted an exchange with no verifier, so the tests " +
			"above would pass with pkce removed")
	}
}

// The challenge is the hash the RFC says it is, checked against a value worked
// out by hand rather than by calling the same function twice.
func TestTheChallengeIsTheHashOfTheVerifier(t *testing.T) {
	sum := sha256.Sum256([]byte("a-verifier"))
	if got := pkceChallenge("a-verifier"); got != base64.RawURLEncoding.EncodeToString(sum[:]) {
		t.Fatalf("the challenge is %q, which is not the base64url sha256 of the verifier", got)
	}
	if strings.ContainsAny(pkceChallenge("a-verifier"), "+/=") {
		t.Fatal("the challenge is standard base64, which a provider will reject")
	}
}
