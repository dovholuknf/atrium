package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

// HubAsker is a Relay that can also ask the hub's backlog and reports endpoints. Optional, so a Relay that predates it
// (and every test double) still satisfies Relay. The link adapter in internal/cli implements it.
type HubAsker interface {
	Hub(ctx context.Context, from, method, path string, body json.RawMessage) (RelayResult, error)
}

// handleHubAsk is `POST /v1/hub/ask` with `{from, method, path, body}`: the room's atrium_backlog and atrium_reports,
// asked of the hub over the link this room already holds. The hub says which paths it serves and stamps who asked from
// the link. Nothing is held for later: a write that did not get an answer is the caller's to look at and repeat.
func (d *Daemon) handleHubAsk(w http.ResponseWriter, r *http.Request) {
	var in struct {
		From   string          `json:"from"`
		Method string          `json:"method"`
		Path   string          `json:"path"`
		Body   json.RawMessage `json:"body"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err)
		return
	}
	ha, _ := d.relay().(HubAsker)
	if ha == nil {
		writeJSONCode(w, http.StatusServiceUnavailable, errBody("this atrium is not a room linked to a hub, so it has "+
			"no hub backlog to ask"))
		return
	}
	cctx, cancel := context.WithTimeout(r.Context(), relayWait)
	defer cancel()
	res, err := ha.Hub(cctx, strings.TrimSpace(in.From), in.Method, in.Path, in.Body)
	switch {
	case errors.Is(err, ErrRelayOld):
		writeJSONCode(w, http.StatusBadGateway, errBody(err.Error()))
		return
	case errors.Is(err, ErrRelayUnconfirmed):
		writeJSONCode(w, http.StatusGatewayTimeout, errBody(err.Error()+". it may have gone through: look before "+
			"asking again"))
		return
	case err != nil:
		writeJSONCode(w, http.StatusBadGateway, errBody(err.Error()))
		return
	case !res.OK && strings.Contains(res.Error, "does not know the relay op"):
		writeJSONCode(w, http.StatusBadGateway, errBody("the hub is older than the backlog over the link, so it cannot "+
			"carry this. update the hub"))
		return
	case !res.OK:
		code := res.Code
		if code < 400 {
			code = http.StatusBadGateway
		}
		writeJSONCode(w, code, errBody(res.Error))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if len(res.Data) == 0 {
		_, _ = w.Write([]byte("{}"))
		return
	}
	_, _ = w.Write(res.Data)
}
