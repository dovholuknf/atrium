## Test plan

## @LETTER@. A hub restart does not cost a session its atrium tools

### @LETTER@1. Relay survives a restart

1. On a throwaway hub, register `atrium mcp-relay` as a stdio MCP server in a session's MCP config, with
   `ATRIUM_AGENT_NAME` and `ATRIUM_ROOM` set.
2. Stop the hub, wait a few seconds, start it again on the same port.
3. Call `atrium_status` from the session while the hub is down, or right after it returns.

**Expected:** the call waits and then answers once the hub is back. The session was not restarted.

### @LETTER@2. A hub that stays down

1. Stop the throwaway hub and leave it down past `--retry-for` (use a short value).
2. Call an atrium tool.

**Expected:** the call fails with "atrium hub unreachable", and works again on the next call after the hub returns.
