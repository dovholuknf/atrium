package link

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// atrium_done and atrium_blocked: how a worker ends its work, in a fixed format.
//
// SHORT AND NOTHING ELSE. `done` takes a commit, `blocked` takes a reason of up to 50 words, and both go to the
// launcher exactly as `atrium_report` goes, through the room's report endpoint with `ended` set. Atrium sends the card
// no nudge after either, because a told report ends what is owed (store.Task.OwesReport) and a blocked one moves its
// item to reported as a done one does. The room checks the commit against the card's own directory, then the hub, and
// records nothing when neither has it. See internal/daemon/finish.go. ONE CODE for the hub's control and the stdio one.

// EndReasonMax is the longest reason atrium_blocked takes, in words.
const EndReasonMax = 50

// endSHAShape is what a commit id looks like: seven to forty hex characters.
var endSHAShape = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)

const doneToolDesc = "End your work: it is finished. THIS IS HOW TO END. Nothing else is needed. Give exactly one of " +
	"`sha` or `artifact`: sha or artifact location. `sha` is the commit the work landed as (7 to 40 hex " +
	"characters, and it has to exist in your directory or on any hub branch, or the call is refused and nothing is " +
	"recorded). `artifact` is a path or URL to what you made, for work with no commit, and a path has to exist on " +
	"your own room. Details go in REPORT.md, not here. Stop after the call: say nothing more and do no more work."

const blockedToolDesc = "End your work: something stops you and you cannot go on. THIS IS HOW TO END. " +
	"Nothing else is needed: give `reason`, what stops you and what you need, in up to 50 words. A longer reason is " +
	"refused, not cut. Details go in REPORT.md, not here. Stop after the call: say " +
	"nothing more and do no more work."

type doneInput struct {
	SHA      string `json:"sha,omitempty" jsonschema:"sha or artifact location. the commit the work landed as: 7 to 40 hex characters"`
	Artifact string `json:"artifact,omitempty" jsonschema:"sha or artifact location. a path or URL to what you made, when there is no commit"`
}

type blockedInput struct {
	Reason string `json:"reason" jsonschema:"what stops you, in up to 50 words"`
}

// EndOutput is what either tool answers.
type EndOutput struct {
	Recorded     bool   `json:"recorded"`
	Status       string `json:"status"`
	LauncherTold bool   `json:"launcher_told"`
	Note         string `json:"note,omitempty"`
}

// CheckDoneSHA is why a commit id is refused, or "".
func CheckDoneSHA(sha string) string {
	if !endSHAShape.MatchString(strings.TrimSpace(sha)) {
		return fmt.Sprintf("sha %q is not a commit id: 7 to 40 hex characters", strings.TrimSpace(sha))
	}
	return ""
}

// CheckBlockedReason is why a reason is refused, or "": 1 to 50 words, split on whitespace. Newlines are fine.
func CheckBlockedReason(reason string) string {
	n := len(strings.Fields(reason))
	switch {
	case n == 0:
		return "say why you are blocked, in up to 50 words"
	case n > EndReasonMax:
		return fmt.Sprintf("the reason is %d words, over %d. shorten it, details go in REPORT.md", n, EndReasonMax)
	}
	return ""
}

// EndDoor is the two tools for a server, given how each reaches the room's report endpoint.
type EndDoor struct {
	// Report files the body as the caller's report and answers what the room recorded.
	Report func(ctx context.Context, req *mcp.CallToolRequest, body map[string]any) (EndOutput, error)
}

// Done is atrium_done.
func (d EndDoor) Done(ctx context.Context, req *mcp.CallToolRequest, in doneInput) (
	*mcp.CallToolResult, EndOutput, error) {

	sha, artifact := strings.TrimSpace(in.SHA), strings.TrimSpace(in.Artifact)
	switch {
	case sha != "" && artifact != "":
		return nil, EndOutput{}, fmt.Errorf("give sha or artifact, not both")
	case sha == "" && artifact == "":
		return nil, EndOutput{}, fmt.Errorf("give sha (the commit) or artifact (a path or URL to what you made)")
	case artifact != "":
		out, err := d.Report(ctx, req, map[string]any{
			"status": "done", "recap": "done " + artifact, "artifact": artifact, "ended": true,
		})
		return nil, out, err
	}
	if why := CheckDoneSHA(sha); why != "" {
		return nil, EndOutput{}, fmt.Errorf("%s", why)
	}
	out, err := d.Report(ctx, req, map[string]any{
		"status": "done", "recap": "done " + sha, "sha": sha, "ended": true,
	})
	return nil, out, err
}

// Blocked is atrium_blocked.
func (d EndDoor) Blocked(ctx context.Context, req *mcp.CallToolRequest, in blockedInput) (
	*mcp.CallToolResult, EndOutput, error) {

	if why := CheckBlockedReason(in.Reason); why != "" {
		return nil, EndOutput{}, fmt.Errorf("%s", why)
	}
	out, err := d.Report(ctx, req, map[string]any{
		"status": "blocked", "ask": strings.TrimSpace(in.Reason), "ended": true,
	})
	return nil, out, err
}

// AddEndTools puts both tools on a server that has no class of its own (the stdio one).
func AddEndTools(s *mcp.Server, d EndDoor) {
	mcp.AddTool(s, &mcp.Tool{Name: "atrium_done", Description: doneToolDesc}, d.Done)
	mcp.AddTool(s, &mcp.Tool{Name: "atrium_blocked", Description: blockedToolDesc}, d.Blocked)
}
