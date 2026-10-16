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

// A per-item minion wears atrium:director so it can end itself, yet a final done still exits it. A plain director,
// a minion held open and a minion that is also resident do not.
func TestMinionDirectorExitsOnDone(t *testing.T) {
	t.Parallel()
	d := &Daemon{}
	for _, tc := range []struct {
		name string
		tags []string
		exit bool
	}{
		{"minion director", []string{DirectorTag, MinionTag}, true},
		{"plain director", []string{DirectorTag}, false},
		{"minion keep-open", []string{DirectorTag, MinionTag, KeepOpenTag}, false},
		{"minion resident", []string{DirectorTag, MinionTag, ResidentTag}, false},
		{"minion orchestrator", []string{DirectorTag, MinionTag, OrchestratorTag}, false},
	} {
		task := &store.Task{SpawnedByID: "launcher", Tags: tc.tags}
		if got := d.exitsOnReport(task, FinishRequest{Status: ReportDone}); got != tc.exit {
			t.Errorf("%s: exits = %v, want %v", tc.name, got, tc.exit)
		}
	}
	task := &store.Task{SpawnedByID: "launcher", Tags: []string{DirectorTag, MinionTag}}
	if d.exitsOnReport(task, FinishRequest{Status: ReportDone, Ask: "which one?"}) {
		t.Error("a done carrying an ask exited the minion")
	}
}
