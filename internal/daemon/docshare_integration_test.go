//go:build integration

package daemon

import (
	"errors"
	"fmt"
	"github.com/dovholuknf/atrium/internal/daemon/publish"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/store"
)

type fakeDocBinder struct {
	mu       sync.Mutex
	reserved map[string]bool
	released []string
	lns      map[string]net.Listener
}

func newFakeDocBinder() *fakeDocBinder {
	return &fakeDocBinder{reserved: map[string]bool{}, lns: map[string]net.Listener{}}
}

func (f *fakeDocBinder) Reserve(ns, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reserved[ns+"/"+name] = true
	return nil
}

func (f *fakeDocBinder) Unreserve(ns, name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.reserved, ns+"/"+name)
}

func (f *fakeDocBinder) Release(token string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.released = append(f.released, token)
}

func (f *fakeDocBinder) Bind(rec *store.DocShare, h http.Handler) (net.Listener, string, string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", "", err
	}
	f.mu.Lock()
	f.lns[rec.ID] = ln
	f.mu.Unlock()
	return ln, "tok-" + rec.ID, "http://" + ln.Addr().String() + "/", nil
}

func (f *fakeDocBinder) reservedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.reserved)
}

func docShareFixture(t *testing.T) (*Daemon, *fakeDocBinder, *store.Task, string) {
	t.Helper()
	d, _, cancel, _ := startDaemon(t)
	t.Cleanup(cancel)
	fb := newFakeDocBinder()
	d.Docs.B = fb
	wt := t.TempDir()
	task, _, err := d.st.Register(store.Observed{WireName: "blog", Worktree: wt, Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	must := func(rel, body string) {
		p := filepath.Join(wt, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must("docs/a.md", "# Title\n\n![one](img/p.png) ![two](img/q.png)\n\n[other](b.md)\n")
	must("docs/b.md", "# Other\n")
	must("docs/img/p.png", "PNG-P")
	must("docs/img/q.png", "PNG-Q")
	must("secret.md", "-----BEGIN RSA PRIVATE KEY-----\nabc\n")
	must("outside-img.md", "x")
	return d, fb, task, wt
}

func docGet(t *testing.T, base, path string) (int, string, http.Header) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, base, nil)
	req.URL.Path = path
	req.URL.RawPath = path
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), resp.Header
}

func asDocShare(t *testing.T, v any) publish.DocShareInfo {
	t.Helper()
	info, ok := v.(publish.DocShareInfo)
	if !ok {
		t.Fatalf("a share came back as %T", v)
	}
	return info
}

func docShareStatus(err error) int {
	var de *api.DocShareError
	if errors.As(err, &de) {
		return de.Code
	}
	return 0
}

func TestDocShareServesOnlyTheSnapshot(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: a whole daemon")
	}
	d, _, task, wt := docShareFixture(t)
	out, err := d.MakeDocShare(task.ID, api.DocShareRequest{Path: "docs/a.md", Images: []string{"docs/img/p.png", "docs/img/q.png"}})
	if err != nil {
		t.Fatal(err)
	}
	s := asDocShare(t, out)
	base := strings.TrimSuffix(s.Address, "/")

	for p, want := range map[string]string{"/doc.md": "# Title", "/f/0": "PNG-P", "/f/1": "PNG-Q"} {
		code, body, hd := docGet(t, base, p)
		if code != 200 || !strings.Contains(body, want) {
			t.Fatalf("%s answered %d %q", p, code, body)
		}
		if hd.Get("X-Content-Type-Options") != "nosniff" || hd.Get("Referrer-Policy") != "no-referrer" ||
			!strings.Contains(hd.Get("Content-Security-Policy"), "default-src 'none'") {
			t.Fatalf("%s lacks its headers: %v", p, hd)
		}
	}
	if ct := func() string { _, _, h := docGet(t, base, "/f/0"); return h.Get("Content-Type") }(); ct != "image/png" {
		t.Fatalf("an image was served as %q", ct)
	}
	for _, p := range []string{"/read.js", "/vendor/marked.js", "/vendor/purify.js", "/css/tokens.css", "/css/themes.css"} {
		if code, _, _ := docGet(t, base, p); code != 200 {
			t.Fatalf("%s answered %d", p, code)
		}
	}
	code, page, _ := docGet(t, base, "/")
	if code != 200 || !strings.Contains(page, `data-source="share"`) || strings.Contains(page, "__SHARE_MAP__") ||
		!strings.Contains(page, "docs/a.md") || strings.Contains(page, wt) {
		t.Fatalf("the page answered %d: %.300s", code, page)
	}
	for _, p := range []string{"/v1/health", "/v1/tasks", "/read.html", "/index.html", "/f/2", "/f/01", "/f/-1", "/f/",
		"/map.json", "/share.html", "/../x", "/..%2f..%2fx", "/f/0/../../map.json", "/js/read.js"} {
		if code, _, _ := docGet(t, base, p); code != 404 {
			t.Fatalf("%s answered %d, wanted 404", p, code)
		}
	}
	resp, err := http.Post(base+"/doc.md", "text/plain", strings.NewReader("x"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("a POST answered %d", resp.StatusCode)
	}
}

func TestDocShareRefusesWhatItShould(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: a whole daemon")
	}
	d, fb, task, wt := docShareFixture(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "o.md"), []byte("# out"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "o.png"), []byte("PNG"), 0o644); err != nil {
		t.Fatal(err)
	}
	linked := true
	if err := os.Symlink(filepath.Join(outside, "o.md"), filepath.Join(wt, "link.md")); err != nil {
		linked = false
	}
	_ = os.Symlink(filepath.Join(outside, "o.png"), filepath.Join(wt, "docs", "img", "out.png"))

	try := func(req api.DocShareRequest) error {
		_, err := d.MakeDocShare(task.ID, req)
		return err
	}
	if err := try(api.DocShareRequest{Path: "../" + filepath.Base(outside) + "/o.md"}); docShareStatus(err) != 403 {
		t.Fatalf("a path outside the card: %v", err)
	}
	if linked {
		if err := try(api.DocShareRequest{Path: "link.md"}); docShareStatus(err) != 403 {
			t.Fatalf("a symlink out of the card: %v", err)
		}
	}
	if err := try(api.DocShareRequest{Path: "docs/a.md", Images: []string{"../" + filepath.Base(outside) + "/o.png"}}); docShareStatus(err) != 403 {
		t.Fatalf("an image outside the card: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(wt, "docs", "img", "out.png")); err == nil {
		if err := try(api.DocShareRequest{Path: "docs/a.md", Images: []string{"docs/img/out.png"}}); docShareStatus(err) != 403 {
			t.Fatalf("an image symlinked out of the card: %v", err)
		}
	}
	err := try(api.DocShareRequest{Path: "secret.md"})
	if docShareStatus(err) != 403 || !strings.Contains(err.Error(), "speed bump") {
		t.Fatalf("a document holding a secret: %v", err)
	}
	if err := try(api.DocShareRequest{Path: "docs/a.md", Images: []string{"docs/a.md"}}); docShareStatus(err) != 400 {
		t.Fatalf("a non-image as an image: %v", err)
	}
	if err := try(api.DocShareRequest{Path: "docs/img/p.png"}); docShareStatus(err) != 400 {
		t.Fatalf("a non-markdown document: %v", err)
	}
	if err := try(api.DocShareRequest{Path: "docs/nope.md"}); docShareStatus(err) != 404 {
		t.Fatalf("a missing document: %v", err)
	}
	if recs, _ := d.st.DocShares(); len(recs) != 0 || fb.reservedCount() != 0 {
		t.Fatalf("a refusal left %d rows and %d names behind", len(recs), fb.reservedCount())
	}
	if ents, _ := os.ReadDir(d.DocShareRoot()); len(ents) != 0 {
		t.Fatalf("a refusal left %d folders behind", len(ents))
	}
}

func TestDocShareStopExpiryAndRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: a whole daemon")
	}
	d, fb, task, _ := docShareFixture(t)
	first := asDocShare(t, mustDocShare(t, d, task.ID, "docs/a.md"))
	again := asDocShare(t, mustDocShare(t, d, task.ID, "docs/a.md"))
	if again.ID != first.ID || again.Address != first.Address {
		t.Fatalf("sharing the same document twice made a second share: %s then %s", first.Address, again.Address)
	}
	if got := d.DocShareOf(task.ID, "docs/a.md"); got == nil {
		t.Fatal("the share is not found by its card and path")
	}

	if err := d.StopDocShare(first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := http.Get(first.Address + "doc.md"); err == nil {
		t.Fatal("a stopped share still answers")
	}
	if recs, _ := d.st.DocShares(); len(recs) != 0 {
		t.Fatalf("a stopped share left %d rows", len(recs))
	}
	if _, err := os.Stat(d.DocShareDir(first.ID)); !os.IsNotExist(err) {
		t.Fatalf("a stopped share left its folder: %v", err)
	}
	if fb.reservedCount() != 0 {
		t.Fatal("a stopped share kept its name")
	}
	if err := d.StopDocShare(first.ID); docShareStatus(err) != 404 {
		t.Fatalf("stopping twice: %v", err)
	}

	keep := asDocShare(t, mustDocShare(t, d, task.ID, "docs/a.md"))
	d.StopAllDocShares()
	if _, err := http.Get(keep.Address + "doc.md"); err == nil {
		t.Fatal("a shutdown left the share answering")
	}
	if recs, _ := d.st.DocShares(); len(recs) != 1 || !recs[0].Wanted {
		t.Fatalf("a shutdown did not leave the row wanted: %+v", recs)
	}
	d.RestoreDocShares()
	waitFor(t, func() bool { return d.Docs.Has(keep.ID) })
	rec, _ := d.st.DocShareByID(keep.ID)
	if code, body, _ := docGet(t, strings.TrimSuffix(rec.Address, "/"), "/doc.md"); code != 200 || !strings.Contains(body, "# Title") {
		t.Fatalf("a restored share answered %d %q", code, body)
	}

	rec.ExpiresAt = time.Now().Add(-time.Hour).UTC().Format(store.TimeFormat)
	if err := d.st.PutDocShare(*rec); err != nil {
		t.Fatal(err)
	}
	d.SweepDocShares()
	if recs, _ := d.st.DocShares(); len(recs) != 0 {
		t.Fatalf("an expired share survived the sweep: %+v", recs)
	}
	if _, err := os.Stat(d.DocShareDir(keep.ID)); !os.IsNotExist(err) || fb.reservedCount() != 0 || d.Docs.Has(keep.ID) {
		t.Fatal("an expired share kept its folder, its name or its listener")
	}

	stale := asDocShare(t, mustDocShare(t, d, task.ID, "docs/b.md"))
	d.StopAllDocShares()
	rec, _ = d.st.DocShareByID(stale.ID)
	rec.ExpiresAt = time.Now().Add(-time.Minute).UTC().Format(store.TimeFormat)
	_ = d.st.PutDocShare(*rec)
	d.RestoreDocShares()
	waitFor(t, func() bool { r, _ := d.st.DocShareByID(stale.ID); return r == nil })
	if d.Docs.Has(stale.ID) {
		t.Fatal("a restart rebound an expired share")
	}
}

