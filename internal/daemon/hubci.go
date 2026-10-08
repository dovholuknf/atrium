package daemon

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/dovholuknf/atrium/internal/forge"
)

var errBadCIAsk = errors.New("that is not a CI question")

// handleHubCI is POST /v1/hub/ci: a CI question (atrium_ci) asked of the hub over the link. The room never runs a
// forge CLI, so a room with no hub answers that, and the hub's refusal is passed on as its own sentence.
func (d *Daemon) handleHubCI(w http.ResponseWriter, r *http.Request) {
	var ask forge.HubCIAsk
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&ask); err != nil {
		writeJSONErr(w, http.StatusBadRequest, errBadCIAsk)
		return
	}
	hf := d.HubForge()
	if hf == nil {
		writeJSONErr(w, http.StatusBadGateway, errNoHub)
		return
	}
	ans, err := hf.CI(r.Context(), ask)
	if err != nil {
		writeJSONErr(w, http.StatusBadGateway, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ans)
}
