package api

import "net/http"

// getArtifacts is what a card's session made: files, reports, builds, images, branches and links. See
// store.Artifact. Opening one goes through the files endpoints, so containment is theirs.
func (s *Server) getArtifacts(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.st.Get(id); err != nil {
		s.fail(w, err)
		return
	}
	list, err := s.st.Artifacts(id)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}
