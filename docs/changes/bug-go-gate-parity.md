# bug-go-gate-parity

The Go gate (`ATRIUM_GUARD=on`) had three differences from the PowerShell gate it replaces.

- `git -C <dir>` and `git --git-dir` are blocked by the new `git-dir-option` rule.
- A subagent type with a file in `~/.claude/agents/<type>.md` is allowed by `checkSubagent`.
- The `bash-semicolon` and `bash-redirect` rules are removed, so `;` and `>` are allowed.

## Test plan

- `go test ./internal/guard` runs the table in `guard_test.go` and `TestUserAgentsDirExempt`.
- `go test -tags integration ./internal/guard` runs `TestGitGuard` with `git -C` blocked.
