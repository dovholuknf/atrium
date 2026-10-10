package store

import (
	"fmt"
	"strings"
)

// ReportSummaryMax is the longest summary atrium_report takes, in words. The same limit atrium_blocked has.
const ReportSummaryMax = 50

// CheckReportSummary is why the summary of an atrium_report is refused, or "": one line of up to 50 words. Trailing
// whitespace and a trailing newline are not a second line. An empty summary is not this rule's to refuse.
func CheckReportSummary(summary string) string {
	s := strings.TrimSpace(summary)
	if s == "" {
		return ""
	}
	if strings.ContainsAny(s, "\r\n") {
		return "a report summary is one line. put the detail in REPORT.md on your branch and send one line"
	}
	if n := len(strings.Fields(s)); n > ReportSummaryMax {
		return fmt.Sprintf("the summary is %d words, over %d. put the detail in REPORT.md on your branch and "+
			"send one line", n, ReportSummaryMax)
	}
	return ""
}
