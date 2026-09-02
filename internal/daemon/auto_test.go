package daemon

import (
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/hub"
	"github.com/dovholuknf/atrium/internal/store"
)

func autoTask(t *testing.T, d *Daemon, name string) *store.Task {
	t.Helper()
	task, _, err := d.st.Register(store.Observed{
		WireName: name, Worktree: "/tmp/atrium-test", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetAutoApprove(task.ID, true); err != nil {
		t.Fatal(err)
	}
	return task
}

func ask(t *testing.T, d *Daemon, agent, tool, command string) (string, *hub.AutoDecision) {
	t.Helper()
	id, auto, err := d.onPermRequest(hub.PermissionRequest{
		Agent: agent, Tool: tool, Command: command,
	})
	if err != nil {
		t.Fatalf("permission request: %v", err)
	}
	return id, auto
}

// The whole point: nothing is asked, and the agent is not left waiting.
func TestAutoModeApprovesWithoutAsking(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	task := autoTask(t, d, "auto-1")

	_, auto := ask(t, d, "auto-1", "Bash", "go test ./...")
	if auto == nil {
		t.Fatal("auto mode left the request for a human")
	}
	if auto.Decision != "approve" {
		t.Fatalf("auto mode answered %q, wanted approve", auto.Decision)
	}

	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status == store.StatusNeedsPermission {
		t.Fatal("the card moved to needs-permission even though nothing was asked")
	}
}

// Recording is the trade auto mode makes. Approving without a record would be
// the same as not gating at all, and the review that justifies the mode would
// have nothing to read.
func TestAutoModeStillRecordsEverything(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	task := autoTask(t, d, "auto-2")

	ask(t, d, "auto-2", "Bash", "rm -rf build")

	rev, err := d.st.ReviewTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rev.Total != 1 {
		t.Fatalf("the review has %d decisions, wanted 1", rev.Total)
	}
	if rev.Unattended != 1 {
		t.Fatalf("%d decisions marked unattended, wanted 1", rev.Unattended)
	}
	if len(rev.Groups) != 1 || rev.Groups[0].Entries[0].Command != "rm -rf build" {
		t.Fatalf("the command did not survive into the review: %+v", rev.Groups)
	}
	if rev.Groups[0].Entries[0].By != "auto" {
		t.Fatalf("decided by %q, wanted auto so the review can tell them apart",
			rev.Groups[0].Entries[0].By)
	}
}

// Auto mode says stop asking me new questions, not forget the answers I gave.
// A never rule is an answer already given.
func TestAutoModeDoesNotOverrideANeverRule(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	autoTask(t, d, "auto-3")

	if _, err := d.st.AddRule("Bash", "taskkill", "block",
		"never kill things behind my back", ""); err != nil {
		t.Fatal(err)
	}

	_, auto := ask(t, d, "auto-3", "Bash", "taskkill /F /IM claude.exe")
	if auto == nil {
		t.Fatal("the request went to a human even though a rule covered it")
	}
	if auto.Decision != "block" {
		t.Fatalf("auto mode overrode a never rule: got %q", auto.Decision)
	}
}

// A shelved card is a standing no, and shelving is explicit. Auto mode must not
// quietly reopen work the operator deliberately put down.
func TestAutoModeDoesNotOverrideShelving(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	task := autoTask(t, d, "auto-4")
	if err := d.st.SetStatus(task.ID, store.StatusShelved); err != nil {
		t.Fatal(err)
	}

	_, auto := ask(t, d, "auto-4", "Bash", "go build ./...")
	if auto == nil || auto.Decision != "block" {
		t.Fatalf("a shelved card let a request through under auto mode: %+v", auto)
	}
}

// A message is the operator reaching out. Auto mode means do not interrupt me,
// not do not let me speak.
func TestAutoModeStillDeliversMessages(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	task := autoTask(t, d, "auto-5")

	if _, err := d.st.QueueMessage(task.ID, "switch to the other branch first"); err != nil {
		t.Fatal(err)
	}

	_, auto := ask(t, d, "auto-5", "Bash", "go test ./...")
	if auto == nil {
		t.Fatal("the request went to a human under auto mode")
	}
	if auto.Decision != "block" {
		t.Fatalf("a queued message did not reach the session: %+v", auto)
	}
	if !strings.Contains(auto.Reason, "other branch") {
		t.Fatalf("the message text did not ride the tool call: %q", auto.Reason)
	}
}

// Turning the mode off has to put the asking back.
func TestAutoModeOffAsksAgain(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	task := autoTask(t, d, "auto-6")
	if err := d.st.SetAutoApprove(task.ID, false); err != nil {
		t.Fatal(err)
	}

	_, auto := ask(t, d, "auto-6", "Bash", "go test ./...")
	if auto != nil {
		t.Fatalf("auto mode answered after being switched off: %+v", auto)
	}
	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.StatusNeedsPermission {
		t.Fatalf("the card is %s, wanted needs-permission", got.Status)
	}
}

// Auto mode is a decision, so it has to survive a restart. Losing it would
// silently start interrupting again, which is the failure the operator would
// notice last and trust least.
func TestAutoModeSurvivesAReopen(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	task := autoTask(t, d, "auto-7")

	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.AutoApprove {
		t.Fatal("auto mode was not written down")
	}
}
