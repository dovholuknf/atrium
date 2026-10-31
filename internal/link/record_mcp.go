package link

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dovholuknf/atrium/internal/link/ctl"
)

// atrium_record: a worker saying what a test run found, pinned to the commit it ran on. See work_record.go for the rules and
// docs/rnd/item-lifecycle-design.md section 4.
//
// THERE IS NO SHA FIELD, AND NO KIND BUT `test`. The room reads the commit from the card's own worktree at the moment of the
// call, with a dirty flag, and the hub refuses a worktree on another branch than the item's. A reported test is shown on the
// item with the `reported` tag and does not make it ready: only a test atrium saw counts.

const recordToolDesc = "Record a test run against the item you are working on. Call it after you run the tests, with " +
	"the command, `pass` or `fail`, and the log. Atrium pins it to the commit your directory is at RIGHT NOW (and says " +
	"so if there are uncommitted changes), so commit first and run the tests on the commit. You cannot name a commit, and " +
	"you cannot record a review: atrium reads reviews itself. The call is refused when your directory is on a branch that " +
	"is not the item's. A test you report is shown as reported, and it does not make the item ready: only a test atrium " +
	"ran or saw does. `log` is the output as text (up to 512 KB) or `log_file` is a file inside your directory, and atrium " +
	"keeps a copy outside the repository."

type recordInput struct {
	Kind     string  `json:"kind,omitempty" jsonschema:"what is recorded. only test"`
	Cmd      string  `json:"cmd" jsonschema:"the command you ran, up to 300 bytes"`
	Result   string  `json:"result" jsonschema:"pass or fail"`
	Duration float64 `json:"duration_s,omitempty" jsonschema:"optional: how many seconds the run took"`
	Log      string  `json:"log,omitempty" jsonschema:"optional: the output of the run, as text. up to 512 KB"`
	LogFile  string  `json:"log_file,omitempty" jsonschema:"optional: a log file inside your own directory, which atrium copies"`
}

func (c *controlMCP) recordHandler(ctx context.Context, req *mcp.CallToolRequest, in recordInput) (
	*mcp.CallToolResult, RecordOut, error) {

	var out RecordOut
	if k := strings.ToLower(strings.TrimSpace(in.Kind)); k != "" && k != "test" {
		return nil, out, fmt.Errorf("a card records a test and nothing else. atrium reads a %s itself", k)
	}
	me := ctl.AgentOf(req)
	if me == "" {
		return nil, out, fmt.Errorf("this is for a session atrium knows. this call did not say which one it is")
	}
	room := ctl.RoomOf(req)
	id, _, err := c.resolvePeer(ctx, room, me)
	if err != nil {
		return nil, out, err
	}
	if err := c.ask(ctx, http.MethodPost, "/_hub/work/record", "", map[string]any{
		"room": room, "card": id, "kind": "test", "cmd": in.Cmd, "result": in.Result, "duration_s": in.Duration,
		"log": in.Log, "log_file": in.LogFile,
	}, &out); err != nil {
		return nil, out, err
	}
	return nil, out, nil
}
