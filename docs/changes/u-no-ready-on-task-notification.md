The Stop hook reads the turn's last real prompt from the transcript tail. When it is a tag Claude Code wrote
(`<task-notification>`, the same line `internal/daemon/replies.go` draws) it posts `own_turn`. The room stores that in
`turn_seen.turn_own` (migration 0104) unless the turn left an Open Questions block, and the seen row carries it as
`own_turn`. The hub's `cardReason` skips input and finished for such a turn. Questions, a report, a permission and an
`asked` wait still notify.

## Test plan

## @LETTER@. A background shell finishing does not ring twice

### @LETTER@1. Wait on a shell

1. In a session with the Stop hook installed, start a background shell that exits in a few seconds and end the turn.
2. Wait for the shell to exit and for Claude Code to run its `<task-notification>` turn.

**Expected:** one ready notice for the first turn end, none for the notification turn.

### @LETTER@2. A person speaks, and a question still rings

1. Type a prompt and let the turn end.
2. Repeat, with a notification turn that ends with an `Open Questions:` block.

**Expected:** the typed turn raises a ready notice, and the notification turn that asked raises a question notice.
