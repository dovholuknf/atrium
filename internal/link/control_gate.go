package link

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dovholuknf/atrium/internal/link/ctl"
)

const gateToolDesc = "Set the gate on your own card: the one thing you are waiting on the human for, which Your queue " +
	"on the board shows them as one colored verb. Call it whenever you stop on the human, as the LAST call of your " +
	"turn, then end the turn. It is how the board knows you are waiting, so do not rely on the human reading your " +
	"terminal.\n\n" +
	"`verb` is one of:\n" +
	"- `answer`: a question only the human can answer, such as an interview question.\n" +
	"- `approve`: something to look at and approve or send back, such as a mock, a design or a review page.\n" +
	"- `try`: something live to use and judge.\n" +
	"- `sign`: a finished change to walk through and sign before it lands.\n" +
	"- `unblock`: you cannot go on without something the human has, such as a token or a decision.\n\n" +
	"`stage` is where the work is, in a word or two, such as `interview`, `design` or `review`. `text` is the ask in " +
	"a sentence or two. When there is a page to look at, put its board link or its path in your folder (like " +
	"`mock.html`) in `text`, and the board opens it beside Approve and Send back.\n\n" +
	"If the human answers in the terminal instead (typed \"yes, approve it\"), the board cannot see it: call " +
	"atrium_gate with verb `clear` as soon as you go on, so the Approve bar comes down. `clear` needs no text.\n\n" +
	"The gate stays until it is answered: the human replies through the board, or you set a newer gate. A newer gate " +
	"replaces the old one. atrium_done clears every open ask, so when you finish with something to approve, call " +
	"atrium_done first and atrium_gate after it."

type gateInput struct {
	Verb  string `json:"verb" jsonschema:"answer, approve, try, sign or unblock. clear takes your open gate down when the human answered in the terminal"`
	Stage string `json:"stage,omitempty" jsonschema:"where the work is, in a word or two, such as interview, design or review"`
	Text  string `json:"text,omitempty" jsonschema:"the ask in a sentence or two, with the page's link or path when there is one. not needed with clear"`
}

type gateOutput struct {
	Recorded bool   `json:"recorded"`
	Card     string `json:"card"`
	Verb     string `json:"verb"`
	Stage    string `json:"stage,omitempty"`
}

func (c *controlMCP) gateHandler(ctx context.Context, req *mcp.CallToolRequest, in gateInput) (
	*mcp.CallToolResult, gateOutput, error) {

	out := gateOutput{}
	me := ctl.AgentOf(req)
	if me == "" {
		return nil, out, fmt.Errorf("atrium_gate is for a session atrium knows. this call did not say which one it is")
	}
	room := ctl.RoomOf(req)
	id, _, err := c.ResolvePeer(ctx, room, me)
	if err != nil {
		return nil, out, err
	}
	var res struct {
		Recorded bool `json:"recorded"`
	}
	verb := strings.ToLower(strings.TrimSpace(in.Verb))
	if err := c.Ask(ctx, http.MethodPost, "/v1/tasks/"+url.PathEscape(id)+"/gate", room,
		map[string]string{"verb": verb, "stage": strings.TrimSpace(in.Stage), "text": in.Text}, &res); err != nil {
		return nil, out, err
	}
	out.Recorded, out.Card, out.Verb, out.Stage = res.Recorded, id, verb, strings.TrimSpace(in.Stage)
	return nil, out, nil
}

// AddRoomLinkTool puts atrium_link on a room's stdio control server. Build answers the url for a file in the caller's card.
func AddRoomLinkTool(s *mcp.Server, build func(ctx context.Context, path string) (string, error)) {
	mcp.AddTool(s, &mcp.Tool{Name: "atrium_link", Description: linkToolDesc}, func(ctx context.Context,
		_ *mcp.CallToolRequest, in linkInput) (*mcp.CallToolResult, linkOutput, error) {

		u, err := build(ctx, in.Path)
		if err != nil {
			return nil, linkOutput{}, err
		}
		return nil, linkOutput{URL: u}, nil
	})
}

// AddRoomGateTool puts atrium_gate on a room's stdio control server. Set posts the gate for the caller's card and
// returns the card's id.
func AddRoomGateTool(s *mcp.Server, set func(ctx context.Context, body map[string]string) (string, bool, error)) {
	mcp.AddTool(s, &mcp.Tool{Name: "atrium_gate", Description: gateToolDesc}, func(ctx context.Context,
		_ *mcp.CallToolRequest, in gateInput) (*mcp.CallToolResult, gateOutput, error) {

		verb := strings.ToLower(strings.TrimSpace(in.Verb))
		stage := strings.TrimSpace(in.Stage)
		id, recorded, err := set(ctx, map[string]string{"verb": verb, "stage": stage, "text": in.Text})
		if err != nil {
			return nil, gateOutput{}, err
		}
		return nil, gateOutput{Recorded: recorded, Card: id, Verb: verb, Stage: stage}, nil
	})
}
