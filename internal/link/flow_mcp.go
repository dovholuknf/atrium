package link

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

// atrium_flow, the control tool over /_hub/flow. Full class only: `workerTools` does not name it. The HTTP half, and
// who may write, is in flow.go.

const flowToolDesc = "The merge flow, kept on the hub: who holds each role, which stage each backlog item is at, " +
	"which checks passed on which sha, and which sha each room runs. After a restart this is where it is read from, " +
	"not from other sessions.\n\n" +
	"Roles are `orchestrator`, `merger`, `tester` and `director`. One holder each, except tester. Only the merger moves " +
	"claude/main on the hub, and only by fast-forward. Stages, in order: idea, designed, implementing, implemented, " +
	"merged, tested, deployed, pushed-by-clint. A stage only moves forward. The item must be a backlog item.\n\n" +
	"action is one of:\n" +
	"- `roles {role?}`: who holds roles.\n" +
	"- `role_set {role, room, card}` / `role_clear {role, room, card}`: name or release a holder.\n" +
	"- `items {stage?}`: the items in the flow. `item {id}`: one.\n" +
	"- `stage {id, stage, branch?, sha?, note?}`: report an item's stage.\n" +
	"- `checks {sha?}` / `check {sha, name, result}`: list, or record that a check passed, failed or timed out on a sha.\n" +
	"- `deploys` / `deploy {room, sha}`: what each room runs, or record that a room now runs a sha."

type flowInput struct {
	Action string `json:"action" jsonschema:"roles, role_set, role_clear, items, item, stage, checks, check, deploys or deploy"`
	Role   string `json:"role,omitempty" jsonschema:"orchestrator, merger, tester or director"`
	Room   string `json:"room,omitempty" jsonschema:"the room, for role_set, role_clear and deploy"`
	Card   string `json:"card,omitempty" jsonschema:"the card id of the holder, for role_set and role_clear"`
	ID     string `json:"id,omitempty" jsonschema:"the backlog item id, for item and stage"`
	Stage  string `json:"stage,omitempty" jsonschema:"for stage, or to narrow items"`
	Branch string `json:"branch,omitempty" jsonschema:"for stage: the branch the work is on"`
	SHA    string `json:"sha,omitempty" jsonschema:"a commit id, for stage, check, checks and deploy"`
	Note   string `json:"note,omitempty" jsonschema:"for stage: one line, or where the doc is"`
	Name   string `json:"name,omitempty" jsonschema:"for check: the check, like sanity"`
	Result string `json:"result,omitempty" jsonschema:"for check: pass, fail or timeout"`
}

type flowOutput struct {
	Roles    []hubstore.RoleHolder `json:"roles,omitempty"`
	Replaced []hubstore.RoleHolder `json:"replaced,omitempty"`
	Items    []hubstore.FlowItem   `json:"items,omitempty"`
	Item     *hubstore.FlowItem    `json:"item,omitempty"`
	Checks   []hubstore.FlowCheck  `json:"checks,omitempty"`
	Deploys  []hubstore.FlowDeploy `json:"deploys,omitempty"`
	Note     string                `json:"note,omitempty"`
}

func (c *controlMCP) registerFlow(s *mcp.Server, class ctlClass) {
	addTool(s, class, &mcp.Tool{Name: "atrium_flow", Description: flowToolDesc}, c.flowHandler)
}

