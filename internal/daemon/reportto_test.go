package daemon

import (
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// r-014: a launch that names who its reports go to.

// reviewCard is a session other cards can report to as `review`.
func reviewCard(t *testing.T, d *Daemon, name string) *store.Task {
	t.Helper()
	c := peerCard(t, d, name)
	if err := d.st.SetAlias(c.ID, "review"); err != nil {
		t.Fatal(err)
	}
	return c
}

// launchTo starts a slow runner that reports to `to`.
func launchTo(t *testing.T, d *Daemon, to string) *store.Task {
	t.Helper()
	h := slowHarness(t, d)
	task, err := d.Launch(LaunchRequest{Harness: h, Cwd: t.TempDir(), ReportTo: to})
	if err != nil {
		t.Skipf("could not spawn a test runner on this machine: %v", err)
	}
	t.Cleanup(func() { _ = d.StopRunner(task.ID) })
	return task
}

func TestAReportToLaunchSetsTheLauncherAndKeepsTheName(t *testing.T) {
	for _, name := range []string{"review", "@review"} {
		d := testDaemon(t)
		rev := reviewCard(t, d, "reviewer")
		task := launchTo(t, d, name)
		got, _ := d.st.Get(task.ID)
		if got.SpawnedByID != rev.ID || got.SpawnedBy != rev.WireName {
			t.Fatalf("%s: launcher %q / %q, want %q / %q", name, got.SpawnedBy, got.SpawnedByID, rev.WireName, rev.ID)
		}
		if d.st.ReportTo(got.ID) != name {
			t.Fatalf("report_to kept as %q, want %q", d.st.ReportTo(got.ID), name)
		}
		// It sets the launcher and nothing else.
		if hasTag(got.Tags, OriginAgentTag) || len(got.Tags) != 0 {
			t.Fatalf("tags %v, want none: report_to must not add origin:agent", got.Tags)
		}
		if !d.reportsToLauncher(got) {
			t.Fatal("the card does not report to its launcher")
		}
	}
	// A full handle works too.
	d := testDaemon(t)
	rev := reviewCard(t, d, "reviewer")
	if got := launchTo(t, d, rev.WireName); got.SpawnedByID != rev.ID {
		t.Fatalf("by handle: launcher %q", got.SpawnedByID)
	}
}

func TestAReportFromAReportToCardReachesItsLauncher(t *testing.T) {
	d := testDaemon(t)
	rev := reviewCard(t, d, "reviewer")
	task := launchTo(t, d, "review")
	rec, out := finishWith(t, d, FinishRequest{Agent: task.WireName, TaskID: task.ID, Status: ReportDone,
		NoCommit: "review only", Recap: "looks fine"})
	if rec.Code != 200 || out["launcher_told"] != true {
		t.Fatalf("report answered %d: %s", rec.Code, rec.Body)
	}
	msgs := pendingFrom(t, d, rev.ID)
	if len(msgs) != 1 || !strings.Contains(msgs[0].Text, "looks fine") {
		t.Fatalf("@review has %v", msgs)
	}
}

func TestASilentStopAndOwedReportCountAgainstTheReportToCard(t *testing.T) {
	d := testDaemon(t)
	rev := reviewCard(t, d, "reviewer")
	task := launchTo(t, d, "review")
	prompt(t, d, task.ID)
	got, _ := d.st.Get(task.ID)
	if !got.OwesReport() {
		t.Fatal("a report_to card that was prompted owes @review a report")
	}
	stopTwice(t, d, task.WireName)
	msgs := pendingFrom(t, d, rev.ID)
	if len(msgs) != 1 || !strings.Contains(msgs[0].Text, "without reporting") {
		t.Fatalf("@review has %v, want one silent-stop notice", msgs)
	}
}

func TestAnUnknownOrForeignReportToRefusesAndCreatesNoCard(t *testing.T) {
	d := testDaemon(t)
	reviewCard(t, d, "reviewer")
	h := slowHarness(t, d)
	before, _ := d.st.List()
	for _, c := range []struct{ to, want string }{
		{"nobody", "reviewer"},
		{"review@claude-sg4", "another room"},
		{"claude-sg4~L1", "another room"},
	} {
		_, err := d.Launch(LaunchRequest{Harness: h, Cwd: t.TempDir(), ReportTo: c.to})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: err %v, want it to mention %q", c.to, err, c.want)
		}
	}
	if after, _ := d.st.List(); len(after) != len(before) {
		t.Fatalf("a refused launch left %d cards", len(after)-len(before))
	}
}

func TestAnAgentLaunchCannotCarryReportTo(t *testing.T) {
	d := testDaemon(t)
	reviewCard(t, d, "reviewer")
	h := slowHarness(t, d)
	_, err := d.Launch(LaunchRequest{Harness: h, Cwd: t.TempDir(), ReportTo: "review",
		Tags: []string{OriginAgentTag}, SpawnedBy: "orchestrator"})
	if err == nil || !strings.Contains(err.Error(), "board") {
		t.Fatalf("err %v, want a refusal", err)
	}
	// The MCP tool has no such field, so its launcher is always its caller.
}

func TestARelaunchedTargetIsReachedByItsAliasAndTheStoredIDIsTheFallback(t *testing.T) {
	d := testDaemon(t)
	old := reviewCard(t, d, "reviewer")
	task := launchTo(t, d, "review")

	// The director is relaunched: a new card takes the alias.
	if err := d.st.SetAlias(old.ID, ""); err != nil {
		t.Fatal(err)
	}
	fresh := peerCard(t, d, "reviewer2")
	if err := d.st.SetAlias(fresh.ID, "review"); err != nil {
		t.Fatal(err)
	}
	rec, out := finishWith(t, d, FinishRequest{Agent: task.WireName, TaskID: task.ID, Status: ReportDone,
		NoCommit: "x", Recap: "second"})
	if rec.Code != 200 || out["launcher_told"] != true {
		t.Fatalf("report answered %d: %s", rec.Code, rec.Body)
	}

	if n := len(pendingFrom(t, d, fresh.ID)); n != 1 {
		t.Fatalf("the new card has %d messages, want the report", n)
	}
	if n := len(pendingFrom(t, d, old.ID)); n != 0 {
		t.Fatalf("the old card has %d messages", n)
	}
	got, _ := d.st.Get(task.ID)
	if got.SpawnedByID != fresh.ID {
		t.Fatalf("launcher %q, want the new card recorded", got.SpawnedByID)
	}

	// With the name gone entirely, the stored id still answers.
	d2 := testDaemon(t)
	only := reviewCard(t, d2, "reviewer")
	task2 := launchTo(t, d2, "review")
	if err := d2.st.SetAlias(only.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, out := finishWith(t, d2, FinishRequest{Agent: task2.WireName, TaskID: task2.ID, Status: ReportDone,
		NoCommit: "x", Recap: "third"}); out["launcher_told"] != true {
		t.Fatalf("with the name gone the report was not delivered: %v", out)
	}
	if n := len(pendingFrom(t, d2, only.ID)); n != 1 {
		t.Fatalf("the stored card has %d messages, want the report", n)
	}
}

func TestAReportToChildIsNotCountedAsAnAgentSession(t *testing.T) {
	d := testDaemon(t)
	reviewCard(t, d, "reviewer")
	got, _ := d.st.Get(launchTo(t, d, "review").ID)
	// The launch cap and a director's worker limit count cards carrying the
	// agent and subagent tags. This one carries neither.
	if hasTag(got.Tags, OriginAgentTag) || hasTag(got.Tags, SubagentTag) {
		t.Fatalf("tags %v would count against the launcher's limits", got.Tags)
	}
}
