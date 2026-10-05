# f-new-reports-channel-any-room

## Done

- Hub migration `0011_backlog_reports` (appended last in `internal/hubstore/schema.go`, since the hub's tables live there
  and not in `internal/store`): `backlog_item` and `director_report`.
- `internal/hubstore/backlog.go`: file, get, list, set status for items, and add, list, read for reports.
- `internal/link/backlog.go`: `/_hub/backlog` and `/_hub/reports` routes, reads open, writes from the hub's machine
  only, `backlog` and `report` events, audit lines. Wired in `internal/cli/atrium_run.go`.
- `internal/link/backlog_mcp.go`: full class tools `atrium_backlog` and `atrium_reports`.
- Interim rule and design in `docs/rnd/reports-channel-design.md`. Item file updated, changelog written.
- Tests: `hubstore/backlog_test.go`, `link/backlog_test.go`. The pinned last-migration test in
  `changerequest_test.go` now expects 0011.

## Left

- Import of `docs/backlog/**` and `QUEUE.md`, and markdown export. Needs clint's call (questions are in the item file).
- Atrium-given ids, status that follows the card, per-director queue, board view, `atrium backlog` command line.
- No board UI for either table.

## Verify

`go test ./internal/hubstore ./internal/cli` pass. `go test ./internal/link -run 'Backlog|Reports'` passes. The full
`./internal/link` run on this Windows box has git-dependent failures (`unable to access 'NUL'`, for example
`TestAHostThatIsNotANameAndAPortIsNotMadeIntoAURL`) that touch no code here. The 32 failing tests fail identically on clean claude/main 7a0522cc on sg3.
`go build -o build.claude/ ./...` is clean.
