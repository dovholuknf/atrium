## Test plan

## @LETTER@. The launch ending text carries the three ending rules

### @LETTER@1. The text is the same in all three places

1. Read `LaunchEndingLine` in `internal/daemon/launch.go` and `reportLine` in `internal/link/control_mcp.go`.
2. Read `leanSystemPrompt` in `internal/daemon/lean.go`.

**Expected:** each ends with the paragraph that says to report to the launcher and not the operator, never to ask where
to push or offer a push to origin, and that a launcher's "stop" is answered with atrium_done or with nothing. The first
two are equal.

### @LETTER@2. A launched worker sees it

1. Launch a worker with a short prompt.
2. Read the prompt it received.

**Expected:** the prompt ends with the paragraph, once.
