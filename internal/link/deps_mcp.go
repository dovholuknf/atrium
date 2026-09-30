package link

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// atrium_deps, the control tool over /_hub/deps. Full class only: `workerTools` does not
// name it, so a worker never sees it. It has NO clear action, on purpose. See deps.go.

const depsToolDesc = "Work items that wait on other work, kept on the hub across rooms and context resets.\n\n" +
	"Put a prerequisite here THE MOMENT YOU KNOW IT, never only in a handoff. An item is a backlog id " +
	"(`r-037`, `u-033`). `waits_on` takes item ids and conditions: `live:<item>` (landed AND in the " +
	"hub's own build), `room:<name>` (that room attached), `sha:<commit>` (on the integration branch), " +
	"or free text, which only a human can clear.\n\n" +
	"An item target is met when the integration branch holds `changelog/<dept>/<date>-<id>.md`. A gate " +
	"met stays met.\n\n" +
	"THERE IS NO CLEAR. A gate clears when the board sees the work land, or when a human clears it on " +
	"the board. If a gate is wrong, ask the human.\n\n" +
	"`atrium_launch` REFUSES a worker whose title starts with an item that has an open gate.\n\n" +
	"action is one of:\n" +
	"- `add {item, waits_on, why}`: item waits on each entry. A loop is refused, naming it. You are " +
	"told once, by a say, when the item's last gate clears.\n" +
	"- `list {item?, open?}`: every gate and what it waits on. open defaults to true: this is \"what is " +
	"stuck\" in one call.\n" +
	"- `check {item}`: `ready`, or the open gates with their reasons.\n" +
	"- `ready {dept}`: that department's backlog items on the integration branch that have not landed " +
	"and have no open gate. Pick your next item from this.\n" +
	"- `rename {from, to}`: a slug item got its number. Open gates follow. Clears nothing.\n\n" +
	"`repo` is needed only when the hub serves more than one repository."

type depsInput struct {
	Action  string   `json:"action" jsonschema:"add, list, check, ready or rename"`
	Item    string   `json:"item,omitempty" jsonschema:"the waiting item id, for add, list and check"`
	WaitsOn []string `json:"waits_on,omitempty" jsonschema:"for add: item ids, or live:<item>, room:<name>, sha:<commit>, or free text"`
	Why     string   `json:"why,omitempty" jsonschema:"for add: why it waits, read back later"`
	Open    *bool    `json:"open,omitempty" jsonschema:"for list: only open gates. default true"`
	Dept    string   `json:"dept,omitempty" jsonschema:"for ready: the department, e.g. runtime"`
	From    string   `json:"from,omitempty" jsonschema:"for rename: the old id"`
	To      string   `json:"to,omitempty" jsonschema:"for rename: the new id"`
	Repo    string   `json:"repo,omitempty" jsonschema:"one repository from the hub's git_repos. only needed when it has more than one"`
}

type depsOutput struct {
	Gates []depView   `json:"gates,omitempty"`
	Ready *bool       `json:"ready,omitempty"`
	Items []readyItem `json:"items,omitempty"`
	Moved *int        `json:"moved,omitempty"`
	Note  string      `json:"note,omitempty"`
}

func (c *controlMCP) registerDeps(s *mcp.Server, class ctlClass) {
	addTool(s, class, &mcp.Tool{Name: "atrium_deps", Description: depsToolDesc},
		audited(c, "ctl-deps", describeDeps, c.depsHandler))
}

// describeDeps records the writes. A read is not a line.
func describeDeps(req *mcp.CallToolRequest, in depsInput, _ depsOutput) (string, string, bool) {
	switch strings.TrimSpace(in.Action) {
	case "add":
		return roomOf(req), fmt.Sprintf("deps add %s waits on %s", in.Item, strings.Join(in.WaitsOn, ", ")), true
	case "rename":
		return roomOf(req), fmt.Sprintf("deps rename %s to %s", in.From, in.To), true
	}
	return "", "", false
}

// callerName is who added a gate, as the ticker will tell it: `handle@room`.
func callerName(req *mcp.CallToolRequest) string {
	agent, room := agentOf(req), roomOf(req)
	switch {
	case agent == "":
		return ""
	case room == "":
		return agent
	}
	return agent + "@" + room
}

func (c *controlMCP) depsHandler(ctx context.Context, req *mcp.CallToolRequest, in depsInput) (
	*mcp.CallToolResult, depsOutput, error) {

	var out depsOutput
	item := strings.TrimSpace(in.Item)
	q := url.Values{}
	if r := strings.TrimSpace(in.Repo); r != "" {
		q.Set("repo", r)
	}
	switch strings.TrimSpace(in.Action) {
	case "add":
		if item == "" || len(in.WaitsOn) == 0 {
			return nil, out, &refusedError{"add needs item and waits_on"}
		}
		err := c.ask(ctx, http.MethodPost, "/_hub/deps", "", map[string]any{
			"repo": in.Repo, "item": item, "waits_on": in.WaitsOn, "why": in.Why,
			"added_by": callerName(req)}, &out)
		return nil, out, err
	case "list", "check":
		if in.Action == "check" && item == "" {
			return nil, out, &refusedError{"check needs item"}
		}
		if item != "" {
			q.Set("item", item)
		}
		open := in.Open == nil || *in.Open
		if in.Action == "check" {
			open = true
		}
		if open {
			q.Set("open", "1")
		}
		if err := c.ask(ctx, http.MethodGet, "/_hub/deps?"+q.Encode(), "", nil, &out); err != nil {
			return nil, out, err
		}
		if in.Action == "check" {
			ready := len(out.Gates) == 0
			out.Ready = &ready
			if ready {
				out.Note = item + " has no open gate"
			}
		}
		return nil, out, nil
	case "ready":
		if strings.TrimSpace(in.Dept) == "" {
			return nil, out, &refusedError{"ready needs dept"}
		}
		q.Set("dept", strings.TrimSpace(in.Dept))
		err := c.ask(ctx, http.MethodGet, "/_hub/deps/ready?"+q.Encode(), "", nil, &out)
		if err == nil && len(out.Items) == 0 {
			out.Note = "nothing in docs/backlog/" + strings.TrimSpace(in.Dept) + " is ready"
		}
		return nil, out, err
	case "rename":
		if strings.TrimSpace(in.From) == "" || strings.TrimSpace(in.To) == "" {
			return nil, out, &refusedError{"rename needs from and to"}
		}
		by := callerName(req)
		if by == "" {
			by = "control"
		}
		err := c.ask(ctx, http.MethodPost, "/_hub/deps/rename", "", map[string]any{
			"repo": in.Repo, "from": in.From, "to": in.To, "by": by}, &out)
		return nil, out, err
	case "clear":
		return nil, out, &refusedError{"there is no clear. a gate clears when the board sees the work land, " +
			"or when a human clears it on the board. if a gate is wrong, ask the human"}
	}
	return nil, out, &refusedError{fmt.Sprintf("action %q is not one of add, list, check, ready, rename", in.Action)}
}
