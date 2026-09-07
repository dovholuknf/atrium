package daemon

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// The OIDC round trip, and the guard that wraps the published board.
//
// Discovery, the redirect out, the code exchange, and verifying what came back.
// Deliberately small: atrium is a client of somebody else's provider and has no
// business being anything more.

// authPrefix is where the login endpoints live.
//
// UNDER ONE PREFIX so the guard can let exactly these through unauthenticated
// and nothing else. A guard that has to enumerate exceptions scattered over the
// API is a guard with a hole in it the week somebody adds a route.
const authPrefix = "/auth/"

// discovery is the part of an OIDC provider's metadata this needs.
type discovery struct {
	Issuer   string `json:"issuer"`
	AuthURL  string `json:"authorization_endpoint"`
	TokenURL string `json:"token_endpoint"`
	JWKSURL  string `json:"jwks_uri"`
}

// discovered caches provider metadata, which does not change and costs a round
// trip nobody should pay per login.
var discovered struct {
	sync.Mutex
	at  map[string]*discovery
	got map[string]time.Time
}

// discoverFor reads a provider's metadata, or returns what it read earlier.
func discoverFor(ctx context.Context, issuer string) (*discovery, error) {
	issuer = strings.TrimRight(strings.TrimSpace(issuer), "/")

	discovered.Lock()
	if d, ok := discovered.at[issuer]; ok && time.Since(discovered.got[issuer]) < time.Hour {
		discovered.Unlock()
		return d, nil
	}
	discovered.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		issuer+"/.well-known/openid-configuration", nil)
	if err != nil {
		return nil, err
	}
	res, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach the provider at %s: %w", issuer, err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return nil, fmt.Errorf("the provider at %s answered %s to discovery. is that the "+
			"realm URL rather than the console URL", issuer, res.Status)
	}
	var d discovery
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&d); err != nil {
		return nil, fmt.Errorf("the provider's metadata could not be read: %w", err)
	}
	if d.AuthURL == "" || d.TokenURL == "" {
		return nil, fmt.Errorf("the provider's metadata has no authorization or token endpoint")
	}

	discovered.Lock()
	if discovered.at == nil {
		discovered.at, discovered.got = map[string]*discovery{}, map[string]time.Time{}
	}
	discovered.at[issuer], discovered.got[issuer] = &d, time.Now()
	discovered.Unlock()
	return &d, nil
}

// pendingLogin is one login this board started and has not finished.
type pendingLogin struct {
	// verifier is the PKCE secret for this login, and the whole point of PKCE
	// is that it is never sent to the provider until the exchange.
	//
	// What it buys, given the state parameter already exists: state proves the
	// callback belongs to a login this board started, and nothing more. A code
	// stolen out of the callback URL, from a browser history, a proxy log or a
	// referer, is still spendable by whoever holds it, because the token
	// endpoint has no way to tell that the caller is not this board. With
	// PKCE, spending a code needs the verifier as well, and the verifier only
	// ever existed in this process's memory.
	verifier string
	// back is where to put the browser afterwards, so a renewal in the middle
	// of somebody's work does not dump them on the front page.
	back string
	// silent says this was `prompt=none`: an attempt to renew a session
	// against a provider that may or may not still know this browser. A silent
	// attempt that fails is NORMAL and must end at a login form rather than at
	// an error page.
	silent bool
	at     time.Time
}

// pending is the logins this board has started but not finished.
//
// In memory, and short lived. A state that outlived a restart would be a state
// an attacker could hold onto, and the cost of losing them is that somebody
// mid-login presses the button again.
var pending struct {
	sync.Mutex
	at map[string]pendingLogin
}

// pendingMost bounds the map, because starting a login is unauthenticated.
//
// Anybody who can reach the published board can ask it to begin one, and each
// one costs an entry that lives for five minutes. Without a cap that is a way
// to spend the daemon's memory from outside with no credential at all. A real
// board has a handful of logins in flight, so any number that a human could
// reach is far below this.
const pendingMost = 4096

// startLogin records one and returns the state to send the provider.
func startLogin(back string, silent bool) (string, pendingLogin, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", pendingLogin{}, err
	}
	// Thirty two bytes, which is the top of the range RFC 7636 allows for a
	// verifier once it is base64url encoded.
	v := make([]byte, 32)
	if _, err := rand.Read(v); err != nil {
		return "", pendingLogin{}, err
	}
	state := base64.RawURLEncoding.EncodeToString(b)
	l := pendingLogin{
		verifier: base64.RawURLEncoding.EncodeToString(v),
		back:     backTo(back), silent: silent, at: time.Now(),
	}

	pending.Lock()
	defer pending.Unlock()
	if pending.at == nil {
		pending.at = map[string]pendingLogin{}
	}
	// Swept here rather than on a timer. The map is small, this runs once per
	// login, and a timer would be a goroutine for housekeeping nobody is
	// waiting on.
	for k, p := range pending.at {
		if time.Since(p.at) > authStateFor {
			delete(pending.at, k)
		}
	}
	if len(pending.at) >= pendingMost {
		return "", pendingLogin{}, fmt.Errorf("too many sign-ins are already in flight on this " +
			"board. wait a few minutes and start again")
	}
	pending.at[state] = l
	return state, l, nil
}

