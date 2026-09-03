package api

import (
	"io"
	"net/http"
)

// The board's side of reaching atrium from elsewhere.
//
// Every one of these is a thin pass-through: the daemon owns the child process
// a share runs as, and this file only decides what a missing handler means.
// Missing means the feature is not wired, which is a 501 rather than a crash,
// because the API is meant to be servable without a daemon behind it.

func (s *Server) getOverlays(w http.ResponseWriter, r *http.Request) {
	if s.Overlays == nil {
		writeJSON(w, http.StatusOK, map[string]any{"overlays": []any{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"overlays": s.Overlays()})
}

func (s *Server) putOverlay(w http.ResponseWriter, r *http.Request) {
	if s.SaveOverlay == nil {
		writeErr(w, http.StatusNotImplemented, errNoOverlays)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<16))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.SaveOverlay(r.PathValue("kind"), body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.afterOverlayChange(w)
}

func (s *Server) startOverlay(w http.ResponseWriter, r *http.Request) {
	if s.StartOverlay == nil {
		writeErr(w, http.StatusNotImplemented, errNoOverlays)
		return
	}
	// A refusal here is a misconfiguration, not a server fault: no identity,
	// no service, a mode that is not a mode.
	if err := s.StartOverlay(r.PathValue("kind")); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.afterOverlayChange(w)
}

func (s *Server) stopOverlay(w http.ResponseWriter, r *http.Request) {
	if s.StopOverlay == nil {
		writeErr(w, http.StatusNotImplemented, errNoOverlays)
		return
	}
	if err := s.StopOverlay(r.PathValue("kind")); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.afterOverlayChange(w)
}

// afterOverlayChange answers with the new state and tells every open board.
// A share is one thing for the machine, so two tabs must not disagree about
// whether it is up.
func (s *Server) afterOverlayChange(w http.ResponseWriter) {
	var state any = []any{}
	if s.Overlays != nil {
		state = s.Overlays()
	}
	s.Broadcast("overlays", state)
	writeJSON(w, http.StatusOK, map[string]any{"overlays": state})
}

type overlayErr string

func (e overlayErr) Error() string { return string(e) }

const errNoOverlays overlayErr = "this daemon was built without overlay support"
