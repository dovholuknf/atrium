package api

import (
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/dovholuknf/atrium/internal/base/seenhost"
)

// getLink answers the board link for a file in the card's own folder, on the host the human reaches the board on. It is
// atrium_link for a session whose control server is the room's own, which a lean worker has.
func (s *Server) getLink(w http.ResponseWriter, r *http.Request) {
	t, err := s.st.Get(r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	p := path.Clean(strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(r.URL.Query().Get("path")), `\`, "/"), "./"))
	if p == "." || strings.HasPrefix(p, "/") || p == ".." || strings.HasPrefix(p, "../") {
		prError(w, http.StatusBadRequest, "bad_request", "give path as a file inside your own card, relative, no .. segments", nil)
		return
	}
	if strings.TrimSpace(s.Room) == "" {
		prError(w, http.StatusConflict, "no_hub", "this room has no hub, so there is no board link to give", nil)
		return
	}
	segs := strings.Split(p, "/")
	for i, g := range segs {
		segs[i] = url.PathEscape(g)
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": seenhost.Base("http://"+r.Host) + "/room/" + url.PathEscape(s.Room) +
		"/" + url.PathEscape(t.ID) + "/files/" + strings.Join(segs, "/")})
}
