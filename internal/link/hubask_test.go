package link

import "testing"

func TestHubAskPath(t *testing.T) {
	for _, c := range []struct {
		path string
		want bool
	}{
		{"/_hub/backlog", true},
		{"/_hub/backlog?status=open", true},
		{"/_hub/backlog/r-001", true},
		{"/_hub/backlog/r-001/status", true},
		{"/_hub/reports", true},
		{"/_hub/reports?limit=5", true},
		{"/_hub/reports/r-001", true},
		{"/_hub/backlog?q=a%20b", true},
		{"/_hub/backlog/follow", false},
		{"/_hub/backlog/follow/", false},
		{"/_hub/backlog/follow?x=1", false},
		{"/_hub/backlog/%2e%2e/rooms", false},
		{"/_hub/backlog/%2E%2E/rooms", false},
		{"/_hub/backlog/..%2frooms", false},
		{"/_hub/backlog/../rooms", false},
		{"/_hub/backlog/./r-001", false},
		{"/_hub/backlog//r-001", false},
		{"//_hub/backlog", false},
		{"/_hub/backlog/", false},
		{"/_hub/backlog\\r-001", false},
		{"/_hub/rooms", false},
		{"/_hub/backlogs", false},
		{"/v1/tasks", false},
		{"", false},
	} {
		if got := hubAskPath(c.path); got != c.want {
			t.Errorf("hubAskPath(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}
