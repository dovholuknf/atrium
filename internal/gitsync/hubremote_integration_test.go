//go:build integration

package gitsync

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// hubRT sends what the forwarder forwards to the fixture's hub as the room sg4, and records the headers it sent.
type hubRT struct {
	x *recvFix

	mu   sync.Mutex
	hdrs []http.Header
	n    int
}

func (h *hubRT) RoundTrip(r *http.Request) (*http.Response, error) {
	h.mu.Lock()
	h.n++
	h.hdrs = append(h.hdrs, r.Header.Clone())
	h.mu.Unlock()
	u, _ := url.Parse(h.x.srv.URL)
	out := r.Clone(r.Context())
	out.URL.Scheme, out.URL.Host, out.Host = u.Scheme, u.Host, u.Host
	out.Header.Set("X-Test-Caller", "room:sg4")
	return http.DefaultTransport.RoundTrip(out)
}

func (h *hubRT) count() int { h.mu.Lock(); defer h.mu.Unlock(); return h.n }

type remoteFix struct {
	*recvFix
	rt    *hubRT
	cards *CardTokens
	srv   *httptest.Server
	push  string
	base  string // the forwarder's scope: http://127.0.0.1:<port>/git/
}

func newRemote(t *testing.T) *remoteFix {
	t.Helper()
	x := newRecv(t)
	f := &remoteFix{recvFix: x, rt: &hubRT{x: x}, cards: &CardTokens{}, push: "hub"}
	hr := &HubForwarder{
		Auth:      f.cards,
		Push:      func() string { return f.push },
		Transport: func() (http.RoundTripper, error) { return f.rt, nil },
	}
	f.srv = httptest.NewServer(hr)
	t.Cleanup(f.srv.Close)
	f.base = f.srv.URL + "/git/"
	return f
}

func (f *remoteFix) repoURL(name string) string { return f.srv.URL + StorePrefix + name + ".git" }

