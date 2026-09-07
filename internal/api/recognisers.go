package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/dovholuknf/atrium/internal/store"
)

// The recognisers: rows that say what a URL means.
//
// Configuration, plus one verb. The configuration is the same shape as sources
// and harnesses because it is the same kind of thing, and the verb is the whole
// point: paste a pull request URL, get back everything a launch dialog needs.
//
// Atrium learns nothing about any source system here either. It matches a
// pattern somebody wrote and substitutes into templates somebody wrote. See
// docs/scm-design.md.

func (s *Server) listRecognisers(w http.ResponseWriter, r *http.Request) {
	rows, err := s.st.Recognisers()
	if err != nil {
		s.fail(w, err)
		return
	}
	if rows == nil {
		rows = []*store.Recogniser{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"recognisers": rows})
}

func (s *Server) saveRecogniser(w http.ResponseWriter, r *http.Request) {
	var rec store.Recogniser
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&rec); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	rec.ID = r.PathValue("id")
	saved, err := s.st.SaveRecogniser(rec)
	if err != nil {
		if halted, _ := s.st.Halted(); halted {
			s.fail(w, err)
			return
		}
		// A pattern that does not compile is the caller's mistake, and the
		// message names the position in the expression. Reporting it as a 500
		// would bury that behind "something went wrong".
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.Broadcast("recognisers", saved)
	writeJSON(w, http.StatusOK, saved)
}

// deleteRecogniser removes a row. The cards it filled in stay, because they are
// work, and deleting the thing that recognised the URL does not make it not
// work.
func (s *Server) deleteRecogniser(w http.ResponseWriter, r *http.Request) {
	if err := s.st.DeleteRecogniser(r.PathValue("id")); err != nil {
		s.fail(w, err)
		return
	}
	s.Broadcast("recognisers", map[string]string{"removed": r.PathValue("id")})
	w.WriteHeader(http.StatusNoContent)
}

// recognise turns one URL into the contents of a launch dialog.
//
// IT STARTS NOTHING. The answer is a form, and pressing start is a separate
// request the human makes after reading it. That separation is the reason a
// browser can be pointed at this endpoint without it being a way to run
// commands: the worst a URL can do here is fill some text boxes in.
//
// A URL no row matches is a 404 rather than an error, because "nothing here
// knows what that is" is an answer and not a failure, and the board says so
// with a link to where rows are written.
func (s *Server) recognise(w http.ResponseWriter, r *http.Request) {
	if s.Recognise == nil {
		writeErr(w, http.StatusNotImplemented, fmt.Errorf("no daemon wired"))
		return
	}
	var body struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	out, err := s.Recognise(body.URL)
	if errors.Is(err, store.ErrNoRecogniser) {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
