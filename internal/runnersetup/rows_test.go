package runnersetup

import (
	"testing"

	"github.com/dovholuknf/atrium/internal/base/setupcheck"
)

func TestRowsFoldARunnerIntoTheSetupList(t *testing.T) {
	rep := Report{Label: "Claude Code", Installed: false, Checks: []Result{
		{ID: "auth", Label: "signed in", State: Warn, Detail: "sign in", Fix: FixExplain, Command: "claude /login"},
		{ID: "hooks", Label: "hooks", State: OK},
	}}
	rows := Rows("claude", rep)
	if len(rows) != 1 || rows[0].ID != "runner.claude.installed" || rows[0].State != setupcheck.Warn || rows[0].Fix != setupcheck.FixExplain {
		t.Fatalf("a runner that is not installed is one warn and no checks: %+v", rows)
	}
	rep.Installed, rep.Exe = true, "/bin/claude"
	rows = Rows("claude", rep)
	if len(rows) != 3 || rows[1].ID != "runner.claude.auth" || rows[1].State != setupcheck.Warn || rows[1].Command != "claude /login" {
		t.Errorf("check row: %+v", rows[1])
	}
	rep.Version = "2.1.0"
	if r := Rows("claude", rep)[0]; r.State != setupcheck.OK || r.Detail != "/bin/claude 2.1.0" {
		t.Errorf("found: %+v", r)
	}
}
