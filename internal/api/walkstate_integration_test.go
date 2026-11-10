//go:build integration

package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func walkStateCall(s *Server, method, id, body string) (int, string) {
	r := httptest.NewRequest(method, "/v1/tasks/"+id+"/walk", strings.NewReader(body))
	r.SetPathValue("id", id)
	w := httptest.NewRecorder()
	if method == "PUT" {
		s.walkStatePut(w, r)
	} else {
		s.walkStateGet(w, r)
	}
	return w.Code, strings.TrimSpace(w.Body.String())
}

func TestWalkStateIsKeptPerCardAndRefusesNonObjects(t *testing.T) {
	t.Parallel()
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)
	if code, body := walkStateCall(s, "GET", task.ID, ""); code != http.StatusOK || body != `{"state":null}` {
		t.Fatalf("a new card has no walk: %d %s", code, body)
	}
	if code, _ := walkStateCall(s, "PUT", task.ID, `{"comments":[{"line":3}]}`); code != http.StatusOK {
		t.Fatalf("put: %d", code)
	}
	if code, body := walkStateCall(s, "GET", task.ID, ""); code != http.StatusOK || body != `{"state":{"comments":[{"line":3}]}}` {
		t.Fatalf("saved walk read back as %d %s", code, body)
	}
	for _, bad := range []string{`[]`, `null`, `"x"`, `{`} {
		if code, _ := walkStateCall(s, "PUT", task.ID, bad); code != http.StatusBadRequest {
			t.Fatalf("%s was taken: %d", bad, code)
		}
	}
	if code, _ := walkStateCall(s, "PUT", task.ID, `{"a":"`+strings.Repeat("x", maxWalkStateBytes)+`"}`); code != http.StatusRequestEntityTooLarge {
		t.Fatalf("an oversized walk was taken: %d", code)
	}
}
