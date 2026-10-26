package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/dovholuknf/atrium/internal/base/safepath"
	"github.com/dovholuknf/atrium/internal/store"
)

// A card's file opened with the default app of the machine the ROOM runs on.
//
// `files/open` runs the operator's editor command. This hands the file to the OS
// instead, which picks what opens it: an image viewer, a PDF reader, a browser. Like
// `files/open`, the window appears on the room's machine and not beside the browser.
//
// THE DEFAULT HANDLER OF A PROGRAM IS TO RUN IT, and an agent writes these files. So
// only documents and pictures are handed over, judged by the extension of the path
// as RESOLVED, so a link called `pic.png` pointing at `x.exe` is refused. A script
// is not on the list whatever its extension does on this machine, because `.js` and
// `.py` run when opened on a Windows box set up the usual way.
var launchTypes = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".bmp": true, ".ico": true,
	".avif": true, ".svg": true, ".pdf": true,
	".md": true, ".markdown": true, ".txt": true, ".log": true, ".csv": true,
	".json": true, ".html": true, ".htm": true,
}

// neverLaunch is refused whatever the setting, with the reason: a program, a script or a shortcut to one. PATHEXT
// adds whatever else this machine's shell runs.
var neverLaunch = map[string]bool{
	".exe": true, ".msi": true, ".bat": true, ".cmd": true, ".com": true, ".ps1": true, ".psm1": true,
	".vbs": true, ".vbe": true, ".js": true, ".jse": true, ".wsf": true, ".wsh": true, ".scr": true, ".lnk": true,
	".url": true, ".reg": true, ".sh": true, ".command": true, ".app": true, ".pkg": true, ".dmg": true,
	".jar": true, ".msc": true, ".cpl": true, ".hta": true, ".pif": true, ".py": true, ".pyw": true,
}

// Executable says whether a file of this extension is one the OS would run.
func Executable(ext string) bool {
	ext = strings.ToLower(ext)
	if neverLaunch[ext] {
		return true
	}
	for _, e := range strings.Split(os.Getenv("PATHEXT"), ";") {
		if e = strings.ToLower(strings.TrimSpace(e)); e != "" && e == ext {
			return true
		}
	}
	return false
}

// SettingOpenExternalAnywhere is `on` when open externally may hand the room's default app a file outside the card
// and the open roots. Unset is off. Only a local operator changes it.
const SettingOpenExternalAnywhere = "open_external_anywhere"

func (s *Server) openExternalAnywhere() bool {
	v, err := s.st.Setting(SettingOpenExternalAnywhere)
	return err == nil && strings.TrimSpace(v) == "on"
}

// Launchable says whether a file of this extension may be opened with the default app.
func Launchable(ext string) bool { return launchTypes[strings.ToLower(ext)] }

// launchTarget resolves a card's path to the file a default app may open, answering the refusal itself.
func (s *Server) launchTarget(w http.ResponseWriter, task *store.Task, rel string) (string, bool) {
	full, _, err := containedOrOpen(filepath.FromSlash(task.Worktree), rel, s.openRoots)
	if err != nil && filepath.IsAbs(rel) && s.openExternalAnywhere() {
		full, err = filepath.EvalSymlinks(filepath.Clean(rel))
	}
	if err != nil {
		writeErr(w, http.StatusForbidden, safepath.ErrOutside)
		return "", false
	}
	fi, err := os.Stat(full)
	if err != nil {
		writeErr(w, http.StatusForbidden, safepath.ErrOutside)
		return "", false
	}
	if fi.IsDir() {
		writeErr(w, http.StatusBadRequest, errors.New("that is a directory. ask for one file"))
		return "", false
	}
	if Executable(filepath.Ext(full)) {
		writeErr(w, http.StatusBadRequest, errors.New("atrium never opens a "+strings.ToLower(filepath.Ext(full))+
			" file with the default app, whatever the settings say. this machine would run it"))
		return "", false
	}
	if !Launchable(filepath.Ext(full)) {
		writeErr(w, http.StatusBadRequest, errors.New(
			"atrium opens only pictures, pdf and text documents with the default app. "+
				"a program or a script would run"))
		return "", false
	}
	return full, true
}

func (s *Server) launchFile(w http.ResponseWriter, r *http.Request) {
	task, err := s.st.Get(r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	if strings.TrimSpace(task.Worktree) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("this card has no directory"))
		return
	}
	var body openFileRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("could not read that request"))
		return
	}
	if strings.TrimSpace(body.Path) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("no path"))
		return
	}
	full, ok := s.launchTarget(w, task, body.Path)
	if !ok {
		return
	}
	if err := s.launcher()(full, filepath.FromSlash(task.Worktree)); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("could not open it: "+err.Error()))
		return
	}
	if err := s.st.AppendEvent(task.ID, store.EventNotified, map[string]any{
		"what": "opened a file with the default app", "path": body.Path,
	}); err != nil {
		log.Printf("[atrium api] could not record the open of %s: %v", body.Path, err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "opened": body.Path})
}
