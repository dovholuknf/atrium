package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// homes is GET /v1/homes, the Disk view. See internal/daemon/homedisk.go.
func (s *Server) homes(w http.ResponseWriter, _ *http.Request) {
	v, err := s.Homes()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// removeHome is POST /v1/homes/remove. It takes the id of a ledger row and nothing else: a path in the body is refused,
// so no caller can name a folder.
func (s *Server) removeHome(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<12))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil || body.ID == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf(`a removal is {"id": "<home id>"} and nothing else`))
		return
	}
	res, err := s.HomeRemove(body.ID)
	if err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// openHome is POST /v1/homes/open: show a home's folder on the room's own machine. The id of a ledger row, like removal.
func (s *Server) openHome(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<12))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil || body.ID == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf(`opening is {"id": "<home id>"} and nothing else`))
		return
	}
	res, err := s.HomeOpen(body.ID)
	if err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
