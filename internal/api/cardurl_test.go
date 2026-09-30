package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A CARD'S READABLE ADDRESS IS THE BOARD PAGE on the room's own port, the phone's
// on /m/, a wrong shape is a 404 naming the shapes, and no API path is shadowed.
func TestCardAddressesServeThePage(t *testing.T) {
	srv, _, _ := fileServer(t)
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}
	board, phone := get("/").Body.String(), get("/m/").Body.String()
	if board == "" || phone == "" || board == phone {
		t.Fatal("the test needs the board and the phone page to differ")
	}
	for path, want := range map[string]string{
		"/alias/rnd": board, "/room/claude-sg4": board, "/room/claude-sg4/rnd-director": board,
		"/m/alias/rnd": phone, "/m/room/claude-sg4/rnd": phone,
	} {
		rec := get(path)
		if rec.Code != http.StatusOK || rec.Body.String() != want {
			t.Errorf("%s answered %d and not the right page", path, rec.Code)
		}
	}
	for _, path := range []string{"/alias/", "/room/a/b/c", "/m/room/a"} {
		rec := get(path)
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "not a card address") {
			t.Errorf("%s answered %d %q", path, rec.Code, rec.Body.String())
		}
	}
	if rec := get("/v1/tasks"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"tasks"`) {
		t.Errorf("/v1/tasks answered %d %q", rec.Code, rec.Body.String())
	}
}
