package daemon

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Hardening the published board's login: a public share cannot be left with no
// login, guessing is limited, and a session ends when what it was issued for does.

func guardedBoard(d *Daemon) http.Handler {
	return d.authGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("the board"))
	}))
}

func basicRequest(user, pass, remote string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = remote
	r.SetBasicAuth(user, pass)
	return r
}

func sessionCookieOf(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == authCookie && c.Value != "" {
			return c
		}
	}
	return nil
}

// markPublicShareRunning pretends a public zrok share is up.
func markPublicShareRunning(t *testing.T, d *Daemon) {
	t.Helper()
	if err := d.saveOverlayConfig(SettingOverlayZrok, ZrokConfig{Mode: "public"}); err != nil {
		t.Fatal(err)
	}
	n := d.nat(OverlayZrok)
	n.mu.Lock()
	n.srv = &http.Server{}
	n.mu.Unlock()
	t.Cleanup(func() {
		n.mu.Lock()
		n.srv = nil
		n.mu.Unlock()
	})
}

// POINT 1. Turning the login off under a running public share is refused.
func TestTheLoginCannotBeTurnedOffUnderAPublicShare(t *testing.T) {
	d := testDaemon(t)
	if err := d.SaveAuth(basicConfig(t, "clint", "hunter2")); err != nil {
		t.Fatal(err)
	}
	markPublicShareRunning(t, d)

	if err := d.SaveAuth(AuthConfig{}); err == nil {
		t.Fatal("the login was turned off under a running public share")
	}
	off := basicConfig(t, "clint", "hunter2")
	off.Enabled = false
	if err := d.SaveAuth(off); err == nil {
		t.Fatal("the login was disabled under a running public share")
	}
	if !d.authConfig().Enabled {
		t.Fatal("a refused save still changed the stored login")
	}
	// Changing it to another working login is not weakening it.
	if err := d.SaveAuth(basicConfig(t, "clint", "a new password")); err != nil {
		t.Fatalf("a working login was refused: %v", err)
	}
}

func TestTheLoginCanBeTurnedOffWhenNoPublicShareRuns(t *testing.T) {
	d := testDaemon(t)
	if err := d.SaveAuth(basicConfig(t, "clint", "hunter2")); err != nil {
		t.Fatal(err)
	}
	if err := d.SaveAuth(AuthConfig{}); err != nil {
		t.Fatalf("turning the login off with no share was refused: %v", err)
	}
}

// POINT 2. A source that keeps guessing wrong waits, and the wait comes before
// any scrypt.
func TestRepeatedWrongPasswordsMakeASourceWait(t *testing.T) {
	d := testDaemon(t)
	if err := d.SaveAuth(basicConfig(t, "clint", "hunter2")); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	d.authLim.now = func() time.Time { return now }
	h := guardedBoard(d)

	for i := 0; i < authFreeFails; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, basicRequest("clint", "wrong", "198.51.100.7:4000"))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("guess %d answered %d", i, rec.Code)
		}
	}
	// The right password does not get through while the source is waiting.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, basicRequest("clint", "hunter2", "198.51.100.7:4001"))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("a source that guessed %d times answered %d, wanted 429", authFreeFails, rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("no Retry-After on a refusal for waiting")
	}
	// Another source is not affected.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, basicRequest("clint", "hunter2", "198.51.100.8:4000"))
	if rec.Code != http.StatusOK {
		t.Fatalf("an innocent source answered %d", rec.Code)
	}
	// And the wait ends.
	now = now.Add(authLockBase + time.Second)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, basicRequest("clint", "hunter2", "198.51.100.7:4002"))
	if rec.Code != http.StatusOK {
		t.Fatalf("after the wait the right password answered %d", rec.Code)
	}
}

// Behind a share's proxy everybody is loopback, so the address the proxy
// appended is what counts, and the part a caller wrote is ignored.
func TestTheSourceBehindAProxyIsTheLastForwardedAddress(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "127.0.0.1:5000"
	r.Header.Set("X-Forwarded-For", "1.1.1.1, 203.0.113.9")
	if got := authSource(r); got != "203.0.113.9" {
		t.Fatalf("source %q", got)
	}
	r.RemoteAddr = "198.51.100.1:5000"
	if got := authSource(r); got != "198.51.100.1" {
		t.Fatalf("a header from a non-proxy was believed: %q", got)
	}
}

// Only so many password checks run at once.
func TestPasswordChecksAreBounded(t *testing.T) {
	var l authLimiter
	for i := 0; i < authScryptSlots; i++ {
		if !l.acquire() {
			t.Fatalf("slot %d was refused", i)
		}
	}
	if l.acquire() {
		t.Fatal("more checks ran at once than the bound")
	}
	l.release()
	if !l.acquire() {
		t.Fatal("a released slot was not reusable")
	}
}

func TestAFullBoardRefusesRatherThanQueues(t *testing.T) {
	d := testDaemon(t)
	if err := d.SaveAuth(basicConfig(t, "clint", "hunter2")); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < authScryptSlots; i++ {
		d.authLim.acquire()
	}
	rec := httptest.NewRecorder()
	guardedBoard(d).ServeHTTP(rec, basicRequest("clint", "hunter2", "198.51.100.7:4000"))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("a busy board answered %d, wanted 503", rec.Code)
	}
}