// cardEnv is a card's git environment: its token, scoped to the forwarder.
func (f *remoteFix) cardEnv(card string) []string {
	f.t.Helper()
	tok, err := f.cards.Mint(card)
	if err != nil {
		f.t.Fatal(err)
	}
	return append(PushEnv(f.base, tok, 0), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
}

func (f *remoteFix) git(env []string, dir string, args ...string) (string, error) {
	f.t.Helper()
	out, err := Default.GitEnv(bg, dir, env, args...)
	if ge, ok := err.(*Error); ok {
		return out + ge.Stderr, err
	}
	return out, err
}

func TestInACardEnvGitPushHubLandsWithTheCardInThePushLog(t *testing.T) {
	f := newRemote(t)
	f.seedMain(hubRepo)
	sha := f.branch("fix/x", "x.txt")
	git(t, f.work, "remote", "add", "hub", f.repoURL(hubRepo))
	out, err := f.git(f.cardEnv("C1"), f.work, "push", "hub", "fix/x")
	f.must(out, err)
	if got := f.refOn(hubRepo, "refs/heads/fix/x"); got != sha {
		t.Fatalf("fix/x is %q on the hub, want %q", got, sha)
	}
	rows := f.pushRows()
	r := rows[len(rows)-1]
	if r.Room != "sg4" || r.Card != "C1" || r.Ref != "refs/heads/fix/x" {
		t.Fatalf("the push log has %+v, want sg4's C1", r)
	}
	// the token never reached the hub, and the card did, once, as the forwarder set it.
	for _, h := range f.rt.hdrs {
		if h.Get(HeaderCardToken) != "" {
			t.Fatalf("the card's token went to the hub: %v", h)
		}
		if h.Get(HeaderCard) != "C1" || h.Get(HeaderChain) != "C1" {
			t.Fatalf("the hub was told %q / %q", h.Get(HeaderCard), h.Get(HeaderChain))
		}
	}
}

func TestAProcessWithNoCardTokenIsRefusedByTheForwarderBeforeTheHub(t *testing.T) {
	f := newRemote(t)
	f.seedMain(hubRepo)
	f.branch("fix/x", "x.txt")
	git(t, f.work, "remote", "add", "hub", f.repoURL(hubRepo))
	base := f.rt.count()
	out, err := f.git([]string{"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1"}, f.work, "push", "hub", "fix/x")
	if err == nil {
		t.Fatalf("a push with no token worked:\n%s", out)
	}
	if !strings.Contains(out, "atrium: this room's hub remote answers a card with its atrium token") {
		t.Fatalf("not the forwarder's own sentence:\n%s", out)
	}
	if f.rt.count() != base {
		t.Fatal("a request with no token reached the hub")
	}
	if f.refOn(hubRepo, "refs/heads/fix/x") != "" {
		t.Fatal("the branch landed")
	}
	// A fetch with no token is refused the same way.
	if out, err := f.git([]string{"GIT_CONFIG_GLOBAL=/dev/null"}, f.work, "ls-remote", "hub"); err == nil ||
		!strings.Contains(out, "atrium token") {
		t.Fatalf("ls-remote: %v\n%s", err, out)
	}
	if f.rt.count() != base {
		t.Fatal("a fetch with no token reached the hub")
	}
}

func TestAWrongRevokedOrOtherCardsTokenIsRefused(t *testing.T) {
	f := newRemote(t)
	f.seedMain(hubRepo)
	git(t, f.work, "remote", "add", "hub", f.repoURL(hubRepo))
	ok := f.cardEnv("C1")
	if out, err := f.git(ok, f.work, "ls-remote", "hub"); err != nil {
		t.Fatalf("a good token: %v\n%s", err, out)
	}
	tok, _ := f.cards.Mint("C2")
	bad := []string{
		strings.Replace(tok, "C2", "C1", 1), // C2's secret under C1's name
		"C1." + strings.Repeat("0", 64),     // a made-up secret
		"C9." + strings.Repeat("0", 64),     // a card with none
		"garbage", "C1.", ".", "C1.zz",
	}
	for _, b := range bad {
		env := append(PushEnv(f.base, b, 0), "GIT_CONFIG_GLOBAL=/dev/null")
		if out, err := f.git(env, f.work, "ls-remote", "hub"); err == nil || !strings.Contains(out, "atrium token") {
			t.Errorf("token %q was accepted: %v\n%s", b, err, out)
		}
	}
	f.cards.Revoke("C1")
	if out, err := f.git(ok, f.work, "ls-remote", "hub"); err == nil {
		t.Fatalf("a revoked token worked:\n%s", out)
	}
	// A card that is no longer live loses its token without a revoke.
	f.cards.Live = func(string) bool { return false }
	if _, ok := f.cards.Check(tok); ok {
		t.Fatal("a token for a card that is not live was accepted")
	}
}

func TestGitPushNoneRefusesReceivePackAndStillFetches(t *testing.T) {
	f := newRemote(t)
	f.seedMain(hubRepo)
	f.branch("fix/x", "x.txt")
	git(t, f.work, "remote", "add", "hub", f.repoURL(hubRepo))
	f.push = "none"
	env := f.cardEnv("C1")
	out, err := f.git(env, f.work, "push", "hub", "fix/x")
	if err == nil || !strings.Contains(out, "git.push is none") {
		t.Fatalf("push with git.push none: %v\n%s", err, out)
	}
	if f.refOn(hubRepo, "refs/heads/fix/x") != "" {
		t.Fatal("the branch landed with git.push none")
	}
	// The POST too, not only the advertisement.
	tok, _ := f.cards.Mint("C1")
	req, _ := http.NewRequest(http.MethodPost, f.repoURL(hubRepo)+"/git-receive-pack", strings.NewReader("0000"))
	req.Header.Set("Content-Type", "application/x-git-receive-pack-request")
	req.Header.Set(HeaderCardToken, tok)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 512)
	n, _ := res.Body.Read(b)
	res.Body.Close()
	if !strings.Contains(string(b[:n]), "git.push is none") {
		t.Fatalf("POST receive-pack with git.push none: %d %q", res.StatusCode, b[:n])
	}
	before := f.rt.count()
	if out, err := f.git(f.cardEnv("C1"), f.work, "ls-remote", "hub"); err != nil || !strings.Contains(out, "refs/heads/main") {
		t.Fatalf("fetch with git.push none: %v\n%s", err, out)
	}
	if f.rt.count() == before {
		t.Fatal("the fetch did not reach the hub")
	}
}

