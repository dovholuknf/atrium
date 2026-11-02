package pulls

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/dovholuknf/atrium/internal/base/forge"
	"github.com/dovholuknf/atrium/internal/small"
)

var errBadCIAsk = errors.New("that is not a CI question")

// HubCI is POST /v1/hub/ci: a CI question (atrium_ci) asked of the hub over the link. The room never runs a
// forge CLI, so a room with no hub answers noHub, and the hub's refusal is passed on as its own sentence.
func HubCI(hub func() *forge.Remote, noHub error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var ask forge.HubCIAsk
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&ask); err != nil {
			small.WriteJSONErr(w, http.StatusBadRequest, errBadCIAsk)
			return
		}
		hf := hub()
		if hf == nil {
			small.WriteJSONErr(w, http.StatusBadGateway, noHub)
			return
		}
		ans, err := hf.CI(r.Context(), ask)
		if err != nil {
			small.WriteJSONErr(w, http.StatusBadGateway, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ans)
	}
}
