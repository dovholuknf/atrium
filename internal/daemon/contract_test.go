package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestContractAnswersItsVersions(t *testing.T) {
	rec := httptest.NewRecorder()
	handleContract(rec, httptest.NewRequest(http.MethodGet, "/contract", nil))
	if got := strings.TrimSpace(rec.Body.String()); got != `{"versions":[1]}` {
		t.Fatalf("answer: %s", got)
	}
	rec = httptest.NewRecorder()
	handleContract(rec, httptest.NewRequest(http.MethodPost, "/contract", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: %d", rec.Code)
	}
}

func TestSessionStartAcceptsContractAndFeatures(t *testing.T) {
	var in SessionEvent
	body := `{"agent":"a","event":"start","contract":1,"features":["session","stop"]}`
	if err := json.NewDecoder(strings.NewReader(body)).Decode(&in); err != nil {
		t.Fatal(err)
	}
	if in.Contract != 1 || len(in.Features) != 2 {
		t.Fatalf("decoded: %+v", in)
	}
}
