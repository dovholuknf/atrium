package daemon

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

// A login in front of the board, and ONLY in front of the published one.
//
// This reverses a rule written in `CLAUDE.md` and `docs/overlays.md`:
// authentication is out of scope, single machine, loopback only, and reaching
// the board from elsewhere is an overlay's job rather than an auth layer
// invented here.
//
// That held while the board was only ever on loopback or behind a private
// share. It stopped holding when a reserved public address made handing out a
// link comfortable. A public URL with no login in front of a board that reads
// files, answers permission prompts and types into terminals is not a line
// worth defending on principle.
//
// WHAT DID NOT CHANGE, and this is the part the rule was protecting: ATRIUM
// STILL OWNS NO CREDENTIALS. There is no user table, no password, and nothing
// to hash. Identity is delegated to an OIDC provider, atrium verifies what that
// provider signed, and the cookie afterwards proves a completed verification
// rather than standing in for a password. A design review flagged the original
// plan, which included username and password, as contradicting the settled
// decision. It was right, and this is what survived.
//
// WHERE IT APPLIES IS THE OTHER HALF OF THE DESIGN, and it is what makes this
// safe to add. The overlay listener is a different `net.Listener` served by a
// different `http.Server`, so the guard wraps THAT handler and nothing else:
//
//   - The loopback board is untouched. No login, exactly as before.
//   - Every hook, the CLI and the MCP server keep working, because they talk
//     to loopback and were never going to carry a credential.
//   - A lent session is untouched. It has its own handler and its own
//     allowlist, and the address IS the credential there by design. Making a
//     guest sign in would defeat the feature.
//
// So this is not "atrium grew authentication". It is "the published board asks
// who you are, using somebody else's identity provider".
//
// AND IT STILL HOLDS NOTHING, which is the reason there is no refresh token
// anywhere in here. A refresh token is a long-lived credential belonging to
// the person who signed in, and one sitting in atrium's database is atrium
// holding somebody else's credential wearing a different word. A session that
// runs out is renewed by asking the provider again with `prompt=none`, which
// costs a redirect nobody sees and leaves nothing behind. See `auth_flow.go`.

// SettingAuth is where the configuration lives.
const SettingAuth = "auth_oidc"

// SettingAuthKey is the key that signs session cookies.
//
// Generated once and kept, because regenerating it on every start would log
// everybody out on every restart. It is a secret and is on the never-exported
// list for that reason.
const SettingAuthKey = "auth_cookie_key"

// authCookie is the name of the session cookie.
const authCookie = "atrium_session"

// authSessionFor is how long a session lasts before the provider is asked
// again.
//
// Twelve hours. Long enough to not be a nuisance across a working day, short
// enough that revoking somebody at the provider takes effect the same day
// rather than whenever they next close their browser.
//
// Renewal did not change this number, on purpose. Renewal made the end of a
// session silent rather than a login form, so a shorter one is now cheap to
// try if somebody wants revocation to bite sooner. The cost is not zero: every
// renewal is a full page load, and the board is one page that holds open
// terminals.
const authSessionFor = 12 * time.Hour

// authStateFor bounds how long a login may take.
//
// The state parameter is what ties a callback to a login this board started.
// Five minutes is generous for a human typing a password and short enough that
// a captured link is not usable later.
const authStateFor = 5 * time.Minute

// AuthConfig is what makes the published board ask who you are.
//
// EMPTY MEANS OFF, and off is the default. A board that suddenly demanded a
// login after an upgrade, with no provider configured, would be a board nobody
// could reach.
type AuthConfig struct {
	// Enabled is the switch. Off with no provider configured is the only
	// sensible default, and turning it on without an issuer is refused rather
	// than silently ignored.
	Enabled bool `json:"enabled"`
	// Issuer is the OIDC provider, such as a Keycloak realm URL. Discovery
	// hangs off it, so nothing else about the provider has to be configured.
	Issuer string `json:"issuer"`
	// ClientID and ClientSecret identify this board to the provider. A public
	// client leaves the secret empty. PKCE is on either way and there is no
	// switch for it, so a confidential client is a second factor on the
	// exchange rather than the only one.
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret,omitempty"`
	// Redirect is where the provider sends somebody back to. It has to be the
	// PUBLISHED address, because that is where the browser is, and it has to
	// match what the provider was told exactly.
	Redirect string `json:"redirect"`
	// Allow is who may in. Email addresses or subjects, whichever the provider
	// puts in the token.
	//
	// EMPTY MEANS NOBODY, deliberately. The tempting default is "anybody the
	// provider authenticated", which on a provider with open registration is
	// the whole internet with an extra step.
	Allow []string `json:"allow"`
}

