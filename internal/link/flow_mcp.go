package link

import (
	"context"
	"encoding/json"
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
	"Roles are `orchestrator`, `tester` and `director`. One holder each, except tester. The merge queue (`atrium merge`) " +
	"moves claude/main on the hub, and the orchestrator closes an epoch. Stages, in order: idea, designed, implementing, implemented, " +
	"merged, tested, deployed, pushed-by-clint. A stage only moves forward. The item must be a backlog item.\n\n" +
	"action is one of:\n" +
	"- `roles {role?}`: who holds roles.\n" +
	"- `role_set {role, room, card}` / `role_clear {role, room, card}`: name or release a holder.\n" +
	"- `items {stage?}`: the items in the flow. `item {id}`: one, with what git and the recorded checks and deploys say of it. The stage is computed when you ask.\n" +
	"- `stage {id, stage, branch?, sha?, note?}`: report an item's stage.\n" +
	"- `checks {sha?}` / `check {sha, name, result}`: list, or record that a check passed, failed or timed out on a sha.\n" +
	"- `epoch`: the open epoch, the latest ones, and the plan for resetting claude/main onto main: the commits to replay. " +
	"`epoch_open {sha?}`: clint fetched claude/main (at sha, or where it is now). " +
	"`epoch_close {branch?, expect?}`: the MERGER ONLY. Replay the plan's commits on a branch with main under it, push " +
	"it, and give it here; the hub checks that nothing is lost and moves claude/main. No branch is for a plan with nothing to replay.\n" +
	"- `repo {repo?}`: the hub repository stages are read from, like `github.com/dovholuknf/atrium`. Without repo, read it.\n" +
	"- `deploys` / `deploy {room, sha}`: what each room runs, or record that a room now runs a sha."

type flowInput struct {
	Action string `json:"action" jsonschema:"roles, role_set, role_clear, items, item, stage, checks, check, epoch, epoch_open, epoch_close, repo, deploys or deploy"`
	Role   string `json:"role,omitempty" jsonschema:"orchestrator, tester or director"`
	Room   string `json:"room,omitempty" jsonschema:"the room, for role_set, role_clear and deploy"`
	Card   string `json:"card,omitempty" jsonschema:"the card id of the holder, for role_set and role_clear"`
	ID     string `json:"id,omitempty" jsonschema:"the backlog item id, for item and stage"`
	Stage  string `json:"stage,omitempty" jsonschema:"for stage, or to narrow items"`
	Branch string `json:"branch,omitempty" jsonschema:"for stage: the branch the work is on. for epoch_close: the branch with the replayed commits"`
	Expect string `json:"expect,omitempty" jsonschema:"for epoch_close: the old_tip of the plan you replayed, so a landing since is caught"`
	SHA    string `json:"sha,omitempty" jsonschema:"a commit id, for stage, check, checks and deploy"`
	Note   string `json:"note,omitempty" jsonschema:"for stage: one line, or where the doc is"`
	Repo   string `json:"repo,omitempty" jsonschema:"for repo: github.com/owner/repo, or empty to read"`
	Name   string `json:"name,omitempty" jsonschema:"for check: the check, like sanity"`
	Result string `json:"result,omitempty" jsonschema:"for check: pass, fail or timeout"`
}

type flowOutput struct {
	Roles    []hubstore.RoleHolder `json:"roles,omitempty"`
	Replaced []hubstore.RoleHolder `json:"replaced,omitempty"`
	Items    []hubstore.FlowItem   `json:"items,omitempty"`
	Item     *hubstore.FlowItem    `json:"item,omitempty"`
	View     *FlowView             `json:"view,omitempty"`
	Checks   []hubstore.FlowCheck  `json:"checks,omitempty"`
	Deploys  []hubstore.FlowDeploy `json:"deploys,omitempty"`
	Repo     *string               `json:"repo,omitempty"`
	Epoch    json.RawMessage       `json:"epoch,omitempty"`
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
		var f FlowView
		if err := c.ask(ctx, http.MethodGet, "/_hub/flow/items/"+url.PathEscape(id), "", nil, &f); err != nil {
			return nil, out, err
		}
		out.Item = &f.FlowItem
		out.View = &f
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
	case "epoch":
		var raw json.RawMessage
		if err := c.ask(ctx, http.MethodGet, "/_hub/flow/epoch", "", nil, &raw); err != nil {
			return nil, out, err
		}
		out.Epoch = raw
		return nil, out, nil
	case "epoch_open":
		var raw json.RawMessage
		if err := c.ask(ctx, http.MethodPost, "/_hub/flow/epoch", "", map[string]any{"do": "open", "sha": in.SHA, "by": by}, &raw); err != nil {
			return nil, out, err
		}
		out.Epoch = raw
		return nil, out, nil
	case "epoch_close":
		if err := c.requireOrchestrator(ctx, req); err != nil {
			return nil, out, err
		}
		var raw json.RawMessage
		if err := c.ask(ctx, http.MethodPost, "/_hub/flow/epoch", "", map[string]any{"do": "close", "branch": in.Branch,
			"expect": in.Expect, "by": by}, &raw); err != nil {
			return nil, out, err
		}
		out.Epoch = raw
		return nil, out, nil
	case "repo":
		var body struct {
			Repo string `json:"repo"`
		}
		var err error
		if strings.TrimSpace(in.Repo) == "" {
			err = c.ask(ctx, http.MethodGet, "/_hub/flow/repo", "", nil, &body)
		} else {
			err = c.ask(ctx, http.MethodPost, "/_hub/flow/repo", "", map[string]any{"repo": in.Repo, "by": by}, &body)
		}
		out.Repo = &body.Repo
		return nil, out, err
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
	return nil, out, &refusedError{fmt.Sprintf("action %q is not one of roles, role_set, role_clear, items, item, stage, checks, check, epoch, epoch_open, epoch_close, repo, deploys, deploy", in.Action)}
}

// requireOrchestrator refuses unless the caller is the card the hub has recorded as the orchestrator. THE CALLER IS A CLAIM, as for
// every tool here (see ctlaudit.go): the claim is the X-Atrium-Agent name, resolved to a card on the caller's room.
func (c *controlMCP) requireOrchestrator(ctx context.Context, req *mcp.CallToolRequest) error {
	agent, room := agentOf(req), roomOf(req)
	if agent == "" {
		return &refusedError{"only the orchestrator closes an epoch, and this call carries no session name"}
	}
	card, err := c.callerCard(ctx, room, agent)
	if err != nil {
		return err
	}
	var held struct {
		Roles []hubstore.RoleHolder `json:"roles"`
	}
	if err := c.ask(ctx, http.MethodGet, "/_hub/flow/roles?role="+hubstore.RoleOrchestrator, "", nil, &held); err != nil {
		return err
	}
	for _, h := range held.Roles {
		if h.Card == card.ID && strings.EqualFold(h.Room, room) {
			return nil
		}
	}
	return &refusedError{"only the orchestrator closes an epoch, and the hub has recorded another card (or none) as the orchestrator"}
}
