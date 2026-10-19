//go:build integration

package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestARemovalNamingAPathIsRefused(t *testing.T) {
	t.Parallel()
	srv, _, _ := fileServer(t)
	called := false
	srv.Homes = func() (any, error) { return map[string]any{}, nil }
	srv.HomeRemove = func(string) (any, error) { called = true; return map[string]any{}, nil }
	for _, body := range []string{`{"path":"/etc"}`, `{"id":"h1","path":"/etc"}`, `{}`, `{"id":""}`} {
		req := httptest.NewRequest(http.MethodPost, "/v1/homes/remove", strings.NewReader(body))
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s answered %d, want 400", body, rec.Code)
		}
	}
	if called {
		t.Fatal("a refused body reached the removal")
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/homes/remove", strings.NewReader(`{"id":"h1"}`))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !called {
		t.Fatalf("an id alone answered %d, called=%v", rec.Code, called)
	}
}
