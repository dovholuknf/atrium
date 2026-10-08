package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/dovholuknf/atrium/internal/store"
)

// GET /v1/scm/has?repo=host/org/repo says whether this room already holds a checkout of the repo, so the hub can open
// a pasted link on the room that has it and not clone the repo on another. It reads the disk and makes nothing.
// A room built before this answers 404, which the hub reads as "does not know".
func (s *Server) scmHas(w http.ResponseWriter, r *http.Request) {
	slug := strings.Trim(strings.TrimSpace(r.URL.Query().Get("repo")), "/")
	if !store.RepoSlug(slug) {
		writeErr(w, http.StatusBadRequest, errors.New("repo is host/org/repo, like github.com/openziti/ziti"))
		return
	}
	parts := strings.Split(slug, "/")
	host, org, repo := strings.ToLower(parts[0]), parts[1], parts[2]
	writeJSON(w, http.StatusOK, map[string]bool{"has": s.haveCheckout(s.providerByHost(host), host, org, repo)})
}
