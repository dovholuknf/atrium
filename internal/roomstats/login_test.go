package roomstats

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const fakeSecret = "FAKE-NOT-A-TOKEN"

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func num(n int64) string { return strconv.FormatInt(n, 10) }

func claudeBody(exp int64, refresh string) string {
	return `{"claudeAiOauth":{"accessToken":"` + fakeSecret + `","refreshToken":"` + refresh + `","expiresAt":` + num(exp) + `}}`
}

func TestClaudeLoginFromFileAndConfigDir(t *testing.T) {
	home := t.TempDir()
	exp := time.Now().Add(48 * time.Hour).UnixMilli()
	f := LoginFiles{Home: home, GOOS: "linux"}
	if l := f.ReadLogin("claude"); l.State != LoginOut {
		t.Fatalf("no file: %+v", l)
	}
	write(t, filepath.Join(home, ".claude"), ".credentials.json", claudeBody(exp, ""))
	l := f.ReadLogin("claude")
	if l.State != LoginIn || l.Renews || l.ExpiresAt.UnixMilli() != exp {
		t.Fatalf("home login: %+v", l)
	}
	custom := t.TempDir()
	write(t, custom, ".credentials.json", claudeBody(exp, fakeSecret))
	if l := (LoginFiles{Home: home, ClaudeDir: custom}).ReadLogin("claude"); !l.Renews {
		t.Fatalf("CLAUDE_CONFIG_DIR was not read: %+v", l)
	}
	write(t, filepath.Join(home, ".claude"), ".credentials.json", `{"claudeAiOauth":{"accessToken":""}}`)
	if l := f.ReadLogin("claude"); l.State != LoginOut {
		t.Fatalf("an empty token counts as a login: %+v", l)
	}
}

func TestClaudeLoginFromKeychainOnlyOnMac(t *testing.T) {
	item := []byte(claudeBody(time.Now().Add(time.Hour).UnixMilli(), ""))
	keep := func() ([]byte, error) { return item, nil }
	cases := []struct {
		name string
		f    LoginFiles
		want string
	}{
		{"mac item", LoginFiles{Home: t.TempDir(), GOOS: "darwin", Keychain: keep}, LoginIn},
		{"mac no item", LoginFiles{Home: t.TempDir(), GOOS: "darwin", Keychain: func() ([]byte, error) { return nil, errKeychainMissing }}, LoginOut},
		{"mac keychain locked", LoginFiles{Home: t.TempDir(), GOOS: "darwin", Keychain: func() ([]byte, error) { return nil, errors.New("locked") }}, LoginUnknown},
		{"linux ignores it", LoginFiles{Home: t.TempDir(), GOOS: "linux", Keychain: keep}, LoginOut},
		{"config dir ignores it", LoginFiles{Home: t.TempDir(), ClaudeDir: t.TempDir(), GOOS: "darwin", Keychain: keep}, LoginOut},
	}
	for _, c := range cases {
		if got := c.f.ReadLogin("claude").State; got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func jwt(exp int64) string {
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"none"}`)) + "." + enc([]byte(`{"exp":`+num(exp)+`}`)) + ".sig"
}

func TestCodexLogin(t *testing.T) {
	home := t.TempDir()
	f := LoginFiles{Home: home}
	if l := f.ReadLogin("codex"); l.State != LoginOut {
		t.Fatalf("no file: %+v", l)
	}
	exp := time.Now().Add(24 * time.Hour).Unix()
	write(t, filepath.Join(home, ".codex"), "auth.json",
		`{"OPENAI_API_KEY":null,"tokens":{"access_token":"`+jwt(exp)+`","refresh_token":"`+fakeSecret+`"}}`)
	l := f.ReadLogin("codex")
	if l.State != LoginIn || !l.Renews || l.ExpiresAt.Unix() != exp {
		t.Fatalf("chatgpt login: %+v", l)
	}
	write(t, filepath.Join(home, ".codex"), "auth.json", `{"OPENAI_API_KEY":"`+fakeSecret+`"}`)
	if l := f.ReadLogin("codex"); l.State != LoginIn || !l.ExpiresAt.IsZero() {
		t.Fatalf("api key login: %+v", l)
	}
	write(t, filepath.Join(home, ".codex"), "auth.json", `{"OPENAI_API_KEY":null,"tokens":null}`)
	if l := f.ReadLogin("codex"); l.State != LoginOut {
		t.Fatalf("empty auth: %+v", l)
	}
	write(t, filepath.Join(home, ".codex"), "auth.json", `not json`)
	if l := f.ReadLogin("codex"); l.State != LoginUnknown {
		t.Fatalf("garbage: %+v", l)
	}
	if l := f.ReadLogin("ollama"); l != (Login{}) {
		t.Fatalf("a kind with no store: %+v", l)
	}
}

func TestLoginCacheAsksOncePerWindow(t *testing.T) {
	asked := 0
	now := time.Now()
	c := &LoginCache{Files: LoginFiles{GOOS: "darwin", Keychain: func() ([]byte, error) { asked++; return nil, errKeychainMissing }},
		Now: func() time.Time { return now }}
	c.Read("claude")
	c.Read("claude")
	now = now.Add(loginCacheFor + time.Second)
	c.Read("claude")
	if asked != 2 {
		t.Fatalf("keychain asked %d times", asked)
	}
}

func TestLoginDueAndAlert(t *testing.T) {
	now := time.Now()
	soon := now.Add(48 * time.Hour).UTC().Format(time.RFC3339)
	far := now.Add(10 * 24 * time.Hour).UTC().Format(time.RFC3339)
	runners := []Runner{
		{Kind: "claude", Resolves: true, Enabled: true, Login: LoginIn, LoginExpires: soon},
		{Kind: "codex", Resolves: true, Enabled: true, Login: LoginOut, LoginFix: "codex login --device-auth"},
		{Kind: "gemini", Resolves: true, Enabled: false, Login: LoginOut},
	}
	s := New(Sources{Clock: &stepClock{now: now}, Runners: func() ([]Runner, error) { return runners, nil }})
	s.Tick()
	a := s.snap.Alert
	if a == nil || len(a.LoginsDue) != 2 || a.LoginsDue[0].State != "expiring" || a.LoginsDue[1].Kind != "codex" || a.LoginsDue[1].State != "out" ||
		a.LoginsDue[0].Fix != "" || a.LoginsDue[1].Fix != "codex login --device-auth" {
		t.Fatalf("alert = %+v", a)
	}
	runners[0].LoginExpires, runners[1].Login = far, LoginIn
	s.Tick()
	if s.snap.Alert != nil {
		t.Fatalf("alert stayed: %+v", s.snap.Alert)
	}
	runners[0].LoginExpires, runners[0].LoginRenews = soon, true
	s.Tick()
	if s.snap.Alert != nil {
		t.Fatalf("a login that renews itself alerted: %+v", s.snap.Alert)
	}
}

func TestSnapshotNeverCarriesTheToken(t *testing.T) {
	home := t.TempDir()
	write(t, filepath.Join(home, ".claude"), ".credentials.json", claudeBody(time.Now().UnixMilli(), fakeSecret))
	l := (LoginFiles{Home: home}).ReadLogin("claude")
	s := New(Sources{Runners: func() ([]Runner, error) {
		return []Runner{{Kind: "claude", Resolves: true, Enabled: true, Login: l.State, LoginRenews: l.Renews}}, nil
	}})
	s.Tick()
	if strings.Contains(string(s.Bytes()), fakeSecret) {
		t.Fatal("the snapshot carried the credential")
	}
}
