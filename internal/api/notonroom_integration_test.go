//go:build integration

package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A PATCH of a card this room does not hold names the card and the room, not
// the store's "sql: no rows". A drag into a group that reached the wrong room
// said only that. See backlog-2 item 63.
func TestAPatchOfACardThisRoomDoesNotHoldNamesIt(t *testing.T) {
	srv, _, _ := fileServer(t)
	srv.Room = "claude-sg4"

	req := httptest.NewRequest(http.MethodPatch, "/v1/tasks/nosuchcard", strings.NewReader(`{"group":"g"}`))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("answered %d %s, want 404", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "card nosuchcard is not on room claude-sg4") || strings.Contains(body, "sql") {
		t.Fatalf("the not-found said %s", body)
	}
}
