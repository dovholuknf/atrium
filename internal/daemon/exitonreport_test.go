package daemon

import (
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func TestScratchWorkerIsExitedByDoneUnlessKeptOpen(t *testing.T) {
	t.Parallel()
	d := &Daemon{}
	for _, tc := range []struct {
		name string
		tags []string
		in   FinishRequest
		exit bool
	}{
		{"scratch worker", []string{OriginAgentTag}, FinishRequest{Status: ReportDone}, true},
		{"scratch resident", []string{OriginAgentTag, ResidentTag}, FinishRequest{Status: ReportDone}, false},
		{"scratch keep-open", []string{OriginAgentTag, KeepOpenTag}, FinishRequest{Status: ReportDone}, false},
		{"scratch investigation", []string{OriginAgentTag, InvestigationTag}, FinishRequest{Status: ReportDone}, false},
		{"scratch blocked", []string{OriginAgentTag}, FinishRequest{Status: ReportBlocked}, false},
	} {
		task := &store.Task{SpawnedByID: "launcher", Tags: tc.tags, Lifetime: store.LifetimeWork}
		if got := d.exitsOnReport(task, tc.in); got != tc.exit {
			t.Errorf("%s: exits = %v, want %v", tc.name, got, tc.exit)
		}
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

func TestAnUnresolvedAgentLauncherStillGetsItsWorkerExited(t *testing.T) {
	t.Parallel()
	d := &Daemon{}
	for _, tc := range []struct {
		name string
		task *store.Task
		exit bool
	}{
		{"handle only", &store.Task{SpawnedBy: "saorch", Tags: []string{OriginAgentTag}}, true},
		{"human launcher", &store.Task{SpawnedBy: store.HumanLauncher, Tags: []string{OriginAgentTag}}, false},
		{"no launcher", &store.Task{Tags: []string{OriginAgentTag}}, false},
	} {
		if got := d.exitsOnReport(tc.task, FinishRequest{Status: ReportDone}); got != tc.exit {
			t.Errorf("%s: exits = %v, want %v", tc.name, got, tc.exit)
		}
	}
}

func TestAWorkLifetimeCardIsExitedWhoeverLaunchedIt(t *testing.T) {
	t.Parallel()
	d := &Daemon{}
	for _, tc := range []struct {
		name string
		task *store.Task
		exit bool
	}{
		{"work by the operator", &store.Task{SpawnedBy: store.HumanLauncher, Lifetime: store.LifetimeWork}, true},
		{"work with no launcher", &store.Task{Lifetime: store.LifetimeWork}, true},
		{"work resident", &store.Task{Lifetime: store.LifetimeWork, Tags: []string{ResidentTag}}, false},
		{"work orchestrator", &store.Task{Lifetime: store.LifetimeWork, Tags: []string{OrchestratorTag}}, false},
		{"work keep-open", &store.Task{Lifetime: store.LifetimeWork, Tags: []string{KeepOpenTag}}, false},
		{"no lifetime by the operator", &store.Task{SpawnedBy: store.HumanLauncher}, false},
	} {
		if got := d.exitsOnReport(tc.task, FinishRequest{Status: ReportDone}); got != tc.exit {
			t.Errorf("%s: exits = %v, want %v", tc.name, got, tc.exit)
		}
	}
}
