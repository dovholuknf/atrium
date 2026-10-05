# r-new-edited-input-unparsed: an approval whose edit does not parse runs the original

Status: HELD (pause). Medium. Owned by @runtime. Filed by the orchestrator 2026-10-01, from @review's re-read of the
opencode runner (7fda0756, appended to docs/backlog/runtime/r-new-review-4ea3dc66.md).

## What is wrong

`editedInput` in `internal/cli/hook_permission.go` returns nil when the human edited a raw-JSON summary on the board
and the edit does not parse. The hook then sends a plain allow with no updatedInput, so Claude (and opencode) run
the ORIGINAL tool call, the one the human changed. The human believes the edit ran.

## Wanted

- An approval that carries an edit which does not parse is a deny with the reason "the edit did not parse, nothing
  was run".
- A test that proves it, for a raw-JSON tool (an MCP call or apply_patch).

## Also for @runtime, found in the same review

`internal/daemon/fyi_test.go` fails gofmt on claude/main (since 430234c3), so the gofmt step in `scripts/ci.sh` is
red. One gofmt run.
