package cli

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestNowLineIsOneLineWithDateWeekdayTimeAndZone(t *testing.T) {
	got := nowLine(time.Date(2026, 10, 22, 8, 38, 0, 0, time.FixedZone("EDT", -4*3600)))
	if got != "now: 2026-10-22 Thu 08:38 EDT" {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(got, "\n") || !regexp.MustCompile(`^now: \d{4}-\d\d-\d\d \w{3} \d\d:\d\d \S+$`).MatchString(got) {
		t.Fatalf("not one now line: %q", got)
	}
}
