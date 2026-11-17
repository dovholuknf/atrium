package link

import (
	"net/http"
	"testing"
)

func TestARoomEventCarriesOnlyTheRequestItsKindNames(t *testing.T) {
	for _, c := range []struct {
		kind, method, path string
		ok                 bool
	}{
		{"backlog.file", http.MethodPost, "/_hub/backlog", true},
		{"backlog.edit", http.MethodPost, "/_hub/backlog/m1-3", true},
		{"merge.ask", http.MethodPost, "/_hub/merge", true},
		{"merge.ask", http.MethodGet, "/_hub/merge", false},
		{"merge.ask", http.MethodPost, "/_hub/merge/land", false},
		{"backlog.file", http.MethodPost, "/_hub/items/sync", false},
		{"backlog.edit", http.MethodPost, "/_hub/backlog/follow", false},
		{"backlog.edit", http.MethodPost, "/_hub/backlog/a/b", false},
		{"reports", http.MethodPost, "/_hub/reports", false},
	} {
		if got := roomEventAllowed(c.kind, c.method, c.path); got != c.ok {
			t.Errorf("%s %s %s: %v, want %v", c.kind, c.method, c.path, got, c.ok)
		}
	}
	if !hubAskPath(hubRoomEventsPath) || hubAskPath("/_hub/room-events") {
		t.Error("only the apply path is relayed")
	}
}
