package daemon

import (
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// A scratch launch is tagged to stay open, so its done report does not exit it. A worktree launch still exits.
func TestScratchLaunchIsNotExitedByDone(t *testing.T) {
	t.Parallel()
	d := &Daemon{}
	for _, tc := range []struct {
		name    string
		req     LaunchRequest
		exit    bool
		wantTag bool
	}{
		{"scratch", LaunchRequest{Scratch: true, Tags: []string{OriginAgentTag}}, false, true},
		{"worktree", LaunchRequest{Tags: []string{OriginAgentTag}}, true, false},
	} {
		keepScratchOpen(&tc.req)
		if got := hasTag(tc.req.Tags, KeepOpenTag); got != tc.wantTag {
			t.Errorf("%s: keep-open tag = %v, want %v", tc.name, got, tc.wantTag)
		}
		task := &store.Task{SpawnedByID: "launcher", Tags: tc.req.Tags}
		if got := d.exitsOnReport(task, FinishRequest{Status: ReportDone}); got != tc.exit {
			t.Errorf("%s: exits = %v, want %v", tc.name, got, tc.exit)
		}
	}
	req := LaunchRequest{Scratch: true, Tags: []string{KeepOpenTag}}
	keepScratchOpen(&req)
	if len(req.Tags) != 1 {
		t.Errorf("tag added twice: %v", req.Tags)
	}
}
