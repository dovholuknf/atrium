package claudeconf

import (
	"strings"
	"testing"
)

const testExe = "C:/tools/atrium.exe"

const dotfilesGate = "pwsh -NoProfile -File C:/Users/claude/.claude/hooks/atrium-perm-hook.ps1"

func gateRow(t *testing.T, rep *HookReport) HookStatus {
	t.Helper()
	for _, h := range rep.Hooks {
		if h.Event == permissionEvent {
			return h
		}
	}
	t.Fatal("no permission row in the report")
	return HookStatus{}
}

// hooksOf digs the registered command maps for one hook name out of a written file.
func hooksOf(t *testing.T, path, hook string) []map[string]any {
	t.Helper()
	doc := readSettings(t, path)
	var out []map[string]any
	all, _ := doc["hooks"].(map[string]any)
	entries, _ := all[hook].([]any)
	for _, e := range entries {
		list, _ := e.(map[string]any)["hooks"].([]any)
		for _, h := range list {
			out = append(out, h.(map[string]any))
		}
	}
	return out
}

func timeoutFor(t *testing.T, path, hook, contains string) (float64, bool) {
	t.Helper()
	for _, h := range hooksOf(t, path, hook) {
		if s, _ := h["command"].(string); strings.Contains(s, contains) {
			n, ok := h["timeout"].(float64)
			return n, ok
		}
	}
	t.Fatalf("no %s command containing %q", hook, contains)
	return 0, false
}

func TestInstallAllWritesTheGateWithADayTimeout(t *testing.T) {
	path := withHome(t, "")
	rep, _, err := Install(testExe)
	if err != nil {
		t.Fatal(err)
	}
	g := gateRow(t, rep)
	if !g.Installed || g.TimeoutShort || g.Other != "" {
		t.Fatalf("gate row after install all: %+v", g)
	}
	if n, ok := timeoutFor(t, path, "PreToolUse", "--event permission"); !ok || n != 86400 {
		t.Fatalf("gate timeout is %v (present %v), wanted 86400", n, ok)
	}
	if rep.Missing != 0 {
		t.Fatalf("missing is %d after install all", rep.Missing)
	}
}

func TestEveryOtherHookGetsNoTimeout(t *testing.T) {
	path := withHome(t, "")
	if _, _, err := Install(testExe); err != nil {
		t.Fatal(err)
	}
	for _, w := range WantedHooks {
		if w.Event == permissionEvent || w.Optional {
			continue
		}
		if w.Timeout != 0 {
			t.Fatalf("%s has a wanted timeout of %d", w.Event, w.Timeout)
		}
		for _, h := range hooksOf(t, path, w.Hook) {
			if s, _ := h["command"].(string); strings.Contains(s, "--event "+w.Arg) {
				if _, has := h["timeout"]; has {
					t.Fatalf("%s was written with a timeout", w.Event)
				}
			}
		}
	}
}

func TestAShortGateTimeoutIsDriftAndInstallRaisesItKeepingTheMatcher(t *testing.T) {
	path := withHome(t, `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command",`+
		`"command":"`+testExe+` hook --event permission","timeout":60}]}]}}`)

	rep, err := Inspect(testExe)
	if err != nil {
		t.Fatal(err)
	}
	g := gateRow(t, rep)
	if !g.Installed || !g.TimeoutShort {
		t.Fatalf("a 60s gate should be installed and timeout_short: %+v", g)
	}
	// Every other row is missing too, so only check the gate adds one.
	if rep.Missing != wantedCount() {
		t.Fatalf("missing is %d, wanted %d", rep.Missing, wantedCount())
	}

	if _, res, err := InstallOnly(testExe, []string{permissionEvent}); err != nil || !res.Changed {
		t.Fatalf("install: changed=%v err=%v", res.Changed, err)
	}
	if n, _ := timeoutFor(t, path, "PreToolUse", "--event permission"); n != 86400 {
		t.Fatalf("timeout is %v after install, wanted 86400", n)
	}
	doc := readSettings(t, path)
	entry := doc["hooks"].(map[string]any)["PreToolUse"].([]any)[0].(map[string]any)
	if entry["matcher"] != "Bash" {
		t.Fatalf("the matcher was lost: %v", entry["matcher"])
	}
}

func TestAMissingGateTimeoutIsDrift(t *testing.T) {
	withHome(t, `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command",`+
		`"command":"`+testExe+` hook --event permission"}]}]}}`)
	rep, _ := Inspect(testExe)
	if !gateRow(t, rep).TimeoutShort {
		t.Fatal("a gate with no timeout should be timeout_short")
	}
}

func TestALongerGateTimeoutIsLeftAloneAndWritesNothing(t *testing.T) {
	path := withHome(t, `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command",`+
		`"command":"`+testExe+` hook --event permission","timeout":90000}]}]}}`)

	rep, _ := Inspect(testExe)
	if gateRow(t, rep).TimeoutShort {
		t.Fatal("90000 is longer than wanted and is not drift")
	}
	_, res, err := InstallOnly(testExe, []string{permissionEvent})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Backup != "" {
		t.Fatalf("an already right gate was rewritten: %+v", res)
	}
	if n, _ := timeoutFor(t, path, "PreToolUse", "--event permission"); n != 90000 {
		t.Fatalf("timeout became %v", n)
	}
}

func TestTheDotfilesGateCountsAsPresentAndIsNeverRewritten(t *testing.T) {
	contents := `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"` + dotfilesGate +
		`","timeout":86400}]}]}}`
	path := withHome(t, contents)

	rep, err := Inspect(testExe)
	if err != nil {
		t.Fatal(err)
	}
	g := gateRow(t, rep)
	if !g.Installed || g.Stale || g.TimeoutShort || g.Found != dotfilesGate || g.Other == "" || g.TwoGates {
		t.Fatalf("gate row with the script registered: %+v", g)
	}
	if rep.Missing != wantedCount()-1 {
		t.Fatalf("missing is %d, wanted %d: the gate must not count", rep.Missing, wantedCount()-1)
	}

	// Install all writes the other hooks and passes over the gate.
	after, _, err := Install(testExe)
	if err != nil {
		t.Fatal(err)
	}
	if after.Missing != 0 {
		t.Fatalf("missing is %d after install all", after.Missing)
	}
	for _, h := range hooksOf(t, path, "PreToolUse") {
		if s, _ := h["command"].(string); strings.Contains(s, "--event permission") {
			t.Fatal("install all wrote atrium's gate beside the dotfiles one")
		}
	}

	// By name it refuses, and says why.
	_, _, err = InstallOnly(testExe, []string{permissionEvent})
	if err == nil || !strings.Contains(err.Error(), "twice") {
		t.Fatalf("installing the gate by name over the script answered %v", err)
	}
}

func TestBothGatesRegisteredIsReported(t *testing.T) {
	withHome(t, `{"hooks":{"PreToolUse":[{"hooks":[`+
		`{"type":"command","command":"`+dotfilesGate+`","timeout":86400},`+
		`{"type":"command","command":"`+testExe+` hook --event permission","timeout":86400}]}]}}`)
	rep, err := Inspect(testExe)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.TwoGates || !gateRow(t, rep).TwoGates {
		t.Fatalf("two gates were not reported: %+v", gateRow(t, rep))
	}
}

func TestTheCodexTargetHasNoGateRow(t *testing.T) {
	for _, w := range Codex.Wanted {
		if w.Event == permissionEvent {
			t.Fatal("codex carries the permission row")
		}
	}
}
