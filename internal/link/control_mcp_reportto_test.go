package link

import (
	"reflect"
	"strings"
	"testing"
)

// r-014: report_to is the board's and the CLI's. The agent-side atrium_launch
// has no such field, so its launcher is always the session that called it.
func TestAtriumLaunchHasNoReportTo(t *testing.T) {
	typ := reflect.TypeOf(launchInput{})
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if strings.Contains(strings.ToLower(f.Name), "reportto") || strings.Contains(f.Tag.Get("json"), "report_to") {
			t.Fatalf("launchInput has %s", f.Name)
		}
	}
}
