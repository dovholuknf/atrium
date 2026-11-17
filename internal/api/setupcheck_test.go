package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestTheSetupCheckListsNoUsageLine(t *testing.T) {
	rec := httptest.NewRecorder()
	(&Server{}).setupCheck(rec, httptest.NewRequest("GET", "/v1/setup-check", nil))
	var out struct {
		Issues []checkIssue `json:"issues"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	for _, i := range out.Issues {
		if i.ID == "usage" {
			t.Fatalf("the setup check listed %+v", i)
		}
	}
}
