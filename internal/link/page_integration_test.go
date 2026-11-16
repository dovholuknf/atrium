//go:build integration

package link

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/base/seenhost"
	"github.com/dovholuknf/atrium/internal/store"
)

// A CARD'S PAGE ARRIVES WHOLE THROUGH THE HUB, its root-relative links rewritten, and the length it declares is the
// length of the body it sends.
func TestAPageThroughTheHubArrivesWholeWithItsLinksRewritten(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "atrium.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	work := t.TempDir()
	if err := os.MkdirAll(filepath.Join(work, "mock"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(work, "mock", "mock.html"),
		[]byte(`<link rel="stylesheet" href="/css/tokens.css"><link rel="stylesheet" href="/mock.css"><p>hello</p>`), 0o644)
	task, _, err := st.Register(store.Observed{WireName: "pg", Worktree: filepath.ToSlash(work), Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}

	rg := newRig(t)
	rk, err := rg.enrol("r1", rg.secretFor("r1"))
	if err != nil {
		t.Fatal(err)
	}
	room := &Room{
		Name: "r1", Dial: Proven{Dialer: plain{addr: rg.addr}, Keys: rk},
		Handler: api.New(st).Handler(),
		T:       Timings{Beat: 200 * time.Millisecond, Warm: 1, Backoff: 50 * time.Millisecond},
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = room.Run(ctx) }()
	waitFor(t, 10*time.Second, func() bool { _, ok := rg.attachedAs("r1"); return ok && room.State().Up })
	front := httptest.NewServer(NewProxy(rg.hub, nil, "", nil))
	t.Cleanup(front.Close)

	pre := "/room/r1/" + task.ID + "/files/"
	for _, p := range []string{"mock/", "mock/mock.html"} {
		res, err := http.Get(front.URL + pre + p + "?v=1")
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil || res.StatusCode != http.StatusOK {
			t.Fatalf("%s: %d, read %v", p, res.StatusCode, err)
		}
		got := string(b)
		if !strings.Contains(got, `href="`+pre+`_board/css/tokens.css"`) || !strings.Contains(got, `href="`+pre+`mock/mock.css"`) ||
			!strings.Contains(got, "<p>hello</p>") {
			t.Fatalf("%s arrived as %q", p, got)
		}
		if !strings.HasPrefix(res.Header.Get("Content-Security-Policy"), "sandbox ") {
			t.Fatalf("%s is not sandboxed", p)
		}
	}
}

// A ROOM LEARNS THE HOST FROM THE HUB on any request it forwards, and a browser cannot name it.
func TestTheHubForwardsItsSeenHostToTheRoomAndStripsABrowsersOwn(t *testing.T) {
	seenhost.Reset()
	t.Cleanup(seenhost.Reset)
	got := make(chan string, 4)
	rg := newRig(t)
	rk, err := rg.enrol("r1", rg.secretFor("r1"))
	if err != nil {
		t.Fatal(err)
	}
	room := &Room{
		Name: "r1", Dial: Proven{Dialer: plain{addr: rg.addr}, Keys: rk},
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got <- r.Header.Get(seenhost.HeaderName) }),
		T:       Timings{Beat: 200 * time.Millisecond, Warm: 1, Backoff: 50 * time.Millisecond},
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = room.Run(ctx) }()
	waitFor(t, 10*time.Second, func() bool { _, ok := rg.attachedAs("r1"); return ok && room.State().Up })
	front := httptest.NewServer(NewProxy(rg.hub, nil, "", nil))
	t.Cleanup(front.Close)

	do := func() string {
		req, _ := http.NewRequest("GET", front.URL+"/room/r1/x", nil)
		req.Header.Set(seenhost.HeaderName, "http://evil")
		req.Header.Set("X-Atrium-Room", "r1")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return <-got
	}
	if h := do(); h != "" {
		t.Fatalf("a browser's own header reached the room: %q", h)
	}
	seenhost.NoteBase("http://atrium.ziti")
	if h := do(); h != "http://atrium.ziti" {
		t.Fatalf("the room was told %q", h)
	}
}

// THE EVENT STREAM A BROWSER OPENS ON THE HUB is enough for the hub to know the host a launch's watch link uses. The paths
// are /v1/events/hub and /v1/events/room/<room>, which an exact match on /v1/events missed.
func TestABrowsersEventStreamOnTheHubRecordsItsHost(t *testing.T) {
	seenhost.Reset()
	t.Cleanup(seenhost.Reset)
	rg := newRig(t)
	front := httptest.NewServer(NewProxy(rg.hub, nil, "", nil))
	t.Cleanup(front.Close)
	for _, p := range []string{"/v1/events/hub", "/v1/events/room/sgg"} {
		seenhost.Reset()
		req, _ := http.NewRequest("GET", front.URL+p, nil)
		req.Host = "atrium.ziti"
		req.Header.Set("Sec-Fetch-Mode", "cors")
		req.Header.Set("Accept", "text/event-stream")
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		res, err := http.DefaultClient.Do(req.WithContext(ctx))
		if err == nil {
			res.Body.Close()
		}
		cancel()
		if got := seenhost.Base("none"); got != "http://atrium.ziti" {
			t.Fatalf("%s left the host as %q", p, got)
		}
	}
	seenhost.Reset()
	req, _ := http.NewRequest("GET", front.URL+"/v1/events/hub", nil)
	req.Host = "other"
	res, err := http.DefaultClient.Do(req)
	if err == nil {
		res.Body.Close()
	}
	if got := seenhost.Base("none"); got != "none" {
		t.Fatalf("a request with no Sec-Fetch-Mode was recorded as %q", got)
	}
}

// A BOARD OVER PLAIN HTTP ON A NAME, atrium.ziti, gets no Sec-Fetch headers from the browser, which sends them only to https
// and loopback. Its User-Agent is what tells it from the CLI.
func TestABrowserOnPlainHTTPWithNoFetchHeadersRecordsItsHost(t *testing.T) {
	seenhost.Reset()
	t.Cleanup(seenhost.Reset)
	rg := newRig(t)
	front := httptest.NewServer(NewProxy(rg.hub, nil, "", nil))
	t.Cleanup(front.Close)
	hit := func(ua string) {
		req, _ := http.NewRequest("GET", front.URL+"/v1/events/hub", nil)
		req.Host = "atrium.ziti"
		req.Header.Set("User-Agent", ua)
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		if res, err := http.DefaultClient.Do(req.WithContext(ctx)); err == nil {
			res.Body.Close()
		}
	}
	hit("Go-http-client/1.1")
	hit("curl/8.1")
	if got := seenhost.Base("none"); got != "none" {
		t.Fatalf("the CLI's request was recorded as %q", got)
	}
	hit("Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148")
	if got := seenhost.Base("none"); got != "http://atrium.ziti" {
		t.Fatalf("the phone's request was recorded as %q", got)
	}
}
