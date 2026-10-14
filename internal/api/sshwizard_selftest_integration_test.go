//go:build integration

package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// A plain GET must not start the self test, which makes the room ssh into itself. Only self_test=1 does.
func TestSSHWizardRouteSelfTestIsOffUnlessAsked(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "atrium.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	srv := New(st)
	var got string
	srv.SSHWizard = func(r *http.Request) any {
		got = r.URL.Query().Get("self_test")
		return map[string]any{}
	}
	for url, want := range map[string]string{
		"/v1/ssh/wizard":                "0",
		"/v1/ssh/wizard?user=claude":    "0",
		"/v1/ssh/wizard?self_test=1":    "1",
		"/v1/ssh/wizard?self_test=true": "0",
	} {
		got = ""
		srv.Handler().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, url, nil))
		if got != want {
			t.Errorf("%s: handler saw self_test=%q, want %q", url, got, want)
		}
	}
}
