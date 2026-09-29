# sa80 handoff

Branch `claude/sa80`. Read BRIEF.md first, then `## 80.` in docs/backlog-2.md.

## Built and committed (Go, tested)

- `internal/store/usagebuckets.go`: `UsageBuckets(since, until, bucketSecs, card)`. SQL-summed, 500 buckets max, 30 days back
  max (a too-narrow width is widened and reported in `bucket`). Per bucket: `t`, `total`, `cards{id}`, `causes{cause}`,
  raw tokens per kind plus stored cost. NO per-kind dollars. Empty buckets are left out.
  DESIGN ADDITION: an optional `card` id narrows the read to one card. Needed because a bucket carries causes only at
  board level, so "filter everything to that card" could not show that card's cause table otherwise. Tell the director.
- `GET /v1/usage?since=&bucket=&card=` in `internal/api/usage.go`, route in api.go. 400 on a bad since or bucket.
- `usage` SSE event: `usageTracker.emitRow` in `internal/daemon/usage.go` (turn row and subagent row), and the keep-alive
  refresh row is wrapped in daemon.go (`d.ka.spent`). Payload: task_id, ended_at, cause, the five token kinds, cost.
  No message text. `internal/link/events.go` `taggedFields["usage"] = task_id`, so the hub adds `room` and tags the id.
- Tests, all passing: `go test ./internal/store ./internal/api ./internal/daemon -run Usage`, `./internal/link -run UsageEvent`.

## Edited but NOT committed, NOT yet checked (front end wiring)

- index.html: `usage` tab after `history`; `<div id="usage">` (toolbar ids `uc-ranges` buttons with `data-range`,
  `uc-room` select, `uc-card` filter chip, body `uc-body`, count `uc-count`); `<div id="d-usage-chart">` at the top of the
  card details' usage box; `<script src="/js/usage-charts.js">` after usage.js. THE JS FILE DOES NOT EXIST YET.
- board.js: `"usage"` in VIEWS, and `switchView` calls `loadUsageTab()`.
- settings-spine.js: `usage` SSE listener calls `onUsageEvent(e)`, and stream reopen calls `onUsageStreamOpen()`.
- chrome.css: `#usage` added to the fill-height and flex-column rules, `#uc-body` scrolls.
- usage.js `loadUsage` calls `paintCardUsageChart(current)` (must exist in usage-charts.js, guarded by typeof).
- FAILED, redo: notify.css `body.solo #usage { display: none }`. The hook refuses `!important` even though the rule beside
  it uses it. Add `#usage` to that existing selector list some other way (e.g. edit with the hook satisfied, or hide
  via the solo lock in switchView, which already refuses every view but terms).

## Left to build

1. `internal/api/web/js/usage-charts.js` (classic script, globals, no build). Planned design:
   - State `UC {range, room, card:{room,id}|null, rooms:{name:{state ok|old|down|absent, why, buckets}}, since, bw}`.
     Ranges 1h/60s, 6h/300s, 24h/900s, 7d/3600s. `since` floored to seconds, bucket starts normalised to that grid.
   - `loadUsageTab()`: targets = `roomNow()` if set, else `hubRooms` names if `hubIsHub`, else `[""]`. Inventory rooms not
     attached (`hubInventory`, transport not local) are named "not attached". Per room:
     `fetch("/v1/usage?since=..&bucket=..", {headers:{"X-Atrium-Room": room}})` (explicit header, as `loadRoomCfg`). 404 =
     "too old", other failure = "did not answer". Name each missing room in the tab, never count it as zero. With a card
     filter also fetch `&card=<bare id>` from that card's room for the cause table and cumulative.
   - Keys are `room + "|" + bare id` everywhere (filter, small multiples, "others"), never id or title alone.
     Titles from `lastTasks` (`display_title`), matched by bare id and room (`x.room || roomOf(x.id) || roomNow()`).
   - `onUsageEvent(e)`: room = `d.room || roomNow() || (hubIsHub && hubRooms.length===1 ? hubRooms[0].name : "")`; id =
     `d.task_id` with `room~` stripped when `d.room` set. Add to the newest bucket (create it), room total, card, cause.
     Buffer events while a load is in flight, apply after. Repaint throttled, only when `isViewing("usage")`.
     `onUsageStreamOpen()` reloads when the tab is open.
   - Charts as inline SVG, colours only as `rgb(var(--teal-rgb))` style values (skin variables, so a skin change
     recolours live): input `--warn-rgb`, out `--danger-rgb`, cache read `--teal-rgb`, write 5m `--path-rgb`, write 1h
     `--stroke-rgb` (or path at .55 opacity). Labels and tips from `USAGE_TIPS` and the label strings in usage.js
     ("uncached in", "out", "cache read", "cache write 5m", "cache write 1h", "est."). Chart 1 stacked bars tokens/min
     (value / bucket width in minutes) with hover readout. Chart 2 top 12 cards by cost plus "others", small multiples,
     click sets the card filter (path data in `data-` attributes). Chart 3 one stacked bar of tokens per kind plus total
     cost. Chart 4 cumulative cost line plus cost-by-cause table (labels `USAGE_CAUSES`).
   - `paintCardUsageChart(t)`: 24h small stacked chart in `#d-usage-chart` from `/v1/usage?card=<bare id>&bucket=900`
     with the card's room header, and a link that closes `#detail`, sets `UC.card`, `switchView("usage")`.
   - CSS for `.ucranges`, `.ucfilter`, `.ucmini`, `.ucdetail`, legends: append to css/files.css beside the usage block.
     Palette triples only, no new hex, no `!important`.
2. Headless section `usageCharts` in `scripts/test-board-headless.js`, registered in the HEADLESS_ONLY table (line ~5804).
   Pattern: `ctx.route("**/v1/usage*", ...)` mocks (see `peekEverywhereSection`), `hubMode = true` and
   `sggAttached = true` for two rooms (`ALPHA`, `SGG`), tagged `/v1/tasks` via route, events pushed by
   `hubStreams.forEach(r => r.write("event: usage\ndata: {...}\n\n"))`. Cover: tab renders from mocked data, a `usage` event
   grows the newest bucket, two rooms with the same card id kept apart live, a missing room named, 78's labels present,
   a skin change recolours (compare computed fill before and after `data-skin` change).
   Run: `HEADLESS_ONLY=usageCharts,contextSize`, NODE_PATH=D:/worktrees/claude/atrium/attach-loop/node_modules. Not the
   whole suite.
3. `bash scripts/check-board.sh` and `bash scripts/check-skins.sh`.
4. `docs/changes/80.md` (Changelog and Test plan sections), a status paragraph on item 80 in docs/backlog-2.md.
   Do not edit CHANGELOG.md or docs/test-plan.md.

## Hook quirks hit

Bash hook refuses `;`, `>` and `2>&1`, sed with quote tricks, and `!important` in CSS edits. Use the Edit and Write tools.