func (c *controlMCP) flowHandler(ctx context.Context, req *mcp.CallToolRequest, in flowInput) (
	*mcp.CallToolResult, flowOutput, error) {

	var out flowOutput
	by := callerName(req)
	id := strings.TrimSpace(in.ID)
	q := func(k, v string) string {
		if v = strings.TrimSpace(v); v == "" {
			return ""
		}
		return "?" + k + "=" + url.QueryEscape(v)
	}
	switch strings.TrimSpace(in.Action) {
	case "roles":
		err := c.ask(ctx, http.MethodGet, "/_hub/flow/roles"+q("role", in.Role), "", nil, &out)
		if err == nil && len(out.Roles) == 0 {
			out.Note = "no role is held"
		}
		return nil, out, err
	case "role_set", "role_clear":
		if in.Role == "" || strings.TrimSpace(in.Room) == "" || strings.TrimSpace(in.Card) == "" {
			return nil, out, &refusedError{"role_set and role_clear need role, room and card"}
		}
		do := strings.TrimPrefix(in.Action, "role_")
		var body struct {
			Holder   *hubstore.RoleHolder  `json:"holder"`
			Replaced []hubstore.RoleHolder `json:"replaced"`
		}
		err := c.ask(ctx, http.MethodPost, "/_hub/flow/roles", "", map[string]any{
			"do": do, "role": in.Role, "room": strings.TrimSpace(in.Room), "card": strings.TrimSpace(in.Card), "by": by}, &body)
		if err != nil {
			return nil, out, err
		}
		if body.Holder != nil {
			out.Roles = []hubstore.RoleHolder{*body.Holder}
		}
		out.Replaced = body.Replaced
		return nil, out, nil
	case "items":
		err := c.ask(ctx, http.MethodGet, "/_hub/flow/items"+q("stage", in.Stage), "", nil, &out)
		if err == nil && len(out.Items) == 0 {
			out.Note = "no items are in the flow"
		}
		return nil, out, err
	case "item":
		if id == "" {
			return nil, out, &refusedError{"item needs id"}
		}
		var f hubstore.FlowItem
		if err := c.ask(ctx, http.MethodGet, "/_hub/flow/items/"+url.PathEscape(id), "", nil, &f); err != nil {
			return nil, out, err
		}
		out.Item = &f
		return nil, out, nil
	case "stage":
		if id == "" || in.Stage == "" {
			return nil, out, &refusedError{"stage needs id and stage"}
		}
		var f hubstore.FlowItem
		err := c.ask(ctx, http.MethodPost, "/_hub/flow/items/"+url.PathEscape(id), "", map[string]any{
			"stage": in.Stage, "branch": in.Branch, "sha": in.SHA, "note": in.Note, "by": by}, &f)
		if err != nil {
			return nil, out, err
		}
		out.Item = &f
		return nil, out, nil
	case "checks":
		err := c.ask(ctx, http.MethodGet, "/_hub/flow/checks"+q("sha", in.SHA), "", nil, &out)
		if err == nil && len(out.Checks) == 0 {
			out.Note = "no checks are recorded"
		}
		return nil, out, err
	case "check":
		if in.SHA == "" || in.Name == "" || in.Result == "" {
			return nil, out, &refusedError{"check needs sha, name and result"}
		}
		var ck hubstore.FlowCheck
		err := c.ask(ctx, http.MethodPost, "/_hub/flow/checks", "", map[string]any{
			"sha": in.SHA, "name": in.Name, "result": in.Result, "by": by}, &ck)
		if err != nil {
			return nil, out, err
		}
		out.Checks = []hubstore.FlowCheck{ck}
		return nil, out, nil
	case "deploys":
		err := c.ask(ctx, http.MethodGet, "/_hub/flow/deploys", "", nil, &out)
		if err == nil && len(out.Deploys) == 0 {
			out.Note = "no room has reported a deploy"
		}
		return nil, out, err
	case "deploy":
		if strings.TrimSpace(in.Room) == "" || in.SHA == "" {
			return nil, out, &refusedError{"deploy needs room and sha"}
		}
		var d hubstore.FlowDeploy
		err := c.ask(ctx, http.MethodPost, "/_hub/flow/deploys", "", map[string]any{
			"room": strings.TrimSpace(in.Room), "sha": in.SHA, "by": by}, &d)
		if err != nil {
			return nil, out, err
		}
		out.Deploys = []hubstore.FlowDeploy{d}
		return nil, out, nil
	}
	return nil, out, &refusedError{fmt.Sprintf("action %q is not one of roles, role_set, role_clear, items, item, stage, checks, check, deploys, deploy", in.Action)}
}
