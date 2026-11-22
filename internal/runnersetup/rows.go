package runnersetup

import "github.com/dovholuknf/atrium/internal/base/setupcheck"

// Rows is a runner's report as the setup list's rows: runner.<id>.installed, then one row per check, runner.<id>.<check>.
// The states and fix kinds are runnersetup's own, so the two lists never say different things about one runner.
func Rows(id string, rep Report) []setupcheck.Result {
	inst := setupcheck.Result{ID: "runner." + id + ".installed", Label: rep.Label + " is installed"}
	if rep.Installed {
		inst.State, inst.Detail = setupcheck.OK, rep.Exe
		if rep.Version != "" {
			inst.Detail += " " + rep.Version
		}
	} else {
		inst.State, inst.Fix = setupcheck.Warn, setupcheck.FixExplain
		inst.Detail = rep.Label + " is not on this account's PATH. install it, then check again"
	}
	rows := []setupcheck.Result{inst}
	if !rep.Installed {
		return rows
	}
	for _, c := range rep.Checks {
		rows = append(rows, setupcheck.Result{ID: "runner." + id + "." + c.ID, Label: c.Label, State: setupcheck.State(c.State),
			Detail: c.Detail, Fix: c.Fix, Command: c.Command})
	}
	return rows
}
