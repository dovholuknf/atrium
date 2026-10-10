## Design

A worker on a room that is not the hub's machine got atrium-control as stdio (`atrium control`), which talked only to
its own room and had no `atrium_backlog` or `atrium_reports`. Those two lived only on the hub's HTTP control MCP.

The chosen path is to forward from the stdio server, over the link the room already holds. The other option, pointing
a room launch at the hub's `/_hub/mcp`, would need the room's session to reach the hub's loopback listener and to
carry the hub's caller headers, which is a new listener or a new credential. Forwarding adds neither.

The path is:

1. The stdio server registers both tools (`internal/cli/control_backlog.go`, tool code shared in
   `internal/link/backlog_mcp.go`) and posts to its room's `POST /v1/hub/ask`.
2. The room's daemon (`internal/daemon/hubask.go`) sends one `hub` relay op over the link, the same relay `atrium_say`
   uses to reach other rooms.
3. The hub (`internal/link/hubask.go`) serves only `/_hub/backlog` and `/_hub/reports` and what hangs off them, GET and
   POST. `/_hub/backlog/follow` is refused. It asks its own board over loopback and returns the answer.

`by` and `room` on a write are set by the hub from the link, so a room cannot file a row as another room. The same
fix covers `atrium_reports`. The stdio tools are offered to every session on the room, as its other stdio tools are,
so the hub's full-class gate on these two tools does not apply on this path. An older room or hub says so in a
sentence.

## Test plan

## @LETTER@. A room's worker reaches the hub's backlog

### @LETTER@1. Read the hub's row from a room

1. On the hub's machine, file an item with `atrium_backlog` (`file`, id `f-probe`).
2. From a worker on another room, call `atrium_backlog` with `get` and id `f-probe`.

**Expected:** the worker receives the row, body included.

### @LETTER@2. File and report from a room

1. From the same worker, `file` an item and `add` a report with `atrium_reports`.
2. On the hub, `get` the item and `list` the reports.

**Expected:** both are there, filed from the worker's room name and `handle@room`, whatever the worker passed.

### @LETTER@3. A hub that is down

1. Detach the room from its hub, then call `atrium_backlog` `list` from its worker.

**Expected:** a sentence saying the hub is not answering, and nothing is held.
