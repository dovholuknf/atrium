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
	"never build a link by hand. For more than a few files give `paths`, a list, in ONE call instead of one call " +
	"each: it answers `links`, one `{path, url}` or `{path, error}` per path in the order given. A link for the " +
	"human is never 127.0.0.1, localhost, a port or file://, and " +
	"atrium_done, atrium_blocked and atrium_say refuse them. Before you report it, render the url headless and read " +
	"the PNG: styled, no 4xx, no page error."

type linkInput struct {
	Path  string   `json:"path,omitempty" jsonschema:"a file in your own card, relative, no .. segments"`
	Paths []string `json:"paths,omitempty" jsonschema:"many files in your own card, one link each in this order, in one call"`
}

type linkOutput struct {
	URL   string     `json:"url,omitempty"`
	Links []linkLine `json:"links,omitempty"`
}

type linkLine struct {
	Path  string `json:"path"`
	URL   string `json:"url,omitempty"`
	Error string `json:"error,omitempty"`
}

func (c *controlMCP) linkHandler(ctx context.Context, req *mcp.CallToolRequest, in linkInput) (
	*mcp.CallToolResult, linkOutput, error) {

	if len(in.Paths) == 0 {
		if _, err := cardPath(in.Path); err != nil {
			return nil, linkOutput{}, err
		}
	}
	agent, room := ctl.AgentOf(req), ctl.RoomOf(req)
	if agent == "" || room == "" {
		return nil, linkOutput{}, &ctl.RefusedError{Msg: "this call carries no card or room, so the hub cannot say whose file this is"}
	}
	card, err := ctl.CallerCard(ctx, c, room, agent)
	if err != nil {
		return nil, linkOutput{}, err
	}
	base := fmt.Sprintf("%s/room/%s/%s/files/", seenhost.Base(c.board), url.PathEscape(room), url.PathEscape(card.ID))
	if len(in.Paths) == 0 {
		p, _ := cardPath(in.Path)
		return nil, linkOutput{URL: base + escapeSegs(p)}, nil
	}
	var out linkOutput
	for _, raw := range in.Paths {
		p, err := cardPath(raw)
		if err != nil {
			out.Links = append(out.Links, linkLine{Path: raw, Error: err.Error()})
			continue
		}
		out.Links = append(out.Links, linkLine{Path: raw, URL: base + escapeSegs(p)})
	}
	return nil, out, nil
}

func cardPath(raw string) (string, error) {
	p := path.Clean(strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(raw), `\`, "/"), "./"))
	if p == "." || strings.HasPrefix(p, "/") || p == ".." || strings.HasPrefix(p, "../") {
		return "", &ctl.RefusedError{Msg: "give path as a file inside your own card, relative, no .. segments"}
	}
	return p, nil
}

func escapeSegs(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}
