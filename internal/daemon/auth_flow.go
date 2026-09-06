package daemon

import (
	"context"
	"crypto/rand"
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

// pending is the logins this board has started but not finished.
//
// In memory, and short lived. A state that outlived a restart would be a state
// an attacker could hold onto, and the cost of losing them is that somebody
// mid-login presses the button again.
var pending struct {
	sync.Mutex
	at map[string]time.Time
}

func newState() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	s := base64.RawURLEncoding.EncodeToString(b)

	pending.Lock()
	defer pending.Unlock()
	if pending.at == nil {
		pending.at = map[string]time.Time{}
	}
	// Swept here rather than on a timer. The map is small, this runs once per
	// login, and a timer would be a goroutine for housekeeping nobody is
	// waiting on.
	for k, t := range pending.at {
		if time.Since(t) > authStateFor {
			delete(pending.at, k)
		}
	}
	pending.at[s] = time.Now()
	return s, nil
}

// takeState consumes one. SINGLE USE: a state that could be replayed is a
// callback that could be replayed.
func takeState(s string) bool {
	pending.Lock()
	defer pending.Unlock()
	t, ok := pending.at[s]
	delete(pending.at, s)
	return ok && time.Since(t) <= authStateFor
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
		http.Redirect(w, r, authPrefix+"login", http.StatusFound)
	})
}

// serveAuth handles the three endpoints under the prefix.
func (d *Daemon) serveAuth(w http.ResponseWriter, r *http.Request, cfg AuthConfig) {
	switch strings.TrimPrefix(r.URL.Path, authPrefix) {
	case "login":
		d.authLogin(w, r, cfg)
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

func (d *Daemon) authLogin(w http.ResponseWriter, r *http.Request, cfg AuthConfig) {
	disc, err := discoverFor(r.Context(), cfg.Issuer)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	state, err := newState()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	q := url.Values{}
	q.Set("client_id", cfg.ClientID)
	q.Set("redirect_uri", cfg.Redirect)
	q.Set("response_type", "code")
	q.Set("scope", "openid email profile")
	q.Set("state", state)
	http.Redirect(w, r, disc.AuthURL+"?"+q.Encode(), http.StatusFound)
}

func (d *Daemon) authCallback(w http.ResponseWriter, r *http.Request, cfg AuthConfig) {
	if e := r.URL.Query().Get("error"); e != "" {
		http.Error(w, "the provider refused: "+e, http.StatusForbidden)
		return
	}
	if !takeState(r.URL.Query().Get("state")) {
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
	log.Printf("[atrium] %s signed in to the published board", email)
	http.Redirect(w, r, "/", http.StatusFound)
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
