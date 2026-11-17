package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"
)

func TestUsageCheckFiresOnlyAfterARecentTurnWithNoPost(t *testing.T) {
	keepFacts, keepStart := UsageFacts, apiStart
	t.Cleanup(func() { UsageFacts, apiStart = keepFacts, keepStart })
	apiStart = time.Now().Add(-24 * time.Hour)
	now := time.Now()
	stale := now.Add(-time.Hour)
	cases := []struct {
		name      string
		ended, at time.Time
		want      bool
	}{
		{"turn ended 5 minutes ago, no usage posted", now.Add(-5 * time.Minute), stale, true},
		{"turn ended 30 seconds ago", now.Add(-30 * time.Second), stale, false},
		{"idle room, turn ended an hour ago", now.Add(-time.Hour), stale, false},
		{"usage posted recently", now.Add(-5 * time.Minute), now.Add(-4 * time.Minute), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			UsageFacts = func() (time.Time, time.Time) { return c.ended, c.at }
			rec := httptest.NewRecorder()
			(&Server{}).setupCheck(rec, httptest.NewRequest("GET", "/v1/setup-check", nil))
			var out struct {
				Issues []checkIssue `json:"issues"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			got := false
			for _, i := range out.Issues {
				got = got || i.ID == "usage"
			}
			if got != c.want {
				t.Fatalf("usage issue = %v, want %v (issues %+v)", got, c.want, out.Issues)
			}
		})
	}
}
