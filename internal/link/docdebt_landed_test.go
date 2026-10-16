package link

import "testing"

func TestDocsSaidReadsTheLineAboveTheTestPlan(t *testing.T) {
	for name, tc := range map[string]struct {
		note string
		want bool
	}{
		"an id":          {"# x\n\nDocs: d-7\n\n## Test plan\n- a\n", true},
		"none and why":   {"# x\n\nDocs: none, nothing a reader names\n\n## Test plan\n", true},
		"none no why":    {"# x\n\nDocs: none\n\n## Test plan\n", false},
		"no line":        {"# x\n\n## Test plan\n- a\n", false},
		"only in a plan": {"# x\n\n## Test plan\nDocs: d-7\n", false},
		"indented":       {"# x\n  Docs: d-7\n## Test plan\n", false},
		"crlf":           {"# x\r\nDocs: d-7\r\n## Test plan\r\n", true},
		"empty":          {"", false},
	} {
		if got := docsSaid(tc.note); got != tc.want {
			t.Errorf("%s: docsSaid = %v, want %v", name, got, tc.want)
		}
	}
}
