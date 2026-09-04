package api

import (
	"encoding/json"
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

// Opening a file in an editor, on the machine the session runs on.
//
// The obvious reading of this over an overlay is the wrong one, so it is worth
// saying plainly: the DAEMON runs the command, so the window appears wherever
// the daemon is. That is the same machine the agent is editing on, which is the
// point. A board open on a laptop cannot open a file on the laptop, and should
// not try to: the file is not there.
//
// This is the one thing in atrium that starts a process nobody asked for by
// name, so it is fenced on three sides:
//
//  1. **Off until configured.** There is no default editor. Guessing one means
//     atrium picks a program to run on your machine, and the guess being wrong
//     is a process you did not want with a path you did not choose.
//  2. **The operator writes the command, and it is never a shell.** The
//     configured string is split into a program and its arguments and handed to
//     `exec.Command`, so nothing in a FILENAME can become another command. A
//     file called `x; shutdown` is one argument called `x; shutdown`.
//  3. **The path is resolved through `internal/safepath` first**, against the
//     card's own worktree, exactly like download. Everything outside a card is
//     `403` whether or not it exists.
//
// `{path}` in the command is where the file goes. A command without it gets the
// path appended, because that is what every editor on the list does anyway and
// requiring the placeholder would fail for the obvious spelling.

// SettingEditor names the command that opens a file. Empty means this endpoint
// is off, which is the default and is the state that requires no trust.
const SettingEditor = "editor_command"

type openFileRequest struct {
	Path string `json:"path"`
}

func (s *Server) openFile(w http.ResponseWriter, r *http.Request) {
	task, err := s.st.Get(r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	if strings.TrimSpace(task.Worktree) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("this card has no directory"))
		return
	}

	tmpl, err := s.st.Setting(SettingEditor)
	if err != nil {
		s.fail(w, err)
		return
	}
	tmpl = strings.TrimSpace(tmpl)
	if tmpl == "" {
		writeErr(w, http.StatusBadRequest, errors.New(
			"no editor is configured. set one in settings, for example "+
				"`code -g {path}`, and remember it runs on the machine atrium is on"))
		return
	}

	var body openFileRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("could not read that request"))
		return
	}
	if strings.TrimSpace(body.Path) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("no path"))
		return
	}

	// Same containment as download, and the same silence about what is outside:
	// one answer for "not yours" and "not there", so this cannot be used to
	// find out what is on the machine.
	full, err := safepath.Contained(filepath.FromSlash(task.Worktree), body.Path)
	if err != nil {
		writeErr(w, http.StatusForbidden, safepath.ErrOutside)
		return
	}
	if _, err := os.Stat(full); err != nil {
		writeErr(w, http.StatusForbidden, safepath.ErrOutside)
		return
	}

	name, args := editorCommand(tmpl, full)
	cmd := exec.Command(name, args...)
	// Started from the card's directory, so an editor that opens a workspace
	// from its working directory opens the right one.
	cmd.Dir = filepath.FromSlash(task.Worktree)
	if err := cmd.Start(); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New(
			"could not run "+name+": "+err.Error()))
		return
	}
	// Not waited on. An editor outlives this request by design, and holding the
	// handler open until somebody closes their editor is the opposite of what
	// was asked for. The process is released rather than left as a zombie.
	go func() { _ = cmd.Wait() }()

	// Recorded, and a failure to record is not a failure to open: the editor is
	// already up, and answering with an error would say the opposite of what
	// happened.
	if err := s.st.AppendEvent(task.ID, store.EventNotified, map[string]any{
		"what": "opened a file in the editor", "path": body.Path, "with": name,
	}); err != nil {
		log.Printf("[atrium api] could not record the open of %s: %v", body.Path, err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "opened": body.Path})
}

// editorCommand splits the configured string and puts the path in it.
//
// Deliberately not a shell. Handing this to `cmd /c` or `sh -c` would make
// every character in a FILENAME live, and a filename is the part of this that
// atrium does not control. Splitting on spaces, with quoted runs kept whole so
// a program in `C:\Program Files` still works, is the whole grammar.
func editorCommand(tmpl, path string) (string, []string) {
	parts := splitArgs(tmpl)
	if len(parts) == 0 {
		return "", nil
	}
	used := false
	for i, p := range parts {
		if strings.Contains(p, "{path}") {
			parts[i] = strings.ReplaceAll(p, "{path}", path)
			used = true
		}
	}
	// Appended when the placeholder was left out, which is the obvious
	// spelling: `code -g` and `notepad` both mean "and then the file".
	if !used {
		parts = append(parts, path)
	}
	return parts[0], parts[1:]
}

// splitArgs breaks a command line on spaces, keeping quoted runs whole.
//
// Enough for `"C:\Program Files\Microsoft VS Code\code.exe" -g {path}` and no
// more. It is not a shell and must never grow into one: no globbing, no
// variables, no operators, because every one of those would be a way for a
// filename to stop being data.
func splitArgs(s string) []string {
	var out []string
	var cur strings.Builder
	quote := rune(0)
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote == 0 && (r == '"' || r == '\''):
			quote = r
		case quote == 0 && (r == ' ' || r == '\t'):
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}
