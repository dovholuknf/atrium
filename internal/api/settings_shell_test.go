package api

import (
	"net/http"
	"testing"
)

// THE BOX HAS TO ACTUALLY SAVE.
//
// `shell_command` was posted by the board, drawn as a field in the settings
// pane, and read back by two more fields, and it had no member on the struct
// the request body decodes into. So it was dropped on the floor: the write
// answered 200 carrying the OLD value, which reads exactly like a save that
// worked, and the only way to change the setting was to edit the database.
//
// Nothing rejected it either. The guard on this handler is a rule about one
// specific pair of fields, not a check that every key is one the handler knows.
func TestTheShellCommandCanBeSaved(t *testing.T) {
	srv, _, _ := fileServer(t)

	if rec := settingsPost(t, srv, `{"shell_command":"pwsh -NoLogo"}`); rec.Code != http.StatusOK {
		t.Fatalf("saving a shell command answered %d: %s", rec.Code, rec.Body.String())
	}
	if got := settingsGet(t, srv)["shell_command"]; got != "pwsh -NoLogo" {
		t.Fatalf("the shell command read back as %v", got)
	}
}

// OFF IS A VALUE, and it is the switch that stops a machine opening shells.
//
// There used to be one by accident: a shell was a runner, so disabling that row
// turned shells off. Removing the row removed the switch with it, and the only
// way left would have been to name a command that does not exist. `off` is the
// same word the worktree command already takes for the same idea.
func TestShellsCanBeSwitchedOffAndBackOn(t *testing.T) {
	srv, _, _ := fileServer(t)

	if got := settingsGet(t, srv)["shell_command_ok"]; got != true {
		t.Fatalf("a machine with no setting reports no shell: %v", got)
	}

	if rec := settingsPost(t, srv, `{"shell_command":"off"}`); rec.Code != http.StatusOK {
		t.Fatalf("off answered %d: %s", rec.Code, rec.Body.String())
	}
	out := settingsGet(t, srv)
	if out["shell_command"] != "off" {
		t.Fatalf("off read back as %v", out["shell_command"])
	}
	// The board hangs the `agent | shell` control off this, so a machine with
	// shells switched off has to report that it has none. Otherwise the button
	// stays and every press is refused.
	if out["shell_command_ok"] != false {
		t.Fatal("shells are off and the daemon still says one is available")
	}

	// And back on, or the switch is a trap.
	if rec := settingsPost(t, srv, `{"shell_command":""}`); rec.Code != http.StatusOK {
		t.Fatalf("clearing it answered %d: %s", rec.Code, rec.Body.String())
	}
	if got := settingsGet(t, srv)["shell_command_ok"]; got != true {
		t.Fatal("clearing the box did not turn shells back on")
	}
}
