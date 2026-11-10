package roomstats

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// A runner's login, read for its expiry and for whether one exists, and nothing else. The credential files hold
// tokens, so every field below that could carry one is decoded into `seen`, which keeps a yes or no and drops the
// bytes. No token is stored in a variable, logged, sent or put in the snapshot. See f-runner-login-expiry.
const (
	LoginIn      = "in"
	LoginOut     = "out"
	LoginUnknown = "unknown"
	// LoginWarn is how far ahead of an expiry the room starts to say so.
	LoginWarn      = 72 * time.Hour
	loginCacheFor  = 5 * time.Minute
	keychainClaude = "Claude Code-credentials"
	keychainMax    = 5 * time.Second
	// securityNotFound is `security`'s exit code for an item that is not in the keychain.
	securityNotFound = 44
)

// Login is what one runner's credential store says. Renews is true when it holds a refresh token, in which case the
// runner mints a new access token itself and ExpiresAt is only the current one's.
type Login struct {
	State     string
	ExpiresAt time.Time
	Renews    bool
}

// seen is a JSON value reduced to whether it was there and not empty.
type seen bool

func (s *seen) UnmarshalJSON(b []byte) error {
	t := bytes.TrimSpace(b)
	*s = len(t) > 0 && string(t) != "null" && string(t) != `""`
	return nil
}

// LoginFiles is where a runner keeps its login. Empty fields take the real defaults.
type LoginFiles struct {
	Home string
	// ClaudeDir is CLAUDE_CONFIG_DIR and CodexDir is CODEX_HOME.
	ClaudeDir, CodexDir string
	GOOS                string
	// Keychain returns the macOS keychain item's bytes. The caller treats them as a secret.
	Keychain func() ([]byte, error)
}

// RealLoginFiles reads the environment and the operating system.
func RealLoginFiles() LoginFiles {
	h, _ := os.UserHomeDir()
	return LoginFiles{Home: h, ClaudeDir: os.Getenv("CLAUDE_CONFIG_DIR"), CodexDir: os.Getenv("CODEX_HOME"),
		GOOS: runtime.GOOS, Keychain: readKeychain}
}

// errKeychainMissing is an item that is not there, as against a keychain that could not be asked.
var errKeychainMissing = errors.New("keychain item not found")

func readKeychain() ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), keychainMax)
	defer cancel()
	out, err := exec.CommandContext(ctx, "security", "find-generic-password", "-s", keychainClaude, "-w").Output()
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == securityNotFound {
		return nil, errKeychainMissing
	}
	return out, err
}

// ReadLogin answers for a reader, as a runner row's login_read names it. A reader that is not one of these is the zero
// Login.
func (f LoginFiles) ReadLogin(reader string) Login {
	switch reader {
	case "claude":
		return f.claude()
	case "codex":
		return f.codex()
	}
	return Login{}
}

type claudeCreds struct {
	OAuth *struct {
		Access    seen  `json:"accessToken"`
		Refresh   seen  `json:"refreshToken"`
		ExpiresAt int64 `json:"expiresAt"`
	} `json:"claudeAiOauth"`
}

func parseClaude(b []byte) (Login, bool) {
	var c claudeCreds
	if json.Unmarshal(b, &c) != nil || c.OAuth == nil || !bool(c.OAuth.Access) {
		return Login{}, false
	}
	l := Login{State: LoginIn, Renews: bool(c.OAuth.Refresh)}
	if c.OAuth.ExpiresAt > 0 {
		l.ExpiresAt = time.UnixMilli(c.OAuth.ExpiresAt)
	}
	return l, true
}

func (f LoginFiles) claude() Login {
	dir := f.ClaudeDir
	if dir == "" {
		dir = filepath.Join(f.Home, ".claude")
	}
	if b, err := os.ReadFile(filepath.Join(dir, ".credentials.json")); err == nil {
		if l, ok := parseClaude(b); ok {
			return l
		}
	}
	// A custom config dir keeps its keychain item under a name this does not derive, so only the file speaks for it.
	if f.GOOS != "darwin" || f.ClaudeDir != "" || f.Keychain == nil {
		return Login{State: LoginOut}
	}
	b, err := f.Keychain()
	switch {
	case errors.Is(err, errKeychainMissing):
		return Login{State: LoginOut}
	case err != nil:
		return Login{State: LoginUnknown}
	}
	if l, ok := parseClaude(b); ok {
		return l
	}
	return Login{State: LoginOut}
}

type codexCreds struct {
	APIKey seen `json:"OPENAI_API_KEY"`
	Tokens *struct {
		Access  jwtExp `json:"access_token"`
		Refresh seen   `json:"refresh_token"`
	} `json:"tokens"`
}

// jwtExp is the exp claim of a JWT and nothing of the token itself.
type jwtExp struct {
	present bool
	exp     int64
}

func (j *jwtExp) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) != nil || s == "" {
		return nil
	}
	j.present = true
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return nil
	}
	p, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	var c struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(p, &c) == nil {
		j.exp = c.Exp
	}
	return nil
}

func (f LoginFiles) codex() Login {
	dir := f.CodexDir
	if dir == "" {
		dir = filepath.Join(f.Home, ".codex")
	}
	b, err := os.ReadFile(filepath.Join(dir, "auth.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Login{State: LoginOut}
		}
		return Login{State: LoginUnknown}
	}
	var c codexCreds
	if json.Unmarshal(b, &c) != nil {
		return Login{State: LoginUnknown}
	}
	if c.Tokens != nil && c.Tokens.Access.present {
		l := Login{State: LoginIn, Renews: bool(c.Tokens.Refresh)}
		if c.Tokens.Access.exp > 0 {
			l.ExpiresAt = time.Unix(c.Tokens.Access.exp, 0)
		}
		return l
	}
	if c.APIKey {
		return Login{State: LoginIn, Renews: true}
	}
	return Login{State: LoginOut}
}

// LoginCache keeps each kind's answer for loginCacheFor, since the sampler asks every ten seconds and the keychain
// is a process to spawn.
type LoginCache struct {
	Files LoginFiles
	Now   func() time.Time
	mu    sync.Mutex
	at    map[string]time.Time
	got   map[string]Login
}

func (c *LoginCache) Read(kind string) Login {
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if t, ok := c.at[kind]; ok && now().Sub(t) < loginCacheFor {
		return c.got[kind]
	}
	if c.at == nil {
		c.at, c.got = map[string]time.Time{}, map[string]Login{}
	}
	l := c.Files.ReadLogin(kind)
	c.at[kind], c.got[kind] = now(), l
	return l
}

// LoginDue is a runner whose login the operator has to renew: State is `out` or `expiring`.
type LoginDue struct {
	Kind      string `json:"kind"`
	State     string `json:"state"`
	ExpiresAt string `json:"expires_at,omitempty"`
	Fix       string `json:"fix,omitempty"`
}

// loginDue is whether r needs a login from a person at now.
func loginDue(r Runner, now time.Time) (LoginDue, bool) {
	if !r.Enabled || !r.Resolves {
		return LoginDue{}, false
	}
	switch r.Login {
	case LoginOut:
		return LoginDue{Kind: r.Kind, State: "out", Fix: r.LoginFix}, true
	case LoginIn:
		if r.LoginRenews || r.LoginExpires == "" {
			return LoginDue{}, false
		}
		t, err := time.Parse(time.RFC3339, r.LoginExpires)
		if err == nil && t.Sub(now) < LoginWarn {
			return LoginDue{Kind: r.Kind, State: "expiring", ExpiresAt: r.LoginExpires, Fix: r.LoginFix}, true
		}
	}
	return LoginDue{}, false
}
