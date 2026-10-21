package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// DocShareRequest is what the reader sends to share one document: its path in the card, the images it just drew, and
// how long the link should live.
type DocShareRequest struct {
	Path    string   `json:"path"`
	Images  []string `json:"images"`
	TTLDays int      `json:"ttl_days"`
}

// DocShareError is a refusal with the status it should be answered with.
type DocShareError struct {
	Code int
	Msg  string
}

func (e *DocShareError) Error() string { return e.Msg }

func (s *Server) docShareFail(w http.ResponseWriter, err error) {
	var de *DocShareError
	if errors.As(err, &de) {
		writeErr(w, de.Code, err)
		return
	}
	s.fail(w, err)
}

func (s *Server) makeDocShare(w http.ResponseWriter, r *http.Request) {
	if s.MakeDocShare == nil {
		writeErr(w, http.StatusNotImplemented, errNoOverlays)
		return
	}
	var body DocShareRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	out, err := s.MakeDocShare(r.PathValue("id"), body)
	if err != nil {
		s.docShareFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getDocShare(w http.ResponseWriter, r *http.Request) {
	if s.DocShareOf == nil {
		writeErr(w, http.StatusNotFound, errors.New("that document is not shared"))
		return
	}
	out := s.DocShareOf(r.PathValue("id"), strings.TrimSpace(r.URL.Query().Get("path")))
	if out == nil {
		writeErr(w, http.StatusNotFound, errors.New("that document is not shared"))
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) stopDocShareFor(w http.ResponseWriter, r *http.Request) {
	if s.StopDocShareFor == nil {
		writeErr(w, http.StatusNotImplemented, errNoOverlays)
		return
	}
	if err := s.StopDocShareFor(r.PathValue("id"), r.URL.Query().Get("path")); err != nil {
		s.docShareFail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) stopDocShare(w http.ResponseWriter, r *http.Request) {
	if s.StopDocShare == nil {
		writeErr(w, http.StatusNotImplemented, errNoOverlays)
		return
	}
	if err := s.StopDocShare(r.PathValue("id")); err != nil {
		s.docShareFail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
