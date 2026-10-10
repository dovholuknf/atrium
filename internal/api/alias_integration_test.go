//go:build integration

package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Renaming a card that wears no alias to `saorch: ...` gives it `@saorch`, and
// a rename that also sets the alias keeps what it set. See backlog-2 item 47.
func TestARenameGivesTheDefaultAlias(t *testing.T) {
	srv, st, dir := fileServer(t)
	c := cardIn(t, st, dir)
	patch := func(body string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPatch, "/v1/tasks/"+c.ID, strings.NewReader(body))
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s answered %d %s", body, rec.Code, rec.Body.String())
		}
	}
	patch(`{"overrides":{"title":"saorch: merger, owns claude/main"}}`)
	if got, _ := st.Get(c.ID); got.Alias != "saorch" {
		t.Fatalf("the rename gave alias %q, want saorch", got.Alias)
	}
	patch(`{"alias":"","overrides":{"title":"doer: something"}}`)
	if got, _ := st.Get(c.ID); got.Alias != "" {
		t.Fatalf("a rename that cleared the alias was given %q", got.Alias)
	}
}