// authConfig reads it, with the defaults that make being unconfigured safe.
func (d *Daemon) authConfig() AuthConfig {
	var c AuthConfig
	raw, err := d.st.Setting(SettingAuth)
	if err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &c)
	}
	return c
}

// ready reports whether this configuration could actually let somebody in.
//
// Enabled is not enough. A configuration missing an issuer or a client would
// send every visitor to a provider that does not exist, and an empty allowlist
// would let nobody in after they got back, which reads as broken rather than
// as unconfigured.
func (c AuthConfig) ready() error {
	if !c.Enabled {
		return nil
	}
	if strings.TrimSpace(c.Issuer) == "" {
		return fmt.Errorf("a login needs an issuer: the provider's URL, such as a keycloak realm")
	}
	if strings.TrimSpace(c.ClientID) == "" {
		return fmt.Errorf("a login needs a client id, registered with that provider")
	}
	if strings.TrimSpace(c.Redirect) == "" {
		return fmt.Errorf("a login needs the address the provider sends people back to, and it " +
			"has to be the PUBLISHED one rather than localhost")
	}
	if len(c.Allow) == 0 {
		return fmt.Errorf("a login with nobody on the allow list would let nobody in. name at " +
			"least one email address or subject")
	}
	return nil
}

// allows reports whether this is somebody who may in.
//
// Matched case-insensitively on email or subject, because a provider may put
// either in the token and an operator typing their own address should not have
// to know which.
func (c AuthConfig) allows(subject, email string) bool {
	for _, a := range c.Allow {
		a = strings.TrimSpace(strings.ToLower(a))
		if a == "" {
			continue
		}
		if a == strings.ToLower(strings.TrimSpace(subject)) ||
			a == strings.ToLower(strings.TrimSpace(email)) {
			return true
		}
	}
	return false
}

// SaveAuth stores the configuration, refusing one that cannot work.
func (d *Daemon) SaveAuth(c AuthConfig) error {
	if err := c.ready(); err != nil {
		return err
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if err := d.st.SetSetting(SettingAuth, string(raw)); err != nil {
		return err
	}
	if c.Enabled {
		log.Printf("[atrium] the published board now asks who you are, via %s", c.Issuer)
	} else {
		log.Printf("[atrium] the published board no longer asks who you are")
	}
	return nil
}

// cookieKey returns the signing key, making one the first time.
func (d *Daemon) cookieKey() ([]byte, error) {
	raw, err := d.st.Setting(SettingAuthKey)
	if err == nil && strings.TrimSpace(raw) != "" {
		return base64.RawURLEncoding.DecodeString(raw)
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := d.st.SetSetting(SettingAuthKey, base64.RawURLEncoding.EncodeToString(key)); err != nil {
		return nil, err
	}
	return key, nil
}

// signSession makes a cookie value that says who somebody is and when it stops
// being true.
//
// SIGNED, NOT ENCRYPTED. There is nothing secret in it: the subject is not a
// credential and the expiry is public. What has to be impossible is editing it,
// which a MAC gives, and reading it costs an attacker nothing they did not
// already know about themselves.
func signSession(key []byte, subject string, until time.Time) string {
	body := base64.RawURLEncoding.EncodeToString(
		[]byte(fmt.Sprintf("%s|%d", subject, until.Unix())))
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// readSession checks one, returning the subject and whether it is good.
//
// `hmac.Equal` rather than `==`, because comparing MACs with a string compare
// leaks where they first differ, which is enough to forge one a byte at a time.
func readSession(key []byte, v string) (string, bool) {
	body, sig, ok := strings.Cut(v, ".")
	if !ok {
		return "", false
	}
	want := hmac.New(sha256.New, key)
	want.Write([]byte(body))
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(got, want.Sum(nil)) {
		return "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return "", false
	}
	subject, until, ok := strings.Cut(string(raw), "|")
	if !ok {
		return "", false
	}
	var unix int64
	if _, err := fmt.Sscanf(until, "%d", &unix); err != nil {
		return "", false
	}
	if time.Now().After(time.Unix(unix, 0)) {
		return "", false
	}
	return subject, true
}
