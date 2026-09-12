package api

import (
	"errors"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/dovholuknf/atrium/internal/safepath"
	"github.com/dovholuknf/atrium/internal/store"
)

// Opening a card's directory in a real terminal window, on the machine the
// session runs on.
//
// NOT the pane's shell, and the difference is the whole reason this is an
// action rather than a runner. `wt.exe` is a terminal EMULATOR: it makes its
// own window with its own ConPTY, hosts a shell inside it, and returns
// immediately. A supervisor that spawned it would hold a pty nothing writes
// to, an empty pane, and a process that exits within a second, which the
// reaper would correctly file as a dead card.
//
// So this is `editor_command` with a different setting, and it carries the
// same three fences:
//
//  1. **Off until configured.** There is no default terminal. Guessing one
//     means atrium picks a program to run on your machine.
//  2. **The operator writes the command, and it is never a shell.** The
//     configured string is split into a program and its arguments and handed
//     to `exec.Command`, so nothing in a PATH can become a second command. A
//     directory called `x; shutdown` is one argument called `x; shutdown`.
//  3. **The path is resolved through `internal/safepath` first**, against the
//     card's own worktree, so what is handed to the program is the resolved
//     directory and not whatever string the card is carrying.
//
// And the fourth thing, which is a caveat for an editor and the whole point
// here: THE DAEMON RUNS THE COMMAND, so the window appears wherever the daemon
// is. A board open on a phone or over a share cannot open a terminal on the
// phone and must not try. The machine with the files is the machine that gets
// the window, and that is the machine the agent is working on.
//
// `{path}` in the command is the directory. A command without it gets the
// directory appended, the same as the editor, because that is the obvious
// spelling for a program that takes one.

// SettingTerminal names the command that opens a directory in a terminal
// window. Empty means this endpoint is off, which is the default and is the
// state that requires no trust.
const SettingTerminal = "terminal_command"

func (s *Server) openTerminal(w http.ResponseWriter, r *http.Request) {
	task, err := s.st.Get(r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	if strings.TrimSpace(task.Worktree) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("this card has no directory"))
		return
	}

	tmpl, err := s.st.Setting(SettingTerminal)
	if err != nil {
		s.fail(w, err)
		return
	}
	tmpl = strings.TrimSpace(tmpl)
	if tmpl == "" {
		writeErr(w, http.StatusBadRequest, errors.New(
			"no terminal is configured. set `terminal_command` in settings, for example "+
				"`wt.exe -d {path}`, and remember the window opens on the machine atrium "+
				"is on"))
		return
	}

	// The card's own directory, resolved rather than trusted. Symlinks are
	// followed on both sides, so what the program is handed is a real
	// directory, and a worktree that has been deleted or replaced by a file
	// fails here rather than as an argument to a terminal.
	dir, err := safepath.Contained(filepath.FromSlash(task.Worktree), ".")
	if err != nil {
		writeErr(w, http.StatusForbidden, safepath.ErrOutside)
		return
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		writeErr(w, http.StatusBadRequest, errors.New(
			"this card's directory is not there any more"))
		return
	}

	// The same splitter the editor uses, for the same reason. One grammar for
	// "a program and its arguments" means one place where it could grow into a
	// shell, and it must not.
	name, args := editorCommand(tmpl, dir)
	cmd := exec.Command(name, args...)
	// Started IN the directory as well as pointed at it, so a terminal command
	// with no placeholder at all still opens in the right place.
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New(
			"could not run "+name+": "+err.Error()))
		return
	}
	// Not waited on. A terminal window outlives this request by design, and
	// `wt.exe` in particular hands off to the running Windows Terminal and
	// exits at once. The process is released rather than left as a zombie.
	go func() { _ = cmd.Wait() }()

	// Recorded, and a failure to record is not a failure to open: the window
	// is already up.
	if err := s.st.AppendEvent(task.ID, store.EventNotified, map[string]any{
		"what": "opened this card's directory in a terminal", "path": dir, "with": name,
	}); err != nil {
		log.Printf("[atrium api] could not record the terminal open of %s: %v", dir, err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "opened": dir})
}
