//go:build integration

package link

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// The phone page is a directory of the board. The hub serves the board itself, so /m/ has to answer with
// m/index.html and /m has to be sent to /m/, the way the room's own file server does.
func TestHubServesThePhonePage(t *testing.T) {
	board := fstest.MapFS{
		"index.html":             &fstest.MapFile{Data: []byte("<!doctype html>the board")},
		"m/index.html":           &fstest.MapFile{Data: []byte("<!doctype html>the phone page")},
		"m/manifest.webmanifest": &fstest.MapFile{Data: []byte(`{"name":"atrium"}`)},
	}
	h := NewProxy(NewHub(Timings{}), board, "", nil)
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}
	if rec := get("/m/"); rec.Code != 200 || !strings.Contains(rec.Body.String(), "the phone page") {
		t.Fatalf("/m/ answered %d %q, want the phone page", rec.Code, rec.Body.String())
	}
	rec := get("/m")
	if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "/m/" {
		t.Fatalf("/m answered %d to %q, want a redirect to /m/", rec.Code, rec.Header().Get("Location"))
	}
	if rec := get("/m/manifest.webmanifest"); rec.Code != 200 || rec.Body.String() != `{"name":"atrium"}` {
		t.Fatalf("the manifest answered %d %q", rec.Code, rec.Body.String())
	}
}
