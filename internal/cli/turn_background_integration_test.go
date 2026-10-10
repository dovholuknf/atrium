//go:build integration

package cli

import "testing"

func TestRunningOtherCountsRunningTasksThatAreNotSubagents(t *testing.T) {
	tasks := []backgroundTask{
		{ID: "a", Type: "subagent", Status: "running"},
		{ID: "b", Type: "shell", Status: "running"},
		{ID: "c", Type: "shell", Status: "completed"},
		{ID: "d", Type: "bash", Status: "Running"},
	}
	if got := runningOther(tasks); got != 2 {
		t.Fatalf("runningOther = %d, want 2", got)
	}
}