// takeLogin consumes one. SINGLE USE: a state that could be replayed is a
// callback that could be replayed.
func takeLogin(state string) (pendingLogin, bool) {
	pending.Lock()
	defer pending.Unlock()
	l, ok := pending.at[state]
	delete(pending.at, state)
	return l, ok && time.Since(l.at) <= authStateFor
}

// pkceChallenge is what the provider is shown at the start of a login, and it
// is a hash so that seeing it tells an eavesdropper nothing about the verifier.
//
// S256 and never `plain`. A `plain` challenge is the verifier itself, sent in
// the same redirect an attacker would have to be watching anyway, which buys
// exactly nothing over not doing it.
func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// backTo returns somewhere on this board that a browser may be sent after a
// login, given whatever arrived in the request.
//
// ONLY A PATH HERE, and everything else becomes the front page. The value
// travels through a redirect to a provider and back, so it is under an
// attacker's control end to end: a login link that sends somebody to a page
// they did not expect, on a domain they did not expect, is the oldest phishing
// primitive there is. `//elsewhere.example` and `/\elsewhere.example` are the
// two that look like paths and are not.
func backTo(v string) string {
	if !strings.HasPrefix(v, "/") || strings.HasPrefix(v, "//") || strings.HasPrefix(v, "/\\") {
		return "/"
	}
	return v
}

// authGuard wraps a handler so it asks who you are.
//
// ONLY EVER WRAPS THE PUBLISHED HANDLER. See the header in `auth.go`: the
// loopback board, the hooks, the CLI, the MCP server and a lent session are all
// on other handlers and none of them is touched.
//
// Unconfigured is a pass-through, so turning sharing on before setting a
// provider up leaves the board exactly as it was rather than locking it.
func (d *Daemon) authGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg := d.authConfig()
		if !cfg.Enabled {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, authPrefix) {
			d.serveAuth(w, r, cfg)
			return
		}
		key, err := d.cookieKey()
		if err != nil {
			http.Error(w, "this board cannot check who you are", http.StatusInternalServerError)
			return
		}
		if c, err := r.Cookie(authCookie); err == nil {
			if _, ok := readSession(key, c.Value); ok {
				next.ServeHTTP(w, r)
				return
			}
		}
		// A BROWSER IS REDIRECTED AND ANYTHING ELSE IS REFUSED. Bouncing an
		// API call through a login page produces an HTML document where JSON
		// was expected, which reads as a corrupt response rather than as a
		// missing session.
		if strings.HasPrefix(r.URL.Path, "/v1/") || r.Method != http.MethodGet {
			http.Error(w, "not signed in", http.StatusUnauthorized)
			return
		}
		// SENT TO RENEW RATHER THAN TO SIGN IN. The provider is asked
		// silently first, and a browser whose provider session is still alive
		// comes straight back with a new one having seen nothing. Only a
		// provider that has forgotten this browser produces a login form.
		http.Redirect(w, r, authPrefix+"renew?back="+url.QueryEscape(r.URL.RequestURI()),
			http.StatusFound)
	})
}

// serveAuth handles the endpoints under the prefix.
func (d *Daemon) serveAuth(w http.ResponseWriter, r *http.Request, cfg AuthConfig) {
	switch strings.TrimPrefix(r.URL.Path, authPrefix) {
	case "login":
		d.authStart(w, r, cfg, false)
	case "renew":
		d.authStart(w, r, cfg, true)
	case "callback":
		d.authCallback(w, r, cfg)
	case "logout":
		http.SetCookie(w, &http.Cookie{
			Name: authCookie, Value: "", Path: "/", MaxAge: -1,
			HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
		})
		http.Redirect(w, r, "/", http.StatusFound)
	default:
		http.NotFound(w, r)
	}
}

// authStart sends a browser to the provider, either to sign in or to renew.
//
// ONE FUNCTION FOR BOTH, because the two requests differ by a single parameter
// and a second copy is how the renewal path ends up without PKCE on it.
func (d *Daemon) authStart(w http.ResponseWriter, r *http.Request, cfg AuthConfig, silent bool) {
	disc, err := discoverFor(r.Context(), cfg.Issuer)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	state, l, err := startLogin(r.URL.Query().Get("back"), silent)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	q := url.Values{}
	q.Set("client_id", cfg.ClientID)
	q.Set("redirect_uri", cfg.Redirect)
	q.Set("response_type", "code")
	q.Set("scope", "openid email profile")
	q.Set("state", state)
	q.Set("code_challenge", pkceChallenge(l.verifier))
	q.Set("code_challenge_method", "S256")
	if silent {
		// `prompt=none` says: answer from the session you already have, and
		// refuse rather than show this person anything. It is the whole of the
		// renewal, and it is why renewing costs atrium no stored credential.
		q.Set("prompt", "none")
	}
	http.Redirect(w, r, disc.AuthURL+"?"+q.Encode(), http.StatusFound)
}

