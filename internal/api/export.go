package api

import (
	"encoding/json"
	"io"
	"net/http"
	"time"
)

// Atrium's configuration, out to a file and back.
//
// The daemon does the work, because the configuration includes overlay
// settings it owns. This is the two routes and the one decision that belongs
// at this layer: what the export is called when a browser saves it.

// exportLimit bounds what will be read as a configuration.
//
// A configuration is harnesses, sources and settings. Anything the size of a
// database is not one, and reading it to find that out is how a request
// becomes a memory problem.
const exportLimit = 4 << 20

// exportConfig writes this atrium's configuration.
//
// FILENAMED, because the ordinary use is a browser saving it into a checkout
// and a file called `export` in a repository is a file nobody can identify in
// six months. Dated for the same reason.
func (s *Server) exportConfig(w http.ResponseWriter, r *http.Request) {
	if s.BuildExport == nil {
		writeErr(w, http.StatusNotImplemented, overlayErr("this daemon cannot export its configuration"))
		return
	}
	out, err := s.BuildExport()
	if err != nil {
		// A REFUSAL IS THE POINT rather than a failure. The export stops when
		// something in the configuration looks like a credential, and the
		// message names where. It goes back as a 400 because the operator has
		// something to fix, not because the server broke.
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	name := "atrium-config-" + time.Now().UTC().Format("20060102") + ".json"
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	enc := json.NewEncoder(w)
	// Indented, because this is going into a repository and a diff of one long
	// line is not a diff anybody can read.
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

// importConfig reads one back, and by default only says what it would do.
//
// `?apply=1` writes. `?force=1` overwrites what is already set. Both off is
// the default and answers the question somebody restoring a machine has, which
// is what would change rather than what changed.
func (s *Server) importConfig(w http.ResponseWriter, r *http.Request) {
	if s.ApplyImport == nil {
		writeErr(w, http.StatusNotImplemented, overlayErr("this daemon cannot import a configuration"))
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, exportLimit))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	q := r.URL.Query()
	out, err := s.ApplyImport(body, q.Get("apply") == "1", q.Get("force") == "1")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
