package link

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dovholuknf/atrium/internal/link/ctl"
)

// atrium_transport, the control tool over a room's /v1/link/transports. Full class only: `workerTools` does not name
// it. The room does the work and does the refusing: it will not turn off its last working path to the hub.

const transportToolDesc = "Turn one of a room's overlay transports, ziti or zrok, off or on, without restarting " +
	"the room or touching a runner.\n\n" +
	"Off closes the overlay's context and stops redialling it, and the choice survives a room restart. " +
	"The room must still reach the hub another way, and a room refuses to turn off its last working path. " +
	"`room` defaults to yours. Leave `on` out to read each transport's state."

const transportTimeout = 40 * time.Second

type transportInput struct {
	Room      string `json:"room,omitempty" jsonschema:"the room. default your own"`
	Transport string `json:"transport,omitempty" jsonschema:"ziti or zrok. omit to just read the states"`
	On        *bool  `json:"on,omitempty" jsonschema:"true turns it on, false turns it off"`
}

type transportOutput struct {
	Transports []TransportState `json:"transports"`
	Off        []string         `json:"off,omitempty"`
}

func (c *controlMCP) registerTransport(s *mcp.Server, class ctl.Class) {
	addTool(s, class, &mcp.Tool{Name: "atrium_transport", Description: transportToolDesc},
		audited(c, "ctl-transport", describeTransport, c.transportHandler))
}

func describeTransport(req *mcp.CallToolRequest, in transportInput, out transportOutput) (string, string, bool) {
	if in.On == nil {
		return "", "", false
	}
	verb := "off"
	if *in.On {
		verb = "on"
	}
	return strings.TrimSpace(in.Room), "transport " + strings.TrimSpace(in.Transport) + " " + verb, true
}

func (c *controlMCP) transportHandler(ctx context.Context, req *mcp.CallToolRequest, in transportInput) (
	*mcp.CallToolResult, transportOutput, error) {

	var out transportOutput
	room := strings.TrimSpace(in.Room)
	if room == "" {
		room = ctl.RoomOf(req)
	}
	long := &controlMCP{board: c.board, client: &http.Client{Timeout: transportTimeout, Transport: c.client.Transport}}
	var err error
	switch {
	case in.On == nil && strings.TrimSpace(in.Transport) == "":
		err = long.Ask(ctx, http.MethodGet, "/v1/link/transports", room, nil, &out)
	case in.On == nil || strings.TrimSpace(in.Transport) == "":
		return nil, out, &ctl.RefusedError{Msg: "give both transport and on, or neither to read the states"}
	default:
		err = long.Ask(ctx, http.MethodPost, "/v1/link/transports", room,
			TransportAsk{Transport: in.Transport, On: *in.On}, &out)
	}
	if err != nil {
		var be *ctl.BoardError
		if errors.As(err, &be) && be.NoSentence && be.Code == http.StatusNotFound {
			return nil, out, errors.New("that room is older than atrium_transport. update the room")
		}
		return nil, out, err
	}
	return nil, out, nil
}
