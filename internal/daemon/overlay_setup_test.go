package daemon

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// makeJWT builds a token with the claims a test needs. The signature is
// nonsense on purpose: atrium does not check one, and a test that supplied a
// real signature would be asserting something atrium never does.
func makeJWT(t *testing.T, iss, sub string, exp time.Time) string {
	t.Helper()
	claims := map[string]any{"iss": iss, "sub": sub}
	if !exp.IsZero() {
		claims["exp"] = exp.Unix()
	}
	body, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"none"}`)) + "." + enc(body) + ".not-a-signature"
}

// An enrolment token is one use. Reading which network it is for, before it is
// spent, is the difference between a useful form and one that fails at a
// controller.
func TestReadJWT(t *testing.T) {
	tok := makeJWT(t, "https://ctrl.example.com", "atrium", time.Now().Add(time.Hour))
	claims, err := readJWT(tok)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Issuer != "https://ctrl.example.com" {
		t.Fatalf("issuer came back as %q", claims.Issuer)
	}
	if claims.Expired {
		t.Fatal("a token good for another hour was reported expired")
	}
	if claims.Expires == "" {
		t.Fatal("an expiry was not reported")
	}
}

// Spent is worth saying before it is spent again.
func TestReadJWTNoticesAnExpiredToken(t *testing.T) {
	tok := makeJWT(t, "https://ctrl.example.com", "atrium", time.Now().Add(-time.Minute))
	claims, err := readJWT(tok)
	if err != nil {
		t.Fatal(err)
	}
	if !claims.Expired {
		t.Fatal("a token that expired a minute ago was reported good")
	}
}

// Something pasted into the wrong box should say so, rather than being sent to
// a controller to be refused there.
func TestReadJWTRefusesSomethingElse(t *testing.T) {
	for _, in := range []string{"", "not-a-token", "one.two", "abc.!!!.def"} {
		if _, err := readJWT(in); err == nil {
			t.Fatalf("%q was accepted as an enrolment token", in)
		}
	}
}

// A name arrives from a text box and becomes a path. Anything that is not
// plainly a name is dropped rather than escaped, so nothing can climb out of
// the directory atrium chose.
func TestSafeNameCannotEscape(t *testing.T) {
	cases := map[string]string{
		"atrium":        "atrium",
		"My Laptop":     "mylaptop",
		"../../etc/pwd": "etcpwd",
		"a/b\\c":        "abc",
		"":              "atrium",
		"...":           "atrium",
		"--":            "atrium",
		"C:/x":          "cx",
	}
	for in, want := range cases {
		if got := safeName(in); got != want {
			t.Errorf("safeName(%q) = %q, wanted %q", in, got, want)
		}
		if got := safeName(in); strings.ContainsAny(got, `/\.:`) {
			t.Errorf("safeName(%q) = %q, which is not a bare name", in, got)
		}
	}
}

func TestSafeNameIsBounded(t *testing.T) {
	if got := safeName(strings.Repeat("a", 500)); len(got) > 40 {
		t.Fatalf("a 500 character name produced %d characters", len(got))
	}
}

// Enabling needs the token, and saying which field is missing beats a command
// failing somewhere downstream.
func TestEnableZrokRefusesWithNoToken(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	if _, err := d.EnableZrok("   ", ""); err == nil {
		t.Fatal("enabling was accepted with no token")
	}
}

func TestEnrollZitiRefusesWithNoToken(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	if _, _, err := d.EnrollZiti("", "atrium"); err == nil {
		t.Fatal("enrolling was accepted with no token")
	}
}

// An expired token is refused here, with its date, rather than by a controller
// with something less useful.
func TestEnrollZitiRefusesAnExpiredToken(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	tok := makeJWT(t, "https://ctrl.example.com", "atrium", time.Now().Add(-time.Hour))
	_, _, err := d.EnrollZiti(tok, "atrium")
	if err == nil {
		t.Fatal("an expired token was sent to a controller anyway")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Fatalf("the refusal does not say the token expired: %v", err)
	}
}

// Nothing set up is the state a fresh machine is in, and it has to read as
// that rather than as an error.
func TestZrokEnvReportsNothingWhenThereIsNothing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	if env := zrokEnv(); env.Enabled {
		t.Fatalf("an empty home reported an enabled environment: %+v", env)
	}
}

// The environment file is the same thing the zrok CLI checks, so its presence
// is the whole answer.
func TestZrokEnvReadsAnEnabledEnvironment(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	dir := filepath.Join(home, ".zrok2")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// The real key is `zrok_token`. Written the way zrok writes it, so a
	// change to that tag fails here rather than silently reporting every
	// environment as having no token.
	body := `{"zrok_token":"tok123","api_endpoint":"https://api.zrok.io","ziti_identity":"abc"}`
	if err := os.WriteFile(filepath.Join(dir, "environment.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	env := zrokEnv()
	if !env.Enabled {
		t.Fatal("an environment that is there was reported as missing")
	}
	if env.ApiEndpoint != "https://api.zrok.io" {
		t.Fatalf("the endpoint came back as %q", env.ApiEndpoint)
	}
	if !env.HasAccountToken {
		t.Fatal("a token that is present was reported absent")
	}
}

// The token itself must never leave the daemon. The board has no use for it
// and every reason not to hold one.
func TestZrokEnvNeverCarriesTheToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, ".zrok2")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	secret := "super-secret-account-token"
	body := `{"zrok_token":"` + secret + `","api_endpoint":"https://api.zrok.io"}`
	if err := os.WriteFile(filepath.Join(dir, "environment.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := json.Marshal(zrokEnv())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), secret) {
		t.Fatalf("the account token is in what the board receives: %s", out)
	}
}

// A path pointing at nothing is a state worth telling apart from never having
// configured one, because the fix is different.
func TestZitiEnrolmentTellsMissingFromUnset(t *testing.T) {
	if got := zitiEnrolment(""); got.Path != "" || got.Present {
		t.Fatalf("an unset identity reported as %+v", got)
	}
	got := zitiEnrolment(filepath.Join(t.TempDir(), "nope.json"))
	if got.Present {
		t.Fatal("a file that is not there was reported present")
	}
	if got.Path == "" {
		t.Fatal("the configured path was dropped, so the board cannot say which file is missing")
	}
	if got.Err != "" {
		t.Fatalf("a missing file was reported as an error: %s", got.Err)
	}
}

// The controller is read out so the board can say which network this is. The
// key material beside it is deliberately not a field.
func TestZitiEnrolmentReadsTheControllerAndNotTheKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "id.json")
	key := "PRIVATE-KEY-MATERIAL"
	body := `{"ztAPI":"https://ctrl.example.com:1280","id":{"key":"` + key + `"}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	got := zitiEnrolment(path)
	if !got.Present {
		t.Fatal("an identity that is there was reported missing")
	}
	if got.Controller != "https://ctrl.example.com:1280" {
		t.Fatalf("the controller came back as %q", got.Controller)
	}
	out, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), key) {
		t.Fatalf("key material is in what the board receives: %s", out)
	}
}

// The description is optional and zrok has its own default, so an empty one
// must not become an empty flag.
func TestZrokEnableArgs(t *testing.T) {
	args := zrokEnableArgs("tok123", "")
	got := strings.Join(args, " ")
	if !strings.Contains(got, "enable tok123") {
		t.Fatalf("the token is not what it enables with: %q", got)
	}
	if !strings.Contains(got, "--headless") {
		t.Fatalf("enable would draw a spinner into a pipe: %q", got)
	}
	if strings.Contains(got, "--description") {
		t.Fatalf("an empty description became a flag: %q", got)
	}
	if named := strings.Join(zrokEnableArgs("tok", "my laptop"), " "); !strings.Contains(named, "--description my laptop") {
		t.Fatalf("a description was dropped: %q", named)
	}
}
