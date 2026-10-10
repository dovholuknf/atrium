package store

import (
	"strings"
	"testing"
)

func TestReportSummaryOneLineIsOk(t *testing.T) {
	if why := CheckReportSummary("done, the check is in and tested"); why != "" {
		t.Fatalf("one line refused: %s", why)
	}
}

func TestReportSummaryTwoLinesIsRefused(t *testing.T) {
	why := CheckReportSummary("first line\nsecond line")
	if why == "" || !strings.Contains(why, "REPORT.md") {
		t.Fatalf("two lines gave %q, want a refusal naming REPORT.md", why)
	}
}

func TestReportSummaryOverTheWordLimitIsRefused(t *testing.T) {
	words := strings.TrimSpace(strings.Repeat("word ", ReportSummaryMax+1))
	why := CheckReportSummary(words)
	if why == "" || !strings.Contains(why, "REPORT.md") {
		t.Fatalf("%d words gave %q, want a refusal", ReportSummaryMax+1, why)
	}
	if why := CheckReportSummary(strings.TrimSpace(strings.Repeat("word ", ReportSummaryMax))); why != "" {
		t.Fatalf("%d words refused: %s", ReportSummaryMax, why)
	}
}

func TestReportSummaryTrailingNewlineIsOk(t *testing.T) {
	for _, s := range []string{"one line\n", "one line\r\n", "one line  \n\n", "\n one line"} {
		if why := CheckReportSummary(s); why != "" {
			t.Fatalf("%q refused: %s", s, why)
		}
	}
}
