package link

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ── alias ───────────────────────────────────────────────────────────────────────
//
// Reading and setting a card's alias from an agent, the way the board's card
// menu and terminal bar do for the operator (backlog-2 item 47). The room does
// the work: this is a GET, or a PATCH of `alias`, on the card, so the shape
// check and the uniqueness rule are the same ones the board meets, and a clash
// comes back naming the card that holds it. See store/alias.go.

const aliasToolDesc = "Read or set a card's alias: the short name it is mentioned by, like `@saorch` or " +
	"`@sa89`, which `atrium_say`, `atrium_peers` and `atrium tell` take wherever they take a handle.\n\n" +
	"Leave `card` empty for your own card. Leave `alias` empty and `clear` off to only read it. " +
	"Set `alias` to give the card that name (a leading `@` is fine): lowercase letters, digits, " +
	"`.`, `_` or `-`, at most 32. `clear` removes it.\n\n" +
	"An alias is unique among live cards. One already held is refused, and the refusal names the " +
	"card holding it. A card whose default alias was refused that way says so in `note`."

type aliasInput struct {
	Card  string `json:"card,omitempty" jsonschema:"the handle, alias or card id. empty means your own card"`
	Alias string `json:"alias,omitempty" jsonschema:"the alias to give it. empty only reads it"`
	Clear bool   `json:"clear,omitempty" jsonschema:"remove the card's alias"`
}

type aliasOutput struct {
	Card   string `json:"card"`
	Handle string `json:"handle"`
	Title  string `json:"title"`
	// Alias is the card's alias after the call, without the `@`. Empty for none.
	Alias string `json:"alias"`
	// Was is what it was before a set or a clear. Absent on a read.
	Was string `json:"was,omitempty"`
	// Note is why the card has no alias, when its default was held by another.
	Note string `json:"note,omitempty"`
}

// aliasCard is the part of a card this tool reads: `ctlCard` plus the note.
type aliasCard struct {
	ctlCard
	AliasNote string `json:"alias_note"`
}

func (c *controlMCP) aliasHandler(ctx context.Context, req *mcp.CallToolRequest, in aliasInput) (
	*mcp.CallToolResult, aliasOutput, error) {

	out := aliasOutput{}
	room := roomOf(req)
	who := strings.TrimSpace(in.Card)
	if who == "" {
		if who = agentOf(req); who == "" {
			return nil, out, fmt.Errorf("say which card. this session is not on the board, " +
				"so it has no card of its own to default to")
		}
	}
	set := strings.TrimSpace(in.Alias)
	if set != "" && in.Clear {
		return nil, out, fmt.Errorf("give `alias` or `clear`, not both")
	}
	id, _, err := c.resolvePeer(ctx, room, who)
	if err != nil {
		return nil, out, err
	}
	path := "/v1/tasks/" + url.PathEscape(id)
	var t aliasCard
	if err := c.ask(ctx, http.MethodGet, path, room, nil, &t); err != nil {
		return nil, out, err
	}
	if set != "" || in.Clear {
		out.Was = t.Alias
		if out.Was == "" {
			out.Was = "(none)"
		}
		var patched struct {
			Task aliasCard `json:"task"`
		}
		if err := c.ask(ctx, http.MethodPatch, path, room, map[string]any{"alias": set}, &patched); err != nil {
			return nil, out, err
		}
		t = patched.Task
	}
	out.Card, out.Handle, out.Title = t.ID, t.Wire, t.Title
	out.Alias, out.Note = t.Alias, t.AliasNote
	return nil, out, nil
}
