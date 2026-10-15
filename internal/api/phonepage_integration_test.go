//go:build integration

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// u-025 owns these three files, so the page may name them before they exist.
var phoneOptional = map[string]bool{"js/compose.js": true, "js/perms.js": true, "compose.css": true}

// Every script and stylesheet the phone page names is served, and every file under web/m is named by the page.
func TestPhonePageLoadsEveryFile(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join("web", "m", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)
	h := webHandler("")
	get := func(p string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		return rec
	}
	named := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?:src|href)="(/[^"]+\.(?:js|css|webmanifest|png))"`).FindAllStringSubmatch(page, -1) {
		p := m[1]
		if strings.HasPrefix(p, "/m/") {
			named[strings.TrimPrefix(p, "/m/")] = true
			if phoneOptional[strings.TrimPrefix(p, "/m/")] {
				continue
			}
		}
		if rec := get(p); rec.Code != 200 {
			t.Errorf("%s answered %d", p, rec.Code)
		}
	}
	filepath.WalkDir(filepath.Join("web", "m"), func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(filepath.Join("web", "m"), p)
		rel = filepath.ToSlash(rel)
		if rel != "index.html" && !named[rel] && !strings.HasPrefix(rel, "icons/") {
			t.Errorf("web/m/%s is not loaded by web/m/index.html", rel)
		}
		return nil
	})
	for _, want := range []string{"js/util.js", "js/md.js", "js/store.js", "js/home.js", "js/card.js", "m.css"} {
		if !named[want] {
			t.Errorf("the phone page does not load %s", want)
		}
	}
}

// The room's file server sends /m to the relative m/, which a browser resolves to /m/.
func TestPhonePageRouteAndManifest(t *testing.T) {
	t.Parallel()
	h := webHandler("")
	get := func(p string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		return rec
	}
	if rec := get("/m/"); rec.Code != 200 || !strings.Contains(rec.Body.String(), `rel="manifest"`) {
		t.Fatalf("/m/ answered %d", rec.Code)
	}
	if rec := get("/m"); rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "m/" {
		t.Fatalf("/m answered %d to %q", rec.Code, rec.Header().Get("Location"))
	}
	rec := get("/m/manifest.webmanifest")
	if rec.Code != 200 {
		t.Fatalf("manifest answered %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "json") && !strings.Contains(ct, "manifest") {
		t.Errorf("manifest content type %q", ct)
	}
	for _, p := range []string{"/m/icons/icon-192.png", "/m/icons/icon-512.png", "/m/icons/icon-maskable-512.png", "/m/icons/apple-touch-icon.png"} {
		if r := get(p); r.Code != 200 || r.Header().Get("Content-Type") != "image/png" {
			t.Errorf("%s answered %d %q", p, r.Code, r.Header().Get("Content-Type"))
		}
	}
}

// The reload check notices a change to the phone page, not only to the board.
func TestBuildIDCoversThePhonePage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "m"), 0o755)
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("x"), 0o644)
	f := filepath.Join(dir, "m", "index.html")
	os.WriteFile(f, []byte("one"), 0o644)
	a := buildID(board(dir))
	os.WriteFile(f, []byte("two"), 0o644)
	if b := buildID(board(dir)); a == b {
		t.Fatal("the build id did not change when web/m changed")
	}
}

// The desktop board is installable: its manifest is served as a manifest, owns the whole origin, and asks for links.
func TestDesktopManifest(t *testing.T) {
	t.Parallel()
	h := webHandler("")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/manifest.webmanifest", nil))
	if rec.Code != 200 {
		t.Fatalf("manifest answered %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/manifest+json" {
		t.Errorf("manifest content type %q", ct)
	}
	var m struct {
		StartURL      string `json:"start_url"`
		HandleLinks   string `json:"handle_links"`
		LaunchHandler struct {
			ClientMode []string `json:"client_mode"`
		} `json:"launch_handler"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m.StartURL != "/" || m.HandleLinks != "preferred" || len(m.LaunchHandler.ClientMode) == 0 || m.LaunchHandler.ClientMode[0] != "focus-existing" {
		t.Errorf("manifest is %s", rec.Body.String())
	}
	page := httptest.NewRecorder()
	h.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(page.Body.String(), `rel="manifest" href="/manifest.webmanifest"`) {
		t.Error("the board page does not link its manifest")
	}
}