func TestAClientsOwnCardHeadersNeverReachTheHub(t *testing.T) {
	f := newRemote(t)
	f.seedMain(hubRepo)
	f.branch("fix/x", "x.txt")
	git(t, f.work, "remote", "add", "hub", f.repoURL(hubRepo))
	env := f.cardEnv("C1")
	// git sends two more headers of the same names from the client's own config, as another card would.
	// UNDER THE FORWARDER'S OWN SCOPE: a bare http.extraHeader is dropped by git when a more specific key is set, and
	// would leave the forwarder's Del and Set untested.
	env = append(env, "GIT_CONFIG_COUNT=3",
		"GIT_CONFIG_KEY_1="+ExtraHeaderKey(f.base), "GIT_CONFIG_VALUE_1="+HeaderCard+": evil-card",
		"GIT_CONFIG_KEY_2="+ExtraHeaderKey(f.base), "GIT_CONFIG_VALUE_2="+HeaderChain+": evil-card,C1")
	out, err := f.git(env, f.work, "push", "hub", "fix/x")
	f.must(out, err)
	if len(f.rt.hdrs) == 0 {
		t.Fatal("nothing reached the hub")
	}
	for _, h := range f.rt.hdrs {
		if v := h.Values(HeaderCard); len(v) != 1 || v[0] != "C1" {
			t.Fatalf("%s reached the hub as %q, want only the forwarder's C1", HeaderCard, v)
		}
		if v := h.Values(HeaderChain); len(v) != 1 || v[0] != "C1" {
			t.Fatalf("%s reached the hub as %q, want only the forwarder's C1", HeaderChain, v)
		}
	}
	if rows := f.pushRows(); rows[len(rows)-1].Card != "C1" {
		t.Fatalf("the push log names %q", rows[len(rows)-1].Card)
	}
}

func TestAFetchFromAnotherServerInACardEnvReceivesNoAtriumHeader(t *testing.T) {
	f := newRemote(t)
	var mu sync.Mutex
	var seen []http.Header
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Clone())
		mu.Unlock()
		http.NotFound(w, r)
	}))
	defer other.Close()
	// The dangerous neighbour: the same host, another port, a path under /git/ too.
	for _, u := range []string{other.URL + "/o/r.git", other.URL + "/git/hub/github/o/r.git"} {
		f.git(f.cardEnv("C1"), f.work, "fetch", u)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) == 0 {
		t.Fatal("the other server was never asked, so this proves nothing")
	}
	for _, h := range seen {
		for k := range h {
			if strings.HasPrefix(strings.ToLower(k), "x-atrium") {
				t.Fatalf("the other server received %s", k)
			}
		}
	}
	// And the key is scoped: never a bare http.extraHeader.
	for _, kv := range f.cardEnv("C1") {
		if strings.HasPrefix(kv, "GIT_CONFIG_KEY_") && kv[strings.Index(kv, "=")+1:] == "http.extraHeader" {
			t.Fatalf("a bare extraHeader: %s", kv)
		}
	}
}

