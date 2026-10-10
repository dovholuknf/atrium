//go:build integration

package daemon

import (
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// permGate reads ATRIUM_PERM_GATE out of a built child environment. Last
// occurrence wins in a process environment, so it scans to the end.
func permGate(env []string) (string, bool) {
	val, ok := "", false
	for _, kv := range env {
		if rest, found := strings.CutPrefix(kv, "ATRIUM_PERM_GATE="); found {
			val, ok = rest, true
		}
	}
	return val, ok
}

// launchEnv mirrors what launchLocked builds for a runner: atrium's own values
// plus the perm-gate default, unless the harness named the gate itself. Kept in
// step with the block in Launch that adds ATRIUM_PERM_GATE to the atrium map.
func launchEnv(harnessEnv map[string]string) []string {
	atrium := map[string]string{"ATRIUM_AGENT_NAME": "doer"}
	if gate, ok := permGateDefault(harnessEnv); ok {
		atrium["ATRIUM_PERM_GATE"] = gate
	}
	return childEnv(harnessEnv, atrium)
}

// The stall this fixes: a launched runner whose cwd has no atrium-agent MCP
// runs with the gate unset, so its Bash approvals are claude's own prompts and
// the board-wide switch never sees them. Launching with the gate on routes them
// to atrium instead, so the one board switch controls them.
func TestLaunchGatesTheRunnerByDefault(t *testing.T) {
	env := launchEnv(nil)
	got, ok := permGate(env)
	if !ok {
		t.Fatal("a launched runner had no ATRIUM_PERM_GATE, so its approvals never reach atrium's gate")
	}
	if got != "on" {
		t.Fatalf("ATRIUM_PERM_GATE is %q, want on so the runner routes through atrium's gate", got)
	}
}

// A default, not an override. An operator who set ATRIUM_PERM_GATE=off on a
// runner meant it, so that runner stays ungated.
func TestLaunchHonorsAHarnessGateSetting(t *testing.T) {
	if _, ok := permGateDefault(map[string]string{"ATRIUM_PERM_GATE": "off"}); ok {
		t.Fatal("the launch default overrode a harness that set the gate itself")
	}
	// Matched by name whatever its case, since that is what decides which env
	// entry wins in the child.
	if _, ok := permGateDefault(map[string]string{"atrium_perm_gate": "off"}); ok {
		t.Fatal("a lower-case harness setting was not recognised, so the default would collide with it")
	}
	if got, ok := permGateDefault(nil); !ok || got != "on" {
		t.Fatalf("with no harness setting the launch supplies %q (ok=%v), want on/true", got, ok)
	}
}

// End to end for the thing the operator saw: a session he never set up
// individually, routed through the gate at launch, is turned loose by the
// board-wide switch the moment it asks, and still gates to him when it is off.
// The launch env carries the request to the gate; the gate is what decides.
func TestLaunchedSessionUnderGlobalAuto(t *testing.T) {
	if _, ok := permGate(launchEnv(nil)); !ok {
		t.Fatal("the launch would not route this session through atrium's gate at all")
	}

	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	task := plainTask(t, d, "doer")

	// Auto ON: approved without a human.
	if err := d.st.SetGlobalAuto(true); err != nil {
		t.Fatal(err)
	}
	if _, auto := ask(t, d, "doer", "Bash", "go build ./..."); auto == nil || auto.Decision != "approve" {
		t.Fatalf("board-wide auto did not cover the launched session: %+v", auto)
	}

	// Auto OFF: still gates to the operator, unchanged.
	if err := d.st.SetGlobalAuto(false); err != nil {
		t.Fatal(err)
	}
	_, auto := ask(t, d, "doer", "Bash", "ssh host dpkg -i pkg.deb")
	if auto != nil {
		t.Fatalf("a launched session stopped gating with auto off: %+v", auto)
	}
	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.StatusNeedsPermission {
		t.Fatalf("the card is %s, want needs-permission with auto off", got.Status)
	}
}
