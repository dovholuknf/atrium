The hub's control MCP serves `atrium_close`, the same close the board and the CLI's MCP already had: a preview without
`confirm`, then stop the session, archive its review and remove the worktrees, branches and refs, with keep, stash or
delete asked for work that is nowhere else. Workers get it too, so a card clearing up cards calls the tool instead of
scripting `POST /v1/tasks/{id}/close` by hand. A card on another room is `name@room` or `room~id`.
