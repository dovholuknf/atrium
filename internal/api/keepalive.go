package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/safepath"
	"github.com/dovholuknf/atrium/internal/store"
)

// The cache keep-alive's HTTP half: the per-card switch, and the room's default
// and suspension on the settings screen. The daemon owns the loop. See
// docs/cache-keepalive-design.md.

// setKeepalive flips one card's switch. `{"on": true}` turns it on, which also
// clears any stopped state and restarts the card's budget.
func (s *Server) setKeepalive(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		On *bool `json:"on"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&body); err != nil || body.On == nil {
		writeErr(w, http.StatusBadRequest, errKeepaliveBody)
		return
	}
	view, err := s.SetKeepalive(id, *body.On)
	if err != nil {
		s.fail(w, err)
		return
	}
	if t, err := s.st.Get(id); err == nil {
		s.Broadcast("task", toView(t))
	}
	writeJSON(w, http.StatusOK, map[string]any{"task_id": id, "keepalive": view})
}

var errKeepaliveBody = errors.New(`the keep-alive switch takes {"on": true} or {"on": false}`)

// keepaliveSettingsView adds the room's keep-alive settings to a settings
// payload: the default for new cards, the suspension if there is one, and the
// last week's refresh spend.
func keepaliveSettingsView(st *store.Store, out map[string]any) {
	out["cache_keepalive_default"] = st.KeepaliveDefaultOn()
	out["cache_keepalive_suspended"] = st.KeepaliveSuspended()
	// The week's dollar figure is not sent (item 37b), only the count.
	if _, n, err := st.KeepaliveSpendSince(time.Now().UTC().Add(-7 * 24 * time.Hour)); err == nil {
		out["cache_keepalive_week_refreshes"] = n
	}
}

// TranscriptPath is where a Claude session's transcript lives, or "" when it is
// not there. The keep-alive reads a card's last reply from it.
func TranscriptPath(cwd, id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	dir, err := projectDirFor(cwd)
	if err != nil {
		return ""
	}
	full, err := safepath.Contained(dir, filepath.Join(dir, id+".jsonl"))
	if err != nil {
		return ""
	}
	if _, err := os.Stat(full); err != nil {
		return ""
	}
	return full
}
