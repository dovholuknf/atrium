//go:build windows

package daemon

import (
	"os/exec"
	"syscall"
)

// createNoWindow is the flag that stops a console process opening a console.
//
// Not in `syscall` as a named constant, so it is spelled here once rather than
// as a literal at a call site where nobody would recognise it.
const createNoWindow = 0x08000000

// hideWindow stops a spawned command flashing a console window on screen.
//
// A source is a command on a timer, and the one that found this ran
// `npm view @openai/codex version` every ten minutes, so every tick opened a
// console window on the operator's desktop, in front of whatever he was doing,
// for as long as the command took. There is nothing to read in it: the output
// is parsed by the daemon and what happened is reported on the source's own
// row.
//
// THIS COVERS THE CHILD AND NOT ITS CHILDREN, which is the limit worth knowing
// about. A source spawned as `pwsh` is quiet, and anything that script shells
// out to allocates its own console. That is why the runner version check was
// moved into the daemon and reads a file instead: see runnerupdate.go.
//
// The daemon is usually started without a console of its own, so a console
// child gets a NEW one rather than inheriting one that is already there. That
// is the window. `HideWindow` alone is not enough, because the console is
// allocated before anything is asked about how to show it.
//
// DELIBERATELY NOT APPLIED TO A RUNNER. Window launch mode exists to put a
// real terminal on screen, and `docs/supervision-design.md` is about the case
// where atrium owns the terminal instead. Both are windows somebody asked for.
// This is for the commands nobody asked to watch.
func hideWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}
