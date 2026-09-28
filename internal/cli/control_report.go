package cli

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// atrium_report on the stdio server, for a room whose sessions have no hub
// control MCP to call. The same shape as the hub's, and the same endpoint on
// the room: it validates the report and tells the launcher, on this room or
// another. See internal/daemon/finish.go and docs/cross-room-say-design.md.

type ReportInput struct {
	Status   string `json:"status" jsonschema:"done, blocked, question or progress"`
	Summary  string `json:"summary" jsonschema:"what happened, in your words. your launcher reads it verbatim"`
	SHA      string `json:"sha,omitempty" jsonschema:"for done: the commit the work landed as"`
	NoCommit string `json:"no_commit,omitempty" jsonschema:"for done with no commit: why there is none"`
	Ask      string `json:"ask,omitempty" jsonschema:"for blocked or question: what you need, and from whom"`
}

type ReportOutput struct {
	Recorded     bool   `json:"recorded"`
	Status       string `json:"status"`
	Unverified   bool   `json:"unverified,omitempty"`
	LauncherTold bool   `json:"launcher_told"`
	Note         string `json:"note,omitempty"`
}

func reportHandler(ctx context.Context, _ *mcp.CallToolRequest, in ReportInput) (
	*mcp.CallToolResult, ReportOutput, error) {

	out := ReportOutput{}
	me := strings.TrimSpace(os.Getenv("ATRIUM_AGENT_NAME"))
	if me == "" {
		return nil, out, fmt.Errorf("atrium_report is for a session atrium launched. this one has no " +
			"ATRIUM_AGENT_NAME, so there is no card to report on")
	}
	id, _, err := resolvePeer(ctx, me)
	if err != nil {
		return nil, out, err
	}
	var res struct {
		Recorded     bool   `json:"recorded"`
		Status       string `json:"status"`
		Unverified   bool   `json:"unverified"`
		LauncherTold bool   `json:"launcher_told"`
	}
	if err := ask(ctx, http.MethodPost, "/v1/tasks/"+url.PathEscape(id)+"/report", map[string]string{
		"status": strings.TrimSpace(in.Status), "recap": in.Summary, "sha": strings.TrimSpace(in.SHA),
		"no_commit": in.NoCommit, "ask": in.Ask,
	}, &res); err != nil {
		return nil, out, err
	}
	out.Recorded, out.Status, out.Unverified, out.LauncherTold = res.Recorded, res.Status, res.Unverified, res.LauncherTold
	switch {
	case res.Unverified:
		out.Note = "recorded, but that commit is not in your worktree, so the card is flagged. if it " +
			"landed somewhere else, say where with atrium_say."
	case !res.LauncherTold:
		out.Note = "recorded on your card. nobody launched you, so there was nobody else to tell."
	}
	return nil, out, nil
}

// peersRoomsPath is the room's list of sessions on other rooms.
func peersRoomsPath(all bool) string {
	if all {
		return "/v1/peers/rooms?all=1"
	}
	return "/v1/peers/rooms"
}
