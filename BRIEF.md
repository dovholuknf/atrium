# sa80: real-time token burn and usage charts (backlog-2 item 80)

You are a worker for @ui, the director of the board. clint wants this TONIGHT. Your branch is `claude/sa80` in this
worktree. It already has `claude/ui` merged in, which carries item 78's relabelled usage (`USAGE_TIPS`, `usageOwn`
in `internal/api/web/js/usage.js`) and the final design.

## The job

The design is `## 80.` in `docs/backlog-2.md`. It went through three Mercurius review rounds and every finding is
folded in. BUILD IT AS WRITTEN. Read it in full, including the notes marked "Mercurius round N". Key points:

- Nothing new is recorded. The data is `session_usage` (item 37): `internal/store/usage.go`, migration
  `0063_session_usage`, written by `internal/daemon/usage.go` and `keepalive.go` via `AddSessionUsage`. Today it is
  served per card at `GET /v1/tasks/{id}/usage` (`cardUsage` in `internal/api/api.go`).
- New room endpoint `GET /v1/usage?since=&bucket=`: buckets summed in SQL, bounded (500 buckets max, 30 days back
  max), per bucket the board total plus per card and per cause, raw tokens per kind plus the stored cost summed. NO
  per-kind dollars.
- A `usage` SSE event on the existing stream when a row is written, carrying that row and its card id, and its
  source room when forwarded by the hub. Find how the hub forwards other card events and follow that.
- A new `usage` tab beside `history` (see the `data-view` tabs in `index.html`), a small per-card chart in the card
  details' usage section with a link to the tab filtered to that card.
- Four charts: board burn rate over time stacked by kind with a 1h/6h/24h/7d range picker, per-card small
  multiples (top 12 by cost plus "others"), a split bar of tokens per kind with total cost, cumulative cost plus a
  cost-by-cause table.
- Hand-drawn inline SVG in a new `js/usage-charts.js`. NO chart library, no CDN, no vendoring: offline, no build
  step. Colours from skin variables (palette triples, see internal/api/web/CLAUDE.md), so `scripts/check-skins.sh`
  still passes.
- Cross-room: explicit per-room reads with the `X-Atrium-Room` pattern `loadRoomCfg` uses in `js/rooms.js`, every
  per-card key is room plus card id, a room that does not answer or is too old is named, never counted as zero.
- Labels are item 78's, reusing `USAGE_TIPS`.

If you find the design wrong or impossible somewhere, do not guess: atrium_report status question with what and why,
to your launcher. Keep going on the parts it does not block.

## Tests, targeted only
- Store and API: `go test ./internal/store -run Usage`, `go test ./internal/api -run Usage`, `go test ./internal/daemon
  -run Usage` (name your tests so `-run Usage` catches them). Clear `ATRIUM_LOCATION` and `ATRIUM_DEBUG_INPUTLAG`
  in the environment first.
- `bash scripts/check-board.sh` and `bash scripts/check-skins.sh`.
- Headless: add a `usageCharts` section to `scripts/test-board-headless.js` (Playwright, `wholeBoard()`, mocked
  endpoints, no atrium process) covering the tab from a mocked `/v1/usage`, a mocked `usage` SSE event growing the
  newest bucket, two rooms with the same card id kept apart live, 78's labels, a skin change recolouring. Run with
  `HEADLESS_ONLY=usageCharts,contextSize`. Set NODE_PATH to D:/worktrees/claude/atrium/attach-loop/node_modules. Do
  NOT run the whole suite.

## Finishing
- Write `docs/changes/80.md` with a "Changelog" section and a "Test plan" section. Do NOT edit `CHANGELOG.md` or
  `docs/test-plan.md`. Add a short status paragraph to item 80 in `docs/backlog-2.md`.
- Commit on `claude/sa80` in small steps with one-line messages under 30 words, meaningful, no "WIP", no co-author
  or trailer.
- `atrium_report` status done with the sha, what was built, what was not, and anything the design got wrong. Stay
  up after reporting: the director may send you back for fixes.

## Rules
- No merge, no push, no pull, no fetch, no deploy. Never restart or touch the live room or hub. No restart_atrium.
- Go builds go to build.claude/. Do not edit CLAUDE.md files.
- Hooks refuse `;` chaining, `cd x && y`, `git -C`, and `>` redirection in Bash. Put multi-step pwsh in a .ps1 and
  run it with `& path`. Do not use python.
- Prose and UI text: no em-dashes, no double-hyphen dashes, no semicolons, wrap markdown at 120.
- Watch your context. Past about 150k tokens, commit, write HANDOFF.md with where you are, and atrium_report
  progress asking for a new context.
