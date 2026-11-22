package link

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dovholuknf/atrium/internal/base/seenhost"
	"github.com/dovholuknf/atrium/internal/link/ctl"
)

const linkToolDesc = "The link the human opens for a file in YOUR OWN card, such as a mock at " +
	"`.uncommitted/preview/<name>.html`. Give `path`, relative to your card. Answers `{url}`: send exactly that, and " +
	"never build a link by hand. A link for the human is never 127.0.0.1, localhost, a port or file://, and " +
	"atrium_done, atrium_blocked and atrium_say refuse them. Before you report it, render the url headless and read " +
	"the PNG: styled, no 4xx, no page error."

type linkInput struct {
	Path string `json:"path" jsonschema:"a file in your own card, relative, no .. segments"`
}

type linkOutput struct {
	URL string `json:"url"`
}

func (c *controlMCP) linkHandler(ctx context.Context, req *mcp.CallToolRequest, in linkInput) (
	*mcp.CallToolResult, linkOutput, error) {

	p := path.Clean(strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(in.Path), `\`, "/"), "./"))
	if p == "." || strings.HasPrefix(p, "/") || p == ".." || strings.HasPrefix(p, "../") {
		return nil, linkOutput{}, &ctl.RefusedError{Msg: "give path as a file inside your own card, relative, no .. segments"}
	}
	agent, room := ctl.AgentOf(req), ctl.RoomOf(req)
	if agent == "" || room == "" {
		return nil, linkOutput{}, &ctl.RefusedError{Msg: "this call carries no card or room, so the hub cannot say whose file this is"}
	}
	card, err := ctl.CallerCard(ctx, c, room, agent)
	if err != nil {
		return nil, linkOutput{}, err
	}
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return nil, linkOutput{URL: fmt.Sprintf("%s/room/%s/%s/files/%s", seenhost.Base(c.board), url.PathEscape(room),
		url.PathEscape(card.ID), strings.Join(segs, "/"))}, nil
}
