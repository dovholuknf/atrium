package link

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const reviewToolDesc = "The review of the branch in your directory, held by atrium. Served to a card launched as a review only.\n\n" +
	"Atrium measures, draws and applies, so you never print a diff and never call Edit for a finding. The human reads " +
	"each finding in the review view of your card and answers in chat.\n\n" +
	"action start {base}: finds the merge-base with base and answers it with the changed files and their line counts.\n" +
	"action finding {cat, kind, site, title, edits, note, footer}: stores one finding, computes its diff and its line " +
	"count against the base, and shows it. Answers its number and the count. cat is SCOPE, DEAD, DRY, CHURN, HYGIENE, " +
	"TEST SCOPE, COMMENT or SIMPLIFY. kind is code, comment or test. site is path:line. edits are {path, old, new} " +
	"like an Edit call, and old must appear in the file exactly once. note is one or two lines. footer is the two " +
	"lines the human reads last.\n" +
	"action batch {items}: items are findings as above. They are applied at once and shown as applied for the veto.\n" +
	"action apply {id}: writes the stored edits of finding id into your directory and marks it applied.\n" +
	"action close {id, state, reason}: state is withdrawn, kept or vetoed. A vetoed finding that was applied is put back.\n\n" +
	"Edits reach files inside your directory only. A path outside it is refused."

type reviewEdit struct {
	Path string `json:"path" jsonschema:"the file, relative to your directory"`
	Old  string `json:"old" jsonschema:"the text to replace. it has to appear in the file exactly once"`
	New  string `json:"new" jsonschema:"what to put there"`
}

type reviewItem struct {
	Cat    string       `json:"cat" jsonschema:"SCOPE, DEAD, DRY, CHURN, HYGIENE, TEST SCOPE, COMMENT or SIMPLIFY"`
	Kind   string       `json:"kind,omitempty" jsonschema:"code, comment or test"`
	Origin string       `json:"origin,omitempty" jsonschema:"net-new or existing, against the base"`
	Site   string       `json:"site,omitempty" jsonschema:"path:line"`
	Title  string       `json:"title" jsonschema:"a short title for the rail"`
	Edits  []reviewEdit `json:"edits" jsonschema:"the change, as {path, old, new} like an Edit call"`
	Note   string       `json:"note,omitempty" jsonschema:"one or two lines on the trade-off"`
	Footer string       `json:"footer,omitempty" jsonschema:"the two footer lines, verbatim"`
}

type reviewInput struct {
	Action string       `json:"action" jsonschema:"start, finding, batch, apply or close"`
	Base   string       `json:"base,omitempty" jsonschema:"with start: the branch to measure against"`
	ID     int          `json:"id,omitempty" jsonschema:"with apply and close: the finding's number"`
	State  string       `json:"state,omitempty" jsonschema:"with close: withdrawn, kept or vetoed"`
	Reason string       `json:"reason,omitempty" jsonschema:"with close: why"`
	Items  []reviewItem `json:"items,omitempty" jsonschema:"with batch: the no-brainers, each as a finding"`
	reviewItemFlat
}

// reviewItemFlat carries a finding's fields at the top level of the call, as `finding` takes them.
type reviewItemFlat struct {
	Cat    string       `json:"cat,omitempty" jsonschema:"with finding: SCOPE, DEAD, DRY, CHURN, HYGIENE, TEST SCOPE, COMMENT or SIMPLIFY"`
	Kind   string       `json:"kind,omitempty" jsonschema:"with finding: code, comment or test"`
	Origin string       `json:"origin,omitempty" jsonschema:"with finding: net-new or existing, against the base"`
	Site   string       `json:"site,omitempty" jsonschema:"with finding: path:line"`
	Title  string       `json:"title,omitempty" jsonschema:"with finding: a short title for the rail"`
	Edits  []reviewEdit `json:"edits,omitempty" jsonschema:"with finding: the change, as {path, old, new} like an Edit call"`
	Note   string       `json:"note,omitempty" jsonschema:"with finding: one or two lines on the trade-off"`
	Footer string       `json:"footer,omitempty" jsonschema:"with finding: the two footer lines, verbatim"`
}

// reviewHandler forwards a review call to the caller's own card on its room, which holds the review and does the work.
func (c *controlMCP) reviewHandler(ctx context.Context, req *mcp.CallToolRequest, in reviewInput) (
	*mcp.CallToolResult, map[string]any, error) {

	out := map[string]any{}
	agent := agentOf(req)
	if agent == "" {
		return nil, out, &refusedError{"atrium_review is for a review card, and this call names no card"}
	}
	switch in.Action {
	case "start", "finding", "batch", "apply", "close":
	default:
		return nil, out, &refusedError{"action is start, finding, batch, apply or close"}
	}
	room := roomOf(req)
	scope, id, _, err := c.resolveCardStrict(ctx, room, agent)
	if err != nil {
		return nil, out, err
	}
	long := &controlMCP{board: c.board, client: &http.Client{Timeout: cullTimeout, Transport: c.client.Transport}}
	path := "/v1/tasks/" + url.PathEscape(id) + "/atrium-review"
	var raw map[string]any
	if err := long.ask(ctx, http.MethodPost, path, scope, in, &raw); err != nil {
		var be *boardError
		if errors.As(err, &be) && be.code == http.StatusForbidden {
			return nil, out, &refusedError{"this card was not launched as a review. start one with kind review"}
		}
		return nil, out, err
	}
	if raw != nil {
		out = raw
	}
	return nil, out, nil
}

func describeReview(req *mcp.CallToolRequest, in reviewInput, _ map[string]any) (string, string, bool) {
	if in.Action != "apply" && in.Action != "close" && in.Action != "batch" {
		return "", "", false
	}
	what := fmt.Sprintf("review %s", in.Action)
	if in.ID > 0 {
		what += fmt.Sprintf(" %d", in.ID)
	}
	if s := strings.TrimSpace(in.State); s != "" {
		what += " " + s
	}
	return roomOf(req), what, true
}
