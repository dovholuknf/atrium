package link

import (
	"context"

	"github.com/dovholuknf/atrium/internal/base/calllog"
	"github.com/dovholuknf/atrium/internal/link/ctl"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// logCalls is the one place every control tool call is logged, ahead of the tool, so a call that never returns is
// still a start line.
func (c *controlMCP) logCalls(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		call, ok := req.(*mcp.CallToolRequest)
		if method != "tools/call" || !ok || call.Params == nil {
			return next(ctx, method, req)
		}
		end := c.calls.Begin("mcp", call.Params.Name, ctl.AgentOf(call), ctl.RoomOf(call))
		res, err := next(ctx, method, req)
		if r, isTool := res.(*mcp.CallToolResult); err == nil && isTool && r != nil && r.IsError {
			end(calllog.Error, nil)
			return res, err
		}
		end(calllog.Outcome(ctx, err), err)
		return res, err
	}
}
