- **No test can reach a live room.** The daemon, cli, api and link test binaries clear every `ATRIUM_*` variable
  before any test runs and point `ATRIUM_LOCATION` at a file that does not exist (`internal/testguard`). The daemon's
  tests also run with an empty home directory, so nothing they start finds agent hooks and nothing they write reaches
  the real `~/.claude.json`. And an agent (claude, codex, gemini and the rest) is never started for real in a daemon
  test: its command becomes a shell that waits, or for a keep-alive fork a command that fails at once. This replaces
  per-test care, which twice let a test's runner put cards on the live board. `go test` no longer needs
  `ATRIUM_LOCATION` cleared by hand in a room session. Tests only.
