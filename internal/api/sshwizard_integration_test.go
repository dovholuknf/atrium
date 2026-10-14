//go:build integration

package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// The wizard's route only reads: it is a GET, there is no verb that writes, and a daemon with no hub has none.
func TestSSHWizardRouteIsReadOnlyAndAbsentWithoutARoom(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "atrium.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	srv := New(st)

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/ssh/wizard", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("a daemon with no room answered %d", rec.Code)
	}

	var asked string
	srv.SSHWizard = func(r *http.Request) any {
		asked = r.URL.RawQuery
		return map[string]any{"ready": false}
	}
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/ssh/wizard?user=claude", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"ready":false`) || asked != "self_test=0&user=claude" {
		t.Fatalf("%d %s %q", rec.Code, rec.Body.String(), asked)
	}
	for _, verb := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		rec = httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(verb, "/v1/ssh/wizard", nil))
		if rec.Code != http.StatusMethodNotAllowed && rec.Code != http.StatusNotFound {
			t.Fatalf("%s answered %d", verb, rec.Code)
		}
	}
}
