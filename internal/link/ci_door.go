package link

import (
	"context"
	"fmt"
	"strings"

	"github.com/dovholuknf/atrium/internal/forge"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// CIDoor is atrium_ci, ONE CODE for the two servers that offer it, as GitDoor is for the git tools: the hub's control
// over HTTP (registerGit) and a room's stdio `atrium control` (internal/cli/control_ci.go). What differs is how each
// reaches the hub's forge, which the door is given. The tool is read only and runs no forge CLI where it is called.
type CIDoor struct {
	// Ask puts a CI question to the hub and answers what its forge said.
	Ask func(ctx context.Context, req *mcp.CallToolRequest, in forge.HubCIAsk) (forge.HubCI, error)
}

// AddCITool puts the tool on a server that has no class of its own (the stdio one).
func AddCITool(s *mcp.Server, d CIDoor) {
	mcp.AddTool(s, &mcp.Tool{Name: "atrium_ci", Description: ciToolDesc}, d.Call)
}

const ciToolDesc = "Read a repository's CI from the forge: runs, a run's jobs and steps, a failed job's log, and a " +
	"run's artifacts. READ ONLY, and answered by the HUB, which has gh. A room never runs gh, so do not ask the " +
	"person to run `gh run view`: call this.\n\n" +
	"`action` is one of:\n" +
	"- `runs`: the runs of `repo`, newest first, for `branch` and/or `sha`, at most `limit` (default 10, max 50). " +
	"Each has id, workflow, status, conclusion, head_sha, branch, created and url.\n" +
	"- `run`: one run's jobs and each job's steps with their conclusions. Needs `run_id`.\n" +
	"- `log`: the END of a job's log (`job_id`) or of a run's failed jobs (`run_id`, failed steps only by default, " +
	"like `gh run view --log-failed`). The last `tail` lines (default 400, max 5000) within 64 KiB, and `truncated` " +
	"says earlier lines were left out. Set `failed_only` false to read a run's whole log, or true to read only the " +
	"failed steps of a job.\n" +
	"- `artifacts`: the names, sizes and expiry of a run's artifacts. `ci.sh` uploads build.claude/ci.\n" +
	"- `artifact`: the hub downloads the artifact `name` of `run_id` (at most 200 MiB) into a folder on the hub, and " +
	"answers that folder and its files. Pass `file` (a path from `files`) to read the last lines of one of them.\n\n" +
	"`repo` is `<owner>/<repo>` or `<host>/<owner>/<repo>`. Bitbucket answers `not supported on bitbucket`. A forge " +
	"login the hub lacks is answered with the exact command to run ON THE HUB to fix it."

type ciInput struct {
	Action     string `json:"action" jsonschema:"runs, run, log, artifacts or artifact"`
	Repo       string `json:"repo" jsonschema:"the repository: <owner>/<repo> or <host>/<owner>/<repo>"`
	Branch     string `json:"branch,omitempty" jsonschema:"runs: only runs of this branch"`
	SHA        string `json:"sha,omitempty" jsonschema:"runs: only runs of this commit"`
	Limit      int    `json:"limit,omitempty" jsonschema:"runs: how many, default 10, at most 50"`
	RunID      int64  `json:"run_id,omitempty" jsonschema:"the run, for run, log, artifacts and artifact"`
	JobID      int64  `json:"job_id,omitempty" jsonschema:"log: one job's log"`
	FailedOnly *bool  `json:"failed_only,omitempty" jsonschema:"log: only the failed steps. default true for a run and false for a job"`
	Tail       int    `json:"tail,omitempty" jsonschema:"log and artifact file: the last N lines, default 400, at most 5000"`
	Name       string `json:"name,omitempty" jsonschema:"artifact: the artifact's name, from artifacts"`
	File       string `json:"file,omitempty" jsonschema:"artifact: a file of it, to read its last lines"`
}

// ciRepo splits `repo` into host, owner and name. Two parts is github.com.
func ciRepo(s string) (host, org, repo string, err error) {
	s = strings.Trim(strings.TrimSpace(s), "/")
	s = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(s, "https://"), "http://"), ".git")
	parts := strings.Split(s, "/")
	switch len(parts) {
	case 2:
		return "github.com", parts[0], parts[1], nil
	case 3:
		return strings.ToLower(parts[0]), parts[1], parts[2], nil
	}
	return "", "", "", fmt.Errorf("say the repository as <owner>/<repo> or <host>/<owner>/<repo>, not %q", s)
}

// Call is atrium_ci.
func (d CIDoor) Call(ctx context.Context, req *mcp.CallToolRequest, in ciInput) (*mcp.CallToolResult, forge.HubCI, error) {
	var out forge.HubCI
	host, org, repo, err := ciRepo(in.Repo)
	if err != nil {
		return nil, out, err
	}
	out, err = d.Ask(ctx, req, forge.HubCIAsk{Host: host, Org: org, Repo: repo, Action: strings.TrimSpace(in.Action),
		Branch: in.Branch, SHA: in.SHA, Limit: in.Limit, RunID: in.RunID, JobID: in.JobID, FailedOnly: in.FailedOnly,
		Tail: in.Tail, Name: in.Name, File: in.File})
	return nil, out, err
}

// ciHandler is the hub's: the question goes to the hub's own forge route on the board.
func (c *controlMCP) ciHandler(ctx context.Context, req *mcp.CallToolRequest, in ciInput) (
	*mcp.CallToolResult, forge.HubCI, error) {
	return CIDoor{Ask: func(ctx context.Context, req *mcp.CallToolRequest, a forge.HubCIAsk) (forge.HubCI, error) {
		var ans forge.HubCI
		err := c.longClient().ask(ctx, "POST", "/_hub/forge/ci", roomOf(req), a, &ans)
		return ans, err
	}}.Call(ctx, req, in)
}
