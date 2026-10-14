## Decisions

- A room's lean worker reaches atrium-control over the room's stdio server (`atrium control`), which had no
  `atrium_publish`. The hub's HTTP server (`/_hub/mcp`) always had it in every worker class, so the gap was the stdio
  path only.
- The stdio tool rides the room link the way `atrium_backlog` does: one `hub` relay to the hub, which answers
  `/_hub/publish` in process (`hubAsk`, `publishAsk`). It is not an HTTP endpoint of the hub. The room and the card
  come from the link and the sender the room names, never from the call.
- `daemon.PromptToolNames` reads the tools the worker and interviewer prompts name. A test on each control path asserts
  the set it serves covers them. Only `atrium_publish` was missing. Every other named tool already existed on both.

## Test plan

## @LETTER@. A room's lean interviewer can publish

### @LETTER@1. Publish from a room on the stdio server

1. Launch a lean interview on a room whose atrium-control is `atrium control` (stdio).
2. Have it call `atrium_publish` with a title and content.

**Expected:** a link under `/d/` is returned and the document shows the card as its origin.
