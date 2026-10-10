//go:build integration

package daemon

import (
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func setPolicy(t *testing.T, d *Daemon, p store.WorkerPolicy) {
	t.Helper()
	v, err := store.CheckWorkerPolicy(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetSetting(store.SettingWorkerPolicy, v); err != nil {
		t.Fatal(err)
	}
}

// spend writes one row of a card's spend. A million output tokens on Sonnet 5.5 is ten dollars at list price.
func spend(t *testing.T, d *Daemon, id string, outputMillions int64) {
	t.Helper()
	if err := d.st.AddSessionUsage(&store.SessionUsage{TaskID: id, ResumeID: "r", Model: "claude-sonnet-5-5",
		Replies: 1, Output: outputMillions * 1_000_000}); err != nil {
		t.Fatal(err)
	}
}

func budgetNotices(t *testing.T, d *Daemon, launcherID string) int {
	t.Helper()
	n := 0
	for _, h := range heldOn(t, d, launcherID) {
		if h["source"] == NoticeBudget {
			n++
		}
	}
	return n
}

func TestTheWorkerBudgetIsOffUntilSet(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := holdingPair(t, d, OrchestratorTag)
	spend(t, d, worker.ID, 50)
	d.checkBudget(worker)
	if n := budgetNotices(t, d, launcher.ID); n != 0 {
		t.Fatalf("%d budget notices with no budget set", n)
	}
	if v := d.budgetFor(worker.ID); v != nil {
		t.Fatalf("a card shows %v with no budget set", v)
	}
}

func TestAWorkerPastTheBudgetTellsItsLauncherOnceAndShowsOnItsCard(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := holdingPair(t, d, OrchestratorTag)
	setPolicy(t, d, store.WorkerPolicy{BudgetUSD: 25})

	spend(t, d, worker.ID, 1)
	d.checkBudget(worker)
	if n := budgetNotices(t, d, launcher.ID); n != 0 {
		t.Fatalf("told at $10 of a $25 budget")
	}
	if v := d.budgetFor(worker.ID); v != nil {
		t.Fatalf("shown at $10 of a $25 budget: %v", v)
	}

	spend(t, d, worker.ID, 2)
	d.checkBudget(worker)
	spend(t, d, worker.ID, 1)
	d.checkBudget(worker)
	if n := budgetNotices(t, d, launcher.ID); n != 1 {
		t.Fatalf("%d budget notices, want exactly one", n)
	}
	v, ok := d.budgetFor(worker.ID).(*BudgetView)
	if !ok || v.BudgetUSD != 25 || v.SpentUSD < 39.9 || v.SpentUSD > 40.1 {
		t.Fatalf("the card shows %+v, want $40 against $25", d.budgetFor(worker.ID))
	}
	// Nothing was stopped.
	if got, _ := d.st.Get(worker.ID); got.ParkedAt != nil || got.Status == store.StatusDone {
		t.Fatalf("the budget acted on the card: %+v", got)
	}
}

func TestTheBudgetNeverTouchesACardTheOperatorStarted(t *testing.T) {
	d := testDaemon(t)
	own := peerCard(t, d, "mine")
	setPolicy(t, d, store.WorkerPolicy{BudgetUSD: 1})
	spend(t, d, own.ID, 5)
	d.checkBudget(own)
	if v := d.budgetFor(own.ID); v != nil {
		t.Fatalf("the operator's own card shows %v", v)
	}
}

func TestACardBackUnderTheBudgetStopsShowingIt(t *testing.T) {
	d := testDaemon(t)
	_, worker := holdingPair(t, d, OrchestratorTag)
	setPolicy(t, d, store.WorkerPolicy{BudgetUSD: 5})
	spend(t, d, worker.ID, 1)
	d.checkBudget(worker)
	if d.budgetFor(worker.ID) == nil {
		t.Fatal("not shown past the budget")
	}
	setPolicy(t, d, store.WorkerPolicy{})
	d.checkBudget(worker)
	if v := d.budgetFor(worker.ID); v != nil {
		t.Fatalf("still shown with the budget off: %v", v)
	}
}

func TestTheDefaultWorkerModelOnlyReachesAFreshAgentLaunchThatNamedNone(t *testing.T) {
	d := testDaemon(t)
	claude := &store.Harness{ID: "claude"}
	agent := LaunchRequest{Tags: []string{OriginAgentTag}}
	human := LaunchRequest{}

	if got := d.workerDefaultModel(claude, agent, nil, ""); got != "" {
		t.Fatalf("model %q with the setting empty", got)
	}
	setPolicy(t, d, store.WorkerPolicy{Model: "sonnet"})
	if got := d.workerDefaultModel(claude, agent, nil, ""); got != "sonnet" {
		t.Fatalf("an agent launch got %q, want sonnet", got)
	}
	if got := d.workerDefaultModel(claude, human, nil, "someone"); got != "sonnet" {
		t.Fatalf("a launch with a report-to got %q, want sonnet", got)
	}
	if got := d.workerDefaultModel(claude, human, nil, ""); got != "" {
		t.Fatalf("the operator's own launch got %q", got)
	}
	resume := agent
	resume.Resume = "abc"
	if got := d.workerDefaultModel(claude, resume, nil, ""); got != "" {
		t.Fatalf("a resume got %q", got)
	}
	if got := d.workerDefaultModel(claude, agent, &store.Task{}, ""); got != "" {
		t.Fatalf("a card that already exists got %q", got)
	}
	if got := d.workerDefaultModel(&store.Harness{ID: "codex", Cmd: "codex"}, agent, nil, ""); got != "" {
		t.Fatalf("a runner that is not claude got %q", got)
	}
}
