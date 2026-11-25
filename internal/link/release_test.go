package link

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func releaseGet(t *testing.T, p *Proxy) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	p.serveRelease(rec, httptest.NewRequest(http.MethodGet, "/_hub/release", nil))
	return decodeJSON(t, rec.Body.String())
}

func decodeJSON(t *testing.T, body string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("not json: %q", body)
	}
	return out
}

func waitRelease(t *testing.T, c *ReleaseCheck) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		busy := c.inflight
		c.mu.Unlock()
		if !busy {
			return
		}
		time.Sleep(5 * time.Millisecond) // wait: poll until the release read has finished
	}
	t.Fatal("the read never finished")
}

func TestReleaseIsNotFoundWithoutACheck(t *testing.T) {
	p := &Proxy{}
	rec := httptest.NewRecorder()
	p.serveRelease(rec, httptest.NewRequest(http.MethodGet, "/_hub/release", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code %d", rec.Code)
	}
}

func TestReleaseReadsOnceThenAnswersFromTheCache(t *testing.T) {
	calls := 0
	c := NewReleaseCheck("1.7.4")
	c.fetch = func(context.Context) (string, string, error) { calls++; return "v1.8.0", "https://example/r", nil }
	p := &Proxy{release: c}
	if got := releaseGet(t, p); got["state"] != "checking" || got["hub"] != "1.7.4" {
		t.Fatalf("first answer %v", got)
	}
	waitRelease(t, c)
	got := releaseGet(t, p)
	if got["state"] != "ok" || got["latest"] != "v1.8.0" || got["url"] != "https://example/r" || got["checked_at"] == nil {
		t.Fatalf("after the read %v", got)
	}
	releaseGet(t, p)
	if calls != 1 {
		t.Fatalf("asked github %d times in a day", calls)
	}
}

func TestReleaseOfflineIsQuietAndKeepsTheLastAnswer(t *testing.T) {
	c := NewReleaseCheck("1.7.4")
	c.fetch = func(context.Context) (string, string, error) { return "", "", errors.New("offline") }
	p := &Proxy{release: c}
	releaseGet(t, p)
	waitRelease(t, c)
	if got := releaseGet(t, p); got["state"] != "unknown" || got["checked_at"] != nil || got["latest"] != "" {
		t.Fatalf("never reached github, got %v", got)
	}
	c.mu.Lock()
	c.latest, c.okAt, c.triedAt = "v1.8.0", time.Now().Add(-3*time.Hour), time.Now().Add(-2*time.Hour)
	c.mu.Unlock()
	releaseGet(t, p)
	waitRelease(t, c)
	got := releaseGet(t, p)
	if got["state"] != "unknown" || got["latest"] != "v1.8.0" || got["checked_at"] == nil {
		t.Fatalf("offline after a good read %v", got)
	}
}

func TestReleaseCheckCanBeSwitchedOff(t *testing.T) {
	t.Setenv("ATRIUM_NO_UPDATE_CHECK", "1")
	c := NewReleaseCheck("1.7.4")
	c.fetch = func(context.Context) (string, string, error) { t.Fatal("asked github"); return "", "", nil }
	if got := (&Proxy{release: c}).release.view(time.Now()); got["state"] != "unknown" {
		t.Fatalf("%v", got)
	}
}

func TestGithubLatestSendsNoToken(t *testing.T) {
	var auth, agent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, agent = r.Header.Get("Authorization"), r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(`{"tag_name":"v9.9.9","html_url":"https://x/y"}`))
	}))
	defer srv.Close()
	old := http.DefaultTransport
	http.DefaultTransport = rewriteTo(srv.URL, old)
	defer func() { http.DefaultTransport = old }()
	tag, page, err := githubLatest(context.Background())
	if err != nil || tag != "v9.9.9" || page != "https://x/y" || auth != "" || !strings.Contains(agent, "atrium") {
		t.Fatalf("%q %q %v auth=%q agent=%q", tag, page, err, auth, agent)
	}
}

type rt func(*http.Request) (*http.Response, error)

func (f rt) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func rewriteTo(base string, next http.RoundTripper) http.RoundTripper {
	return rt(func(r *http.Request) (*http.Response, error) {
		r2 := r.Clone(r.Context())
		r2.URL.Scheme, r2.URL.Host = "http", strings.TrimPrefix(base, "http://")
		return next.RoundTrip(r2)
	})
}
