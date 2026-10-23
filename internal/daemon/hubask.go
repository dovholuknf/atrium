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
	code, data, msg := d.hubCall(r.Context(), strings.TrimSpace(in.From), in.Method, in.Path, in.Body)
	if code != http.StatusOK {
		writeJSONCode(w, code, errBody(msg))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if len(data) == 0 {
		_, _ = w.Write([]byte("{}"))
		return
	}
	_, _ = w.Write(data)
}

// hubCall is one ask of the hub over the link, answered as a status, the hub's data and a sentence for a refusal. Both
// `POST /v1/hub/ask` and the room's own subscription handler ride it.
func (d *Daemon) hubCall(ctx context.Context, from, method, path string, body json.RawMessage) (int, json.RawMessage, string) {
	ha, _ := d.relay().(HubAsker)
	if ha == nil {
		return http.StatusServiceUnavailable, nil, "this atrium is not a room linked to a hub, so it has no hub backlog to ask"
	}
	cctx, cancel := context.WithTimeout(ctx, relayWait)
	defer cancel()
	res, err := ha.Hub(cctx, from, method, path, body)
	switch {
	case errors.Is(err, ErrRelayOld):
		return http.StatusBadGateway, nil, err.Error()
	case errors.Is(err, ErrRelayUnconfirmed):
		return http.StatusGatewayTimeout, nil, err.Error() + ". it may have gone through: look before asking again"
	case err != nil:
		return http.StatusBadGateway, nil, err.Error()
	case !res.OK && strings.Contains(res.Error, "does not know the relay op"):
		return http.StatusBadGateway, nil, "the hub is older than the backlog over the link, so it cannot carry this. update the hub"
	case !res.OK:
		code := res.Code
		if code < 400 {
			code = http.StatusBadGateway
		}
		return code, nil, res.Error
	}
	return http.StatusOK, res.Data, ""
}

// HubCall asks the hub one of the paths it serves a room, as this room and no card. See hubCall.
func (d *Daemon) HubCall(ctx context.Context, method, path string, body json.RawMessage) (int, json.RawMessage, string) {
	return d.hubCall(ctx, "", method, path, body)
}
