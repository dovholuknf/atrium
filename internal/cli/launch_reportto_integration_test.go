//go:build integration

package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// `atrium launch --report-to` reaches /v1/launch as report_to.
func TestLaunchReportToFlagReachesTheEndpoint(t *testing.T) {
	c := newLaunch()
	if c.Flags().Lookup("report-to") == nil {
		t.Fatal("atrium launch has no --report-to")
	}
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c1","display_title":"x"}`))
	}))
	defer srv.Close()
	err := launchAgent(launchOpts{boardURL: srv.URL, harness: "claude", cwd: t.TempDir(), reportTo: "review", quiet: true})
	if err != nil {
		t.Fatal(err)
	}
	if got["report_to"] != "review" {
		t.Fatalf("body carried %v", got["report_to"])
	}
}
