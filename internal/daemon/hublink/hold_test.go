package hublink

import "testing"

func TestOnlyAFilingAnEditAndAMergeAskAreHeld(t *testing.T) {
	for _, c := range []struct{ method, path, want string }{
		{"POST", "/_hub/backlog", "backlog.file"},
		{"POST", "/_hub/backlog/f-1", "backlog.edit"},
		{"POST", "/_hub/merge", "merge.ask"},
		{"GET", "/_hub/backlog", ""},
		{"POST", "/_hub/merge/report", ""},
		{"POST", "/_hub/reports", ""},
		{"POST", "/_hub/backlog?x=1", ""},
	} {
		if got := heldKind(c.method, c.path); got != c.want {
			t.Errorf("%s %s: %q, want %q", c.method, c.path, got, c.want)
		}
	}
}
