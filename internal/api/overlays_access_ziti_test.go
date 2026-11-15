package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func postZiti(s *Server, way, remote, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/v1/access/ziti/"+way, strings.NewReader(body))
	r.SetPathValue("way", way)
	r.RemoteAddr, r.Host = remote, "127.0.0.1:7781"
	w := httptest.NewRecorder()
	s.accessZiti(w, r)
	return w
}

func TestAccessZitiIsLocalOperatorOnlyAndTokenModeNeedsTheAck(t *testing.T) {
	s := &Server{bus: newBus(), AccessZitiDir: func() string { return t.TempDir() }}
	if w := postZiti(s, "import", "10.0.0.5:5000", "{}"); w.Code != http.StatusForbidden {
		t.Fatalf("remote: %d", w.Code)
	}
	w := postZiti(s, "join", "127.0.0.1:5000", `{"mode":"token","controller":"c:1280"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "confirm") {
		t.Fatalf("no ack: %d %s", w.Code, w.Body.String())
	}
	if w := postZiti(s, "nope", "127.0.0.1:5000", "{}"); w.Code != http.StatusNotFound {
		t.Fatalf("unknown way: %d", w.Code)
	}
}
