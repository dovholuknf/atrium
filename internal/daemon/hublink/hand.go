package hublink

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

// HandAsker is a Relay that can carry a hook of a session started by hand to the room holding its card. Optional, so a
// Relay that predates it (and every test double) still satisfies Relay. The link adapter in internal/cli implements it.
type HandAsker interface {
	// Hand with no room asks every other room for the card holding the conversation, and the one that does takes the
	// hook. With a room and a card it carries the hook to that card.
	Hand(ctx context.Context, room, card, path string, body json.RawMessage) (RelayResult, error)
}

// HandCall carries one hook over the link, answered as a status, the owning room's answer and a sentence for a refusal.
func (d *Service) HandCall(ctx context.Context, room, card, path string, body json.RawMessage) (int, json.RawMessage, string) {
	ha, _ := d.Relay().(HandAsker)
	if ha == nil {
		return http.StatusServiceUnavailable, nil, "this atrium is not a room linked to a hub"
	}
	res, err := ha.Hand(ctx, room, card, path, body)
	switch {
	case errors.Is(err, ErrRelayOld):
		return http.StatusBadGateway, nil, err.Error()
	case err != nil:
		return http.StatusBadGateway, nil, err.Error()
	case !res.OK:
		code := res.Code
		if code < 400 {
			code = http.StatusBadGateway
		}
		return code, nil, res.Error
	}
	return http.StatusOK, res.Data, ""
}
