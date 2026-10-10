//go:build integration

package daemon

import (
	"net/http"
	"strings"
	"testing"
)

// A launched worker's report summary is one short line. The rules are in store.CheckReportSummary, this proves the
// room's validReport applies them.
func TestAWorkersReportSummaryIsOneLine(t *testing.T) {
	d := testDaemon(t)
	launchedPair(t, d)
	long := strings.TrimSpace(strings.Repeat("word ", 51))
	for _, tc := range []struct {
		name, recap string
		want        int
	}{
		{"one line", "finished the check", http.StatusOK},
		{"trailing newline", "finished the check\n", http.StatusOK},
		{"two lines", "finished\nthe check", http.StatusBadRequest},
		{"over the word limit", long, http.StatusBadRequest},
	} {
		rec, _ := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportProgress, Recap: tc.recap})
		if rec.Code != tc.want {
			t.Fatalf("%s answered %d, want %d: %s", tc.name, rec.Code, tc.want, rec.Body.String())
		}
	}
}