func TestTheFailureTableIsBounded(t *testing.T) {
	var l authLimiter
	for i := 0; i < authFailsMost+50; i++ {
		l.fail("src-" + strings.Repeat("x", i%7) + string(rune('a'+i%26)) + time.Duration(i).String())
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.fails) > authFailsMost {
		t.Fatalf("the table grew to %d", len(l.fails))
	}
}

func TestTheWaitDoublesAndIsCapped(t *testing.T) {
	var l authLimiter
	now := time.Now()
	l.now = func() time.Time { return now }
	for i := 0; i < authFreeFails; i++ {
		l.fail("a")
	}
	first := l.blocked("a")
	l.fail("a")
	if second := l.blocked("a"); second <= first {
		t.Fatalf("the wait did not grow: %v then %v", first, second)
	}
	for i := 0; i < 40; i++ {
		l.fail("a")
	}
	if got := l.blocked("a"); got > authLockMax {
		t.Fatalf("the wait %v passed the cap", got)
	}
}

// POINT 3. A password change ends sessions issued before it.
func TestAPasswordChangeEndsEarlierSessions(t *testing.T) {
	d := testDaemon(t)
	if err := d.SaveAuth(basicConfig(t, "clint", "hunter2")); err != nil {
		t.Fatal(err)
	}
	h := guardedBoard(d)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, basicRequest("clint", "hunter2", "198.51.100.7:4000"))
	c := sessionCookieOf(rec)
	if c == nil {
		t.Fatal("no session was issued")
	}
	withCookie := func() int {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(c)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, r)
		return rr.Code
	}
	if got := withCookie(); got != http.StatusOK {
		t.Fatalf("a fresh session answered %d", got)
	}
	if err := d.SaveAuth(basicConfig(t, "clint", "a different password")); err != nil {
		t.Fatal(err)
	}
	if got := withCookie(); got == http.StatusOK {
		t.Fatal("a session issued before a password change still works")
	}
}

// Saving the same login again does not log everybody out.
func TestSavingTheSameLoginKeepsSessions(t *testing.T) {
	d := testDaemon(t)
	cfg := basicConfig(t, "clint", "hunter2")
	if err := d.SaveAuth(cfg); err != nil {
		t.Fatal(err)
	}
	before := d.sessionGen()
	if err := d.SaveAuth(cfg); err != nil {
		t.Fatal(err)
	}
	if d.sessionGen() != before {
		t.Fatal("an unchanged save bumped the generation")
	}
}

// Removing somebody from the allowlist ends their session, and the guard
// re-checks the subject, so a leftover cookie is not enough.
func TestAnAllowlistRemovalEndsThatSession(t *testing.T) {
	d := testDaemon(t)
	oidc := func(allow ...string) AuthConfig {
		return AuthConfig{
			Enabled: true, Issuer: "https://idp.example/realms/x", ClientID: "atrium",
			Redirect: "https://board.example/auth/callback", Allow: allow,
		}
	}
	if err := d.SaveAuth(oidc("a@example.com", "b@example.com")); err != nil {
		t.Fatal(err)
	}
	key, _ := d.cookieKey()
	mine := func(sub, mail string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(&http.Cookie{Name: authCookie, Value: signSession(key,
			session{Subject: sub, Email: mail, Gen: d.sessionGen()}, time.Now().Add(time.Hour))})
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

// Logout is a POST, and with a password login the browser's cached credentials
// do not sign it straight back in.
func TestLogoutIsAPostAndASignedOutBrowserIsNotSignedBackIn(t *testing.T) {
	d := testDaemon(t)
	if err := d.SaveAuth(basicConfig(t, "clint", "hunter2")); err != nil {
		t.Fatal(err)
	}
	h := guardedBoard(d)

	rec := httptest.NewRecorder()
	req := basicRequest("clint", "hunter2", "198.51.100.7:4000")
	req.URL.Path = authPrefix + "logout"
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("a GET logout answered %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = basicRequest("clint", "hunter2", "198.51.100.7:4000")
	req.Method, req.URL.Path = http.MethodPost, authPrefix+"logout"
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("logout answered %d", rec.Code)
	}
	var out *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == authOutCookie {
			out = c
		}
	}
	if out == nil {
		t.Fatal("logout left no mark, so cached credentials sign straight back in")
	}

	// The browser comes back with the same cached header.
	back := basicRequest("clint", "hunter2", "198.51.100.7:4000")
	back.AddCookie(out)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, back)
	if rec.Code != http.StatusUnauthorized || rec.Header().Get("WWW-Authenticate") == "" {
		t.Fatalf("cached credentials after logout answered %d", rec.Code)
	}
	// Typing the password at the new prompt works, since the mark was cleared.
	again := basicRequest("clint", "hunter2", "198.51.100.7:4000")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, again)
	if rec.Code != http.StatusOK {
		t.Fatalf("signing in again answered %d", rec.Code)
	}
}

// Many parallel guesses do not run more than the bound of checks at once.
func TestParallelGuessesStayWithinTheBound(t *testing.T) {
	d := testDaemon(t)
	if err := d.SaveAuth(basicConfig(t, "clint", "hunter2")); err != nil {
		t.Fatal(err)
	}
	h := guardedBoard(d)
	var wg sync.WaitGroup
	var mu sync.Mutex
	codes := map[int]int{}
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, basicRequest("clint", "nope",
				"198.51.100."+string(rune('1'+i%9))+":4000"))
			mu.Lock()
			codes[rec.Code]++
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	if codes[http.StatusOK] != 0 {
		t.Fatalf("a wrong password got in: %v", codes)
	}
}
