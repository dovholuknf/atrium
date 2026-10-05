# f-new-reports-channel-any-room, finish 2

## Done

- Rebased on claude/main bcdbb267. Conflicts were `ctlclass_test.go` (both tool sets kept: `atrium_backlog`,
  `atrium_reports` and `atrium_resources`), `cli.go` and `rooms.js`. `control_mcp.go`, `proxy.go` and `ctlclass.go`
  needed nothing. No other hub migration took 0011.
- Migration 0011 is not in claude/main yet, so I edited it in place: statuses `built`, `blocked` and `incomplete`, and
  a `card` column on `backlog_item`.
- Decisions written into the item file under "Decided by @fabric, 2026-10-05", and built:
  - `atrium backlog import <dir> [--dry-run]`. One shot, upsert by id through `POST /_hub/backlog {upsert, status}`,
    an unmoved item changes nothing. Skips QUEUE.md, NIGHT-*, HANDOFF*, and also README*, HISTORY* and
    REVIEWER-NOTES* (my addition, they are not items). Status from the first word of the `Status:` line: held, parked
    to held, built, done and fixed to done, dropped, blocked, else open. Against a temp store the real docs/backlog
    gives 532 added, 0 failed, 7 skipped. Never run against the live hub.
  - Interim rule in the design doc now says the files in git are the source of truth and the hub is a mirror.
  - Ids: an item filed with no id takes `<prefix>-<n>` (f, r, u, m, rnd, plus t and review for the two other folders),
    n one above the highest numeric id of that prefix, taken in the filing's transaction. `atrium backlog file [id]`.
  - Status that follows the card: a launch tagged `item:<id>` links the item (`card` is `room~id`) and sets
    in-progress, in `launchOnRoom` so a relayed launch does it too. `atrium_report` then calls
    `POST /_hub/backlog/follow`, which moves the linked item to built on done, blocked on blocked, incomplete on
    incomplete. A question or progress report moves nothing. Both are best effort and never fail the launch or the
    report. No room change was needed, only a column and one route.
  - Per-director queue: not built, as decided.
- Headless board check: a case for the backlog tab in the `core2` unit of `scripts/test-board-headless.js`, with a mock
  for `/_hub/backlog` and `/_hub/reports`. It checks the tab shows on a hub, lists open items only, draws the built
  status and the report, has no write controls, and that the open-only box and the department box go to the hub. I
  installed playwright and chromium here (`npm install`, `npx playwright install chromium`, node_modules is ignored).
  `HEADLESS_UNITS=core,core2` passes, and a deliberately wrong expectation fails with the case's own message.
- Changelog extended. Tests added in hubstore, link and cli.

## Verify

`go build -o build.claude/atrium.exe ./cmd/atrium` is clean. `go test ./internal/hubstore ./internal/cli` pass.
`./internal/link` fails only `TestTheHubRaisesTheForgeAlertOnceAndEndsItOnSuccess`, the known flake. `./internal/api`
fails the ten `TestPRWorktree*` tests here on a missing git signing key (`id_ed25519_sign.pub`), in code this item
does not touch, so I did not compare them against claude/main.

## Left

- Export of the hub back to markdown, and branch, verdict and landing commit on an item: not asked for.
- The card link is made at launch only. A card started by hand with an item in its title is not linked.
