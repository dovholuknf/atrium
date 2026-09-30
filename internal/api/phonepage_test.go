package api

import (
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