func TestTheForwarderServesNothingButGitsFourRequests(t *testing.T) {
	f := newRemote(t)
	tok, _ := f.cards.Mint("C1")
	for _, c := range []struct{ method, path, ctype string }{
		{"GET", "/git/hub/github.com/o/r.git/info/refs?service=git-upload-pack", ""}, // not the canonical host
		{"GET", "/git/hub/github/o/r.git/HEAD", ""},
		{"GET", "/git/hub/github/o/r.git/info/refs?service=other", ""},
		{"DELETE", "/git/hub/github/o/r.git/info/refs?service=git-upload-pack", ""},
		{"POST", "/git/hub/github/o/r.git/git-receive-pack", "text/plain"},
		{"GET", "/git/hub/github/o/%2e%2e/r.git/info/refs?service=git-upload-pack", ""},
	} {
		req, _ := http.NewRequest(c.method, f.srv.URL+c.path, nil)
		req.Header.Set(HeaderCardToken, tok)
		if c.ctype != "" {
			req.Header.Set("Content-Type", c.ctype)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode < 400 {
			t.Errorf("%s %s answered %d", c.method, c.path, res.StatusCode)
		}
	}
	if f.rt.count() != 0 {
		t.Fatal("something that is not git's reached the hub")
	}
}

func TestTheHubsRefusalReachesGitUntouched(t *testing.T) {
	f := newRemote(t)
	f.seedMain(hubRepo)
	f.branch("fix/x", "x.txt")
	git(t, f.work, "remote", "add", "hub", f.repoURL(hubRepo))
	// A push of main from a card is refused by the hub's pre-receive, and its words come back as the hub wrote them.
	out, err := f.git(f.cardEnv("C1"), f.work, "push", "hub", "fix/x:refs/heads/main")
	if err == nil || !strings.Contains(out, "atrium:") {
		t.Fatalf("a card's push of main: %v\n%s", err, out)
	}
	if strings.Contains(out, "this room's hub remote") {
		t.Fatalf("the forwarder spoke where the hub should have:\n%s", out)
	}
}

func TestCardTokensAreMintedPerCardAndReplaced(t *testing.T) {
	c := &CardTokens{}
	a, _ := c.Mint("C1")
	b, _ := c.Mint("C1")
	if a == b {
		t.Fatal("two mints gave the same token")
	}
	if _, ok := c.Check(a); ok {
		t.Fatal("the replaced token still works")
	}
	if id, ok := c.Check(b); !ok || id != "C1" {
		t.Fatal("the new token does not")
	}
	if len(strings.TrimPrefix(b, "C1.")) != 64 {
		t.Fatalf("not 256 bits: %q", b)
	}
	if _, err := c.Mint("not a card id!"); err == nil {
		t.Fatal("minted for something that is not a card id")
	}
}

// The same, by hand: a request that already carries two values of each card header, as a client that Adds would.
// Whatever the client sent, the hub sees one of each and it is the forwarder's.
func TestTwoCardHeadersFromAClientBecomeTheForwardersOneEach(t *testing.T) {
	f := newRemote(t)
	f.seedMain(hubRepo)
	tok, _ := f.cards.Mint("C1")
	req, _ := http.NewRequest(http.MethodGet, f.repoURL(hubRepo)+"/info/refs?service=git-upload-pack", nil)
	req.Header.Set(HeaderCardToken, tok)
	req.Header.Add(HeaderCard, "evil-card")
	req.Header.Add(HeaderCard, "evil-card-2")
	req.Header.Add(HeaderChain, "evil-card,C1")
	req.Header.Add(HeaderChain, "evil-card-2")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 || len(f.rt.hdrs) != 1 {
		t.Fatalf("%d, %d requests to the hub", res.StatusCode, len(f.rt.hdrs))
	}
	h := f.rt.hdrs[0]
	if v := h.Values(HeaderCard); len(v) != 1 || v[0] != "C1" {
		t.Fatalf("%s: %q", HeaderCard, v)
	}
	if v := h.Values(HeaderChain); len(v) != 1 || v[0] != "C1" {
		t.Fatalf("%s: %q", HeaderChain, v)
	}
}
