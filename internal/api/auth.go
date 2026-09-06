package api

import (
	"encoding/json"
	"io"
	"net/http"
)

// Configuring the login that sits in front of the published board.
//
// The daemon owns the behaviour and this is two routes. The one decision at
// this layer is what comes back on a read: the client SECRET is never sent,
// only whether there is one.
//
// That matters because this board is itself something people open. Sending the
// secret back so a form can round-trip it means the secret is in the page, in
// the browser's memory, and in any screenshot of the gear. A form that shows
// whether one is set and replaces it when you type a new one is the same
// usability with none of that.

// authLimit bounds an auth configuration, which is four strings and a list.
const authLimit = 64 << 10

func (s *Server) getAuth(w http.ResponseWriter, r *http.Request) {
	if s.AuthConfig == nil {
		writeErr(w, http.StatusNotImplemented, overlayErr("this daemon has no login support"))
		return
	}
	writeJSON(w, http.StatusOK, s.AuthConfig())
}

func (s *Server) putAuth(w http.ResponseWriter, r *http.Request) {
	if s.SaveAuth == nil {
		writeErr(w, http.StatusNotImplemented, overlayErr("this daemon has no login support"))
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, authLimit))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	// Decoded here only to confirm it is JSON at all. The daemon owns the
	// shape and the refusals, which is where the rules about an empty allow
	// list and a missing issuer live.
	var probe map[string]any
	if err := json.Unmarshal(body, &probe); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.SaveAuth(body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, s.AuthConfig())
}
