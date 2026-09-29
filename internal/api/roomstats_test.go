package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRoomStatsServesTheLastPushedSnapshotOnly(t *testing.T) {
	srv, _, _ := setupServer(t)
	get := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/room/stats", nil))
		return rec
	}
	if rec := get(); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("before a sample: %d", rec.Code)
	}
	last := []byte(`{"v":1,"room":"sg3"}`)
	srv.RoomStats = func() []byte { return last }
	rec := get()
	if rec.Code != http.StatusOK || rec.Body.String() != string(last) {
		t.Errorf("got %d %q", rec.Code, rec.Body.String())
	}
}