func (d *Daemon) authCallback(w http.ResponseWriter, r *http.Request, cfg AuthConfig) {
	// THE STATE IS CONSUMED FIRST, before the provider's error is looked at,
	// because the state is what says whether this was a silent renewal. Read
	// the other way round, a renewal the provider declined becomes an error
	// page, and the state it belonged to is left in the map to expire.
	l, ok := takeLogin(r.URL.Query().Get("state"))
	if e := r.URL.Query().Get("error"); e != "" {
		// A DECLINED RENEWAL IS NOT AN ERROR. `prompt=none` against a provider
		// that has forgotten this browser answers `login_required`, which is
		// the expected answer and means only that this person has to sign in
		// properly now.
		if ok && l.silent {
			http.Redirect(w, r, authPrefix+"login?back="+url.QueryEscape(l.back),
				http.StatusFound)
			return
		}
		http.Error(w, "the provider refused: "+e, http.StatusForbidden)
		return
	}
	if !ok {
		// Not a login this board started, or one that took too long.
		http.Error(w, "that sign-in did not come from here, or it took too long. "+
			"start again", http.StatusForbidden)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "the provider sent no code", http.StatusBadRequest)
		return
	}
	disc, err := discoverFor(r.Context(), cfg.Issuer)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", cfg.Redirect)
	form.Set("client_id", cfg.ClientID)
	// The verifier, which never left this process until now. A stolen code is
	// not spendable without it, and this board is the only holder.
	form.Set("code_verifier", l.verifier)
	if cfg.ClientSecret != "" {
		form.Set("client_secret", cfg.ClientSecret)
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, disc.TokenURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		http.Error(w, "could not reach the provider: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 400 {
		// The provider's own words. A token endpoint refusing is almost always
		// a client id, a secret or a redirect that does not match what it was
		// registered with, and it says which.
		http.Error(w, "the provider refused the exchange: "+string(body), http.StatusForbidden)
		return
	}
	var tok struct {
		IDToken string `json:"id_token"`
	}
	if err := json.Unmarshal(body, &tok); err != nil || tok.IDToken == "" {
		http.Error(w, "the provider returned no id token", http.StatusBadGateway)
		return
	}

	subject, email, err := d.claimsOf(r.Context(), disc, cfg, tok.IDToken)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	if !cfg.allows(subject, email) {
		// NAMED, because the operator adding themselves to the list needs to
		// know which of the two the provider actually sends, and guessing is
		// how somebody ends up locked out of their own board.
		log.Printf("[atrium] refused a sign-in from subject %q email %q: not on the allow list",
			subject, email)
		http.Error(w, "signed in as "+email+" ("+subject+"), which is not on this board's "+
			"allow list", http.StatusForbidden)
		return
	}

	key, err := d.cookieKey()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	until := time.Now().Add(authSessionFor)
	http.SetCookie(w, &http.Cookie{
		Name: authCookie, Value: signSession(key, subject, until), Path: "/",
		Expires: until, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	})
	if !l.silent {
		// A renewal is not worth a line. It happens on a timer nobody set and
		// logging it turns the daemon's log into a heartbeat.
		log.Printf("[atrium] %s signed in to the published board", email)
	}
	// Checked again on the way out rather than trusted from the map. It is one
	// comparison, and the alternative is that a later edit which puts anything
	// else into `back` silently becomes an open redirect.
	http.Redirect(w, r, backTo(l.back), http.StatusFound)
}

// claimsOf verifies the id token against the provider's keys and reads who it
// is about.
//
// VERIFIED, not decoded. An id token that is merely parsed is a claim anybody
// can write: the signature is the entire reason to believe it, and skipping it
// turns a login into a form where you type your own subject.
func (d *Daemon) claimsOf(ctx context.Context, disc *discovery, cfg AuthConfig, raw string) (
	subject, email string, err error) {

	if disc.JWKSURL == "" {
		return "", "", fmt.Errorf("the provider publishes no signing keys, so nothing it " +
			"sends can be verified")
	}
	keys, err := jwksFor(ctx, disc.JWKSURL)
	if err != nil {
		return "", "", err
	}
	var claims jwt.MapClaims
	_, err = jwt.ParseWithClaims(raw, &claims, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		k, ok := keys[kid]
		if !ok {
			return nil, fmt.Errorf("the token was signed with a key the provider does not publish")
		}
		return k, nil
	}, jwt.WithIssuer(strings.TrimRight(cfg.Issuer, "/")),
		jwt.WithAudience(cfg.ClientID),
		jwt.WithExpirationRequired())
	if err != nil {
		return "", "", fmt.Errorf("that sign-in could not be verified: %w", err)
	}
	sub, _ := claims["sub"].(string)
	mail, _ := claims["email"].(string)
	if sub == "" {
		return "", "", fmt.Errorf("the provider's token says nothing about who it is for")
	}
	return sub, mail, nil
}
