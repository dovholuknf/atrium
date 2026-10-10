//go:build integration

package daemon

import (
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

const (
	mcpSearch = "mcp__mercurius__discourse_discourse_search"
	mcpCreate = "mcp__mercurius__discourse_discourse_create_user"
)

func mcpTask(t *testing.T, d *Daemon, name string) *store.Task {
	t.Helper()
	task, _, err := d.st.Register(store.Observed{
		WireName: name, Worktree: "/tmp/atrium-test", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func mcpRule(t *testing.T, d *Daemon, tool, decision string) {
	t.Helper()
	if _, err := d.st.AddMCPRule(tool, decision, "", ""); err != nil {
		t.Fatal(err)
	}
}

// An MCP rule is a standing rule, step 4, and answers like any other one.
func TestMCPRuleAnswersAtTheStandingRuleStep(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	mcpTask(t, d, "gw-1")
	mcpRule(t, d, "mcp__mercurius__*", "approve")
	mcpRule(t, d, mcpCreate, "block")

	id, auto := ask(t, d, "gw-1", mcpSearch, `{"query":"x"}`)
	if auto == nil || auto.Decision != "approve" {
		t.Fatalf("a covered read was not approved: %+v", auto)
	}
	if p, err := d.st.GetPermission(id); err != nil || p.DecidedBy != "mcp__mercurius__*" {
		t.Fatalf("the audit row should name the tool pattern, got %+v %v", p, err)
	}

	_, auto = ask(t, d, "gw-1", mcpCreate, `{"username":"a"}`)
	if auto == nil || auto.Decision != "block" {
		t.Fatalf("the write tool was not blocked: %+v", auto)
	}
}

// The decider for an ordinary rule is still its pattern.
func TestNonMCPRuleKeepsItsPatternAsDecider(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	mcpTask(t, d, "plain-1")
	if _, err := d.st.AddRule("Bash", "go build", "approve", "", ""); err != nil {
		t.Fatal(err)
	}
	id, auto := ask(t, d, "plain-1", "Bash", "go build ./...")
	if auto == nil || auto.Decision != "approve" {
		t.Fatalf("not approved: %+v", auto)
	}
	if p, _ := d.st.GetPermission(id); p == nil || p.DecidedBy != "go build" {
		t.Fatalf("decider changed for a non-MCP rule: %+v", p)
	}
}

// Steps 2 and 3 still sit ahead of a rule, and step 5 behind it.
func TestMCPRuleChainOrder(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	// A queued message beats an approving rule.
	msg := mcpTask(t, d, "gw-msg")
	mcpRule(t, d, "mcp__mercurius__*", "approve")
	if _, err := d.st.QueueMessage(msg.ID, "stop and read this"); err != nil {
		t.Fatal(err)
	}
	_, auto := ask(t, d, "gw-msg", mcpSearch, `{}`)
	if auto == nil || auto.Decision != "block" {
		t.Fatalf("a queued message did not come first: %+v", auto)
	}

	// A shelved card blocks even an approved tool.
	shelved := mcpTask(t, d, "gw-shelved")
	if err := d.st.SetStatus(shelved.ID, store.StatusShelved); err != nil {
		t.Fatal(err)
	}
	_, auto = ask(t, d, "gw-shelved", mcpSearch, `{}`)
	if auto == nil || auto.Decision != "block" {
		t.Fatalf("a shelved card did not block: %+v", auto)
	}

	// Auto mode does not undo a deny.
	loose := autoTask(t, d, "gw-auto")
	_ = loose
	mcpRule(t, d, mcpCreate, "block")
	_, auto = ask(t, d, "gw-auto", mcpCreate, `{}`)
	if auto == nil || auto.Decision != "block" {
		t.Fatalf("auto mode overrode an MCP deny: %+v", auto)
	}
	// And still approves what no rule answers.
	_, auto = ask(t, d, "gw-auto", "mcp__other__thing", `{}`)
	if auto == nil || auto.Decision != "approve" {
		t.Fatalf("auto mode stopped approving an unruled MCP call: %+v", auto)
	}
}

// Turning auto mode on drains the queued call a rule covers with the rule, and
// leaves an uncovered one for the human.
func TestDrainUsesAnMCPRule(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	task := mcpTask(t, d, "gw-drain")
	mcpRule(t, d, mcpCreate, "block")

	covered, _, err := d.st.RecordPermission(task.ID, mcpCreate, `{"username":"a"}`, "", "")
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := d.st.RecordPermission(task.ID, "mcp__other__thing", `{}`, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetStatus(task.ID, store.StatusNeedsPermission); err != nil {
		t.Fatal(err)
	}
	if _, err := d.drainForAuto(); err != nil {
		t.Fatal(err)
	}
	// The drain never blocks. It declines to approve what a block rule covers, and
	// leaves that request in the queue for the human.
	got, err := d.st.GetPermission(covered.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DecidedAt != nil {
		t.Errorf("the drain answered a call a block rule covers: %q", got.Decision)
	}
	o, err := d.st.GetPermission(other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if o.Decision != "approve" {
		t.Errorf("the uncovered call was answered %q, wanted approve", o.Decision)
	}
}
