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
- Merged claude/main c9d65765. No conflicts. 0010 was the last hub migration there, so 0011 stays.
- `internal/cli/backlog.go`: `atrium backlog list|show|file|status` and `atrium reports list|read|add` over the hub
  routes, `--board-addr`, body from stdin with `--body -`, filer from ATRIUM_AGENT_NAME and ATRIUM_ROOM. Tests in
  `internal/cli/backlog_test.go` against a fake hub.
- Board: a read-only `backlog` tab on a hub (`js/backlog.js`, `index.html`, wired in board.js, rooms.js and
  settings-spine.js), items filtered by department and open only, and the reports, live on `backlog` and `report` events.

## Left

- Import of `docs/backlog/**` and `QUEUE.md`, and markdown export. Needs clint's call (questions are in the item file).
- Atrium-given ids. Same, clint's question.
- Status that follows the card. Not small: it needs a link from a card to an item id and a hook on the card's verdict
  and landing, so it is left.
- A queue per director.
- The board tab has no browser test, I did not open it in a browser.

## Verify

`go test ./internal/cli ./internal/hubstore` pass. `./internal/link` and `./internal/api` failures match a clean
claude/main run by name (git commit signing key and the Windows NUL path). `TestTheHubRaisesTheForgeAlertOnceAndEndsItOnSuccess`
is flaky: it failed once on clean claude/main in this session and intermittently here, and I touched no forge code. `go build -o build.claude/atrium.exe
./cmd/atrium` is clean.