func TestDocShareOutlivesItsCardAndIsCapped(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: a whole daemon")
	}
	d, _, task, wt := docShareFixture(t)
	s := asDocShare(t, mustDocShare(t, d, task.ID, "docs/a.md"))
	if err := d.st.Forget(task.ID); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := docGet(t, strings.TrimSuffix(s.Address, "/"), "/doc.md"); code != 200 {
		t.Fatalf("the share stopped with its card: %d", code)
	}
	list, _ := d.DocShares().([]publish.DocShareInfo)
	if len(list) != 1 || !list[0].CardGone {
		t.Fatalf("the list does not say the card is gone: %+v", list)
	}

	task2, _, err := d.st.Register(store.Observed{WireName: "second", Worktree: wt, Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < publish.DocShareMaxLive; i++ {
		name := fmt.Sprintf("n%d.md", i)
		if err := os.WriteFile(filepath.Join(wt, name), []byte("# n"), 0o644); err != nil {
			t.Fatal(err)
		}
		mustDocShare(t, d, task2.ID, name)
	}
	if err := os.WriteFile(filepath.Join(wt, "one-too-many.md"), []byte("# n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = d.MakeDocShare(task2.ID, api.DocShareRequest{Path: "one-too-many.md"})
	if err == nil || !strings.Contains(err.Error(), fmt.Sprint(publish.DocShareMaxLive)) {
		t.Fatalf("the share past the cap: %v", err)
	}
}

func TestDocShareUpdateKeepsNothingStale(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: a whole daemon")
	}
	d, _, task, wt := docShareFixture(t)
	s := asDocShare(t, mustDocShare(t, d, task.ID, "docs/a.md"))
	if err := os.WriteFile(filepath.Join(wt, "docs", "a.md"), []byte("# Changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, body, _ := docGet(t, strings.TrimSuffix(s.Address, "/"), "/doc.md"); !strings.Contains(body, "# Title") {
		t.Fatalf("the snapshot followed the worktree: %q", body)
	}
}

func mustDocShare(t *testing.T, d *Daemon, id, path string) any {
	t.Helper()
	out, err := d.MakeDocShare(id, api.DocShareRequest{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond) // wait: poll under the 10 s deadline
	}
	t.Fatal("timed out waiting")
}
