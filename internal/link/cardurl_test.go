package link

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// THE HUB SERVES A CARD'S READABLE ADDRESS AS THE BOARD PAGE, before anything is
// proxied, and a wrong shape is its 404.
func TestTheHubServesCardAddresses(t *testing.T) {
	board := fstest.MapFS{
		"index.html":   {Data: []byte("the board")},
		"m/index.html": {Data: []byte("the phone")},
	}
	p := NewProxy(NewHub(Timings{}), board, "", nil)
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}
	for path, want := range map[string]string{
		"/alias/rnd": "the board", "/room/claude-sg4/rnd": "the board", "/room/claude-sg4": "the board",
		"/m/alias/rnd": "the phone", "/m/room/claude-sg4/rnd": "the phone",
	} {
		if rec := get(path); rec.Code != http.StatusOK || rec.Body.String() != want {
			t.Errorf("%s answered %d %q", path, rec.Code, rec.Body.String())
		}
	}
	if rec := get("/alias/a/b"); rec.Code != http.StatusNotFound ||
		!strings.Contains(rec.Body.String(), "not a card address") {
		t.Errorf("a wrong shape answered %d %q", rec.Code, rec.Body.String())
	}
}
