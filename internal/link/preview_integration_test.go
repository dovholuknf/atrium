//go:build integration

package link

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

type previewRig struct {
	front *httptest.Server
	port  int
	seen  chan *http.Request
}

// newPreviewRig is a real hub and room over loopback mutual TLS, the hub's proxy in front, and a site on the room's
// 127.0.0.1 that only `allow` is let through to.
func newPreviewRig(t *testing.T) *previewRig {
	t.Helper()
	pr := &previewRig{seen: make(chan *http.Request, 16)}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		select {
		case pr.seen <- r.Clone(context.Background()):
		default:
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Set-Cookie", "board=gone; Path=/")
		fmt.Fprint(w, `<html><head><link href="/atrium/site.css" rel="stylesheet"><style>a{background:url('/img.png')}</style></head>`+
			`<body><a href="/">home</a> <a href="//cdn.example/x.js">cdn</a> <img src=/logo.png></body></html>`)
	})
	mux.HandleFunc("/old", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/atrium/new?x=1", http.StatusFound)
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		typ, b, err := c.Read(r.Context())
		if err != nil {
			return
		}
		_ = c.Write(r.Context(), typ, append([]byte("echo "), b...))
	})
	site := httptest.NewServer(mux)
	t.Cleanup(site.Close)
	pr.port = site.Listener.Addr().(*net.TCPAddr).Port

	rg := newRig(t)
	rk, err := rg.enrol("r1", rg.secretFor("r1"))
	if err != nil {
		t.Fatal(err)
	}
	room := &Room{
		Name: "r1", Dial: Proven{Dialer: plain{addr: rg.addr}, Keys: rk},
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "board") }),
		PreviewPort: func(port int) error {
			if port != pr.port {
				return fmt.Errorf("not %d", port)
			}
			return nil
		},
		T: Timings{Beat: 200 * time.Millisecond, Warm: 1, Backoff: 50 * time.Millisecond},
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = room.Run(ctx) }()
	waitFor(t, 10*time.Second, func() bool { _, ok := rg.attachedAs("r1"); return ok && room.State().Up })
	pr.front = httptest.NewServer(NewProxy(rg.hub, nil, "", nil))
	t.Cleanup(pr.front.Close)
	return pr
}

func (pr *previewRig) prefix() string { return "/r1/preview/" + strconv.Itoa(pr.port) }

func noFollow() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// A SITE ON THE ROOM'S LOOPBACK OPENS THROUGH THE HUB, its root-relative links under the prefix, sandboxed, and the
// board's cookie never reaches it.
func TestPreviewServesARoomLoopbackSiteUnderThePrefix(t *testing.T) {
	pr := newPreviewRig(t)
	req, _ := http.NewRequest(http.MethodGet, pr.front.URL+pr.prefix()+"/atrium/?q=1", nil)
	req.Header.Set("Cookie", "atrium=secret")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("answered %d: %s", res.StatusCode, b)
	}
	got := string(b)
	for _, want := range []string{`href="` + pr.prefix() + `/atrium/site.css"`, `url('` + pr.prefix() + `/img.png')`,
		`href="` + pr.prefix() + `/"`, `href="//cdn.example/x.js"`, `src=` + pr.prefix() + `/logo.png`} {
		if !strings.Contains(got, want) {
			t.Errorf("the page lacks %s:\n%s", want, got)
		}
	}
	if csp := res.Header.Get("Content-Security-Policy"); !strings.HasPrefix(csp, "sandbox ") || strings.Contains(csp, "allow-same-origin") {
		t.Errorf("the preview is not sandboxed away from the board's origin: %q", csp)
	}
	if res.Header.Get("Set-Cookie") != "" {
		t.Errorf("a preview set a cookie on the board's origin: %q", res.Header.Get("Set-Cookie"))
	}
	up := <-pr.seen
	if up.URL.Path != "/atrium/" || up.URL.RawQuery != "q=1" {
		t.Errorf("the site was asked for %s?%s", up.URL.Path, up.URL.RawQuery)
	}
	if up.Header.Get("Cookie") != "" || up.Header.Get("X-Forwarded-For") == "" {
		t.Errorf("cookie %q reached the site, forwarded-for %q", up.Header.Get("Cookie"), up.Header.Get("X-Forwarded-For"))
	}
	if up.Header.Get("X-Forwarded-Prefix") != pr.prefix() {
		t.Errorf("X-Forwarded-Prefix was %q", up.Header.Get("X-Forwarded-Prefix"))
	}
}

// A REDIRECT STAYS UNDER THE PREFIX, and a preview named without its slash is sent to it.
func TestPreviewRedirectsStayUnderThePrefix(t *testing.T) {
	pr := newPreviewRig(t)
	res, err := noFollow().Get(pr.front.URL + pr.prefix() + "/old")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusFound || res.Header.Get("Location") != pr.prefix()+"/atrium/new?x=1" {
		t.Fatalf("answered %d to %q", res.StatusCode, res.Header.Get("Location"))
	}
	res, err = noFollow().Get(pr.front.URL + pr.prefix() + "?a=b")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusMovedPermanently || res.Header.Get("Location") != pr.prefix()+"/?a=b" {
		t.Fatalf("no slash answered %d to %q", res.StatusCode, res.Header.Get("Location"))
	}
}

// A WEBSOCKET PASSES THROUGH to the room's loopback port.
func TestPreviewCarriesAWebsocket(t *testing.T) {
	pr := newPreviewRig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(pr.front.URL, "http")+pr.prefix()+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	if err := c.Write(ctx, websocket.MessageText, []byte("hi")); err != nil {
		t.Fatal(err)
	}
	_, b, err := c.Read(ctx)
	if err != nil || string(b) != "echo hi" {
		t.Fatalf("read %q, %v", b, err)
	}
}

// ONLY A PORT ON THE ROOM'S 127.0.0.1, and only one the room allows. The room's own link route is not reachable.
func TestPreviewRefusesAnythingButAnAllowedLoopbackPort(t *testing.T) {
	pr := newPreviewRig(t)
	for path, want := range map[string]int{
		"/r1/preview/10.0.0.1:80/":                      http.StatusBadRequest,
		"/r1/preview/example.com/":                      http.StatusBadRequest,
		"/r1/preview/70000/":                            http.StatusBadRequest,
		"/r1/preview/" + strconv.Itoa(pr.port+1) + "/":  http.StatusForbidden,
		"/nope/preview/" + strconv.Itoa(pr.port) + "/":  http.StatusServiceUnavailable,
		"/_link/preview/" + strconv.Itoa(pr.port) + "/": http.StatusNotFound,
	} {
		res, err := noFollow().Get(pr.front.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != want {
			t.Errorf("%s answered %d, wanted %d: %s", path, res.StatusCode, want, b)
		}
	}
	select {
	case r := <-pr.seen:
		t.Fatalf("the site was reached by a refused preview: %s", r.URL)
	default:
	}
}
