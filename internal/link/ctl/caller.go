package ctl

import (
	"context"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// CallerCard is the card the X-Atrium-Agent name belongs to on the caller's room.
func CallerCard(ctx context.Context, c Caller, room, agent string) (*Card, error) {
	var body struct {
		Tasks []Card `json:"tasks"`
	}
	if err := c.Ask(ctx, http.MethodGet, "/v1/tasks", room, nil, &body); err != nil {
		return nil, fmt.Errorf("could not find your card on %s: %w", RoomName(room), err)
	}
	card := PickCard(body.Tasks, agent)
	if card == nil {
		return nil, &RefusedError{Msg: fmt.Sprintf("no card on %s answers to %q, so nothing can be published as it",
			RoomName(room), agent)}
	}
	return card, nil
}

// RoomName names a room for a sentence.
func RoomName(room string) string {
	if room == "" {
		return "this hub's room"
	}
	return "room " + room
}

// CallerName is who added a gate, as the ticker will tell it: `handle@room`.
func CallerName(req *mcp.CallToolRequest) string {
	agent, room := AgentOf(req), RoomOf(req)
	switch {
	case agent == "":
		return ""
	case room == "":
		return agent
	}
	return agent + "@" + room
}
