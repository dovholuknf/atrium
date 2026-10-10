## Why

`atrium_close` was registered only in the CLI's stdio MCP (`internal/cli/close.go`). A session on the hub's control
MCP (`/_hub/mcp`, `internal/link/control_mcp.go`) could not see it, so the card-audit card wrote one-off scripts against
`/v1/tasks/{id}/close` to clear cards.

- `closeHandler` in `internal/link/control_mcp.go` resolves the card like every other card tool, then forwards to the
  card's own room: `GET .../close` for the preview, `POST .../close` with `confirm` and `answers` to close.
- It is in the worker set (`ctlclass.go`), because the cards that clear up cards are workers.
- A confirmed close is recorded in the hub audit as `ctl-close`. A preview is not.

## Test plan

## @LETTER@. A session on the hub's control MCP can close a card

### @LETTER@1. Preview, then close

1. From a worker session on any room, call `atrium_close` with `card` set to a finished card and no `confirm`.
2. Call it again with `confirm: true` and an answer for each ask.

**Expected:** the first call lists the items and asks and changes nothing. The second closes the card: it goes to
done, its worktree and branch are gone, and the hub audit has one `ctl-close` line.

### @LETTER@2. Another room

1. Call `atrium_close` with `card` as `name@room` for a card on another room, without `confirm`.

**Expected:** the preview comes from that room.
