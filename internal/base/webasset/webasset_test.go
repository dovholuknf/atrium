package webasset

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

var script = strings.Repeat("function hello() { return 'the board'; }\n", 200)

func tree() fstest.MapFS {
	return fstest.MapFS{
		"js/core.js":             {Data: []byte(script)},
		"vendor/xterm.js":        {Data: []byte(script)},
		"icon.png":               {Data: []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 400))},
		"m/manifest.webmanifest": {Data: []byte(`{"name":"atrium"}`)},
	}
}

func get(t *testing.T, s *Server, name string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/"+name, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	if !s.Serve(rec, req, name) {
		t.Fatalf("%s was not served", name)
	}
	return rec
}

// A SCRIPT IS GZIPPED FOR A BROWSER THAT TAKES IT, and the body unzips to the file.
func TestAScriptIsGzipped(t *testing.T) {
	s := New(tree())
	rec := get(t, s, "js/core.js", map[string]string{"Accept-Encoding": "gzip, deflate, br"})
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("not gzipped: %v", rec.Header())
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/javascript") {
		t.Fatalf("the type was sniffed from the gzip bytes: %q", rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec.Header().Get("Vary"), "Accept-Encoding") {
		t.Fatal("no Vary: Accept-Encoding, so a cache can hand the gzip to a client that did not ask")
	}
	if rec.Body.Len() >= len(script) {
		t.Fatalf("the body is %d bytes for a %d byte file", rec.Body.Len(), len(script))
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(zr)
	if string(got) != script {
		t.Fatal("the gzip does not unzip to the file")
	}
}

// NO GZIP WITHOUT ASKING, or when the browser says q=0, and the tags differ.
func TestPlainWhenNotAsked(t *testing.T) {
	s := New(tree())
	zipped := get(t, s, "js/core.js", map[string]string{"Accept-Encoding": "gzip"})
	for _, ae := range []string{"", "identity", "gzip;q=0", "br"} {
		rec := get(t, s, "js/core.js", map[string]string{"Accept-Encoding": ae})
		if rec.Header().Get("Content-Encoding") != "" || rec.Body.String() != script {
			t.Errorf("Accept-Encoding %q got an encoded body", ae)
		}
		if rec.Header().Get("ETag") == zipped.Header().Get("ETag") {
			t.Errorf("Accept-Encoding %q: the plain and the gzipped body share an ETag", ae)
		}
	}
}

// A RELOAD IS A 304 with no body, for both forms.
func TestARevalidationIsNotModified(t *testing.T) {
	s := New(tree())
	for _, ae := range []string{"", "gzip"} {
		first := get(t, s, "js/core.js", map[string]string{"Accept-Encoding": ae})
		if first.Header().Get("Cache-Control") != "no-cache" {
			t.Fatalf("board files must revalidate every time: %q", first.Header().Get("Cache-Control"))
		}
		again := get(t, s, "js/core.js", map[string]string{"Accept-Encoding": ae, "If-None-Match": first.Header().Get("ETag")})
		if again.Code != http.StatusNotModified || again.Body.Len() != 0 {
			t.Errorf("Accept-Encoding %q: a revalidation answered %d with %d bytes", ae, again.Code, again.Body.Len())
		}
	}
}

// A FILE CHANGED ON DISK is re-read, with a new ETag, so an edit shows on the next reload.
func TestAChangedFileGetsANewTag(t *testing.T) {
	fsys := tree()
	s := New(fsys)
	before := get(t, s, "js/core.js", nil)
	fsys["js/core.js"] = &fstest.MapFile{Data: []byte(script + "// edited\n"), ModTime: time.Now()}
	after := get(t, s, "js/core.js", nil)
	if after.Header().Get("ETag") == before.Header().Get("ETag") || !strings.HasSuffix(after.Body.String(), "// edited\n") {
		t.Fatal("an edited file was served from the old copy")
	}
}

// Vendor files keep a day's cache. An image is never gzipped. The manifest has its type.
func TestTypesAndVendor(t *testing.T) {
	s := New(tree())
	if cc := get(t, s, "vendor/xterm.js", nil).Header().Get("Cache-Control"); cc != "public, max-age=86400" {
		t.Errorf("vendor cache is %q", cc)
	}
	png := get(t, s, "icon.png", map[string]string{"Accept-Encoding": "gzip"})
	if png.Header().Get("Content-Encoding") != "" || !bytes.HasPrefix(png.Body.Bytes(), []byte("\x89PNG")) {
		t.Error("an image was gzipped")
	}
	if ct := get(t, s, "m/manifest.webmanifest", nil).Header().Get("Content-Type"); ct != "application/manifest+json" {
		t.Errorf("the manifest is %q", ct)
	}
}

// A directory or a missing file is not this server's to answer.
func TestNotAFile(t *testing.T) {
	s := New(tree())
	for _, name := range []string{"js", "nope.js", ""} {
		if s.Serve(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil), name) {
			t.Errorf("%q was served", name)
		}
	}
}
