package api

import (
	"errors"
	"github.com/dovholuknf/atrium/internal/base/safepath"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// GET /v1/launch/cwd?path=<dir>: is that a directory on THIS machine.
//
// The hub asks it of each room on the caller's machine before it sends an
// unscoped launch anywhere, because the hub cannot stat a path on another
// machine and a launch sent to the wrong room fails with "is not a directory".
// See internal/link/launchroute.go.
//
// A BOOLEAN AND NOTHING ELSE. No listing, no contents, no stat fields, no
// symlink resolving. It is not `/v1/browse`, which is bounded to roots so that
// it is not a tour of the machine, and it does not need to be: `POST /v1/launch`
// already answers "is not a directory" for any path whatever, on this same
// listener, so the existence of a directory was already askable by anybody who
// can launch. Read only, creates nothing.
func (s *Server) launchCwd(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.URL.Query().Get("path"))
	// a share or device path is refused before any Stat: on Windows that opens SMB and hands the named host an NTLM hash
	if safepath.Network(raw) {
		writeErr(w, http.StatusBadRequest, errors.New("a network path is not a place to run"))
		return
	}
	// A relative path is relative to this daemon's own directory, which is not
	// what the caller meant by it. Not a directory, rather than a guess.
	path := filepath.FromSlash(raw)
	if raw == "" || !filepath.IsAbs(path) {
		writeJSON(w, http.StatusOK, map[string]bool{"exists": false, "dir": false})
		return
	}
	// codeql[go/path-injection] Network refused shares and device paths above, and a boolean is all POST /v1/launch already answers
	fi, err := os.Stat(safepath.NoNUL(path))
	writeJSON(w, http.StatusOK, map[string]bool{
		"exists": err == nil,
		"dir":    err == nil && fi.IsDir(),
	})
}
