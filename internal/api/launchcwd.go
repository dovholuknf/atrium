package api

import (
	"errors"
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
	// A UNC PATH IS REFUSED BEFORE ANYTHING TOUCHES IT. On a Windows room
	// `os.Stat(\\host\share)` opens SMB to that host with this user's credentials,
	// which hands the user's NTLM hash to whatever host the caller named, and the
	// hub fans this call out to every candidate room. Any two leading slashes of
	// either kind, since Windows reads `//host` and `\\?\` and `\\.\` the same way.
	// The hub refuses them first (internal/link/launchroute.go), and this makes a
	// direct call safe too.
	if len(raw) >= 2 && (raw[0] == '/' || raw[0] == '\\') && (raw[1] == '/' || raw[1] == '\\') {
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
	fi, err := os.Stat(path)
	writeJSON(w, http.StatusOK, map[string]bool{
		"exists": err == nil,
		"dir":    err == nil && fi.IsDir(),
	})
}
