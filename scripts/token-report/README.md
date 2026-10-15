# Token report

The overnight token-efficiency report, repeatable. Three scripts, PowerShell 7 (`pwsh`):

| script | does |
| --- | --- |
| `fetch.ps1` | collects each online room's usage rows and card list into a folder |
| `build.ps1` | turns that folder into three CSVs for a window |
| `run.ps1` | `fetch.ps1` then `build.ps1`, one command |

The baseline is `docs/reports/token-efficiency-2026-10-15.md` and its CSVs in
`docs/reports/token-efficiency-2026-10-15/`.

## Where the inputs come from

`atrium_export` is an MCP tool, but it only calls two room endpoints through the hub and stores the answers in the
drop (`exportHandler` in `internal/link/files_mcp.go`). `fetch.ps1` calls the same endpoints directly, so no card and
no drop file is needed:

- `GET <hub>/v1/tasks` with header `X-Atrium-Room: <room>`, the card list
- `GET <hub>/v1/usage/export?since=<RFC3339>` with the same header, one `session_usage` row per line

The room names come from `GET <hub>/_hub/rooms` (the ones attached), or pass `-Rooms a,b`. `build.ps1` also calls
`GET <hub>/v1/says?since=...` per room (paged with `before`). A room too old for `/v1/says` is warned about and its
says are left out.

Files are named `tasks-<room>-<yyyymmdd-hhmm>.json` and `usage-<room>-<yyyymmdd-hhmm>.jsonl`. If a folder holds
several stamps for a room, `build.ps1` uses the newest. The raw exports hold session ids and card titles, so do not
commit them.

## Run it for a new window

```
pwsh scripts/token-report/run.ps1 -Start 2026-10-16T02:30:00Z -End 2026-10-16T10:45:00Z `
    -InDir $env:TEMP/usage-1016 -OutDir docs/reports/token-efficiency-2026-10-16
```

`-Start` and `-End` are RFC 3339 UTC. `-HubUrl` defaults to `http://127.0.0.1:7778`, and use the address, not
`localhost`. `-Label` names the files `<kind>-<label>.csv` and defaults to `overnight-<start date>`. To run again from
inputs you already have, call `build.ps1` alone with the same `-InDir`. Fetch `-Start` should not be later than the
build's, or rows are missing.

`build.ps1 -LandedFile x.csv` takes a CSV with columns `title,landed` and fills the `landed` column of the cards CSV
for cards whose `title` matches. A card with no match says `none`, which is what the baseline has throughout.

Landed items are not in the CSVs. Count them from `git log claude/main` in the window and put them in the report.

## What the outputs hold

All token columns are summed over the turns that started inside the window.

`tokens-<label>.csv`: one row per session (room and card), biggest cache read first, and a final `ALL` row.

| column | meaning |
| --- | --- |
| room, card | the room and the card's display title, or its id when the card is gone |
| turns | turns in the window |
| say_turns | turns whose cause was a say, a message from another session |
| input | uncached input tokens |
| cache_write | cache creation tokens, 5 minute plus 1 hour |
| cache_read | tokens read from the cache. The bulk of "all tokens" and cheap per token |
| output | output tokens |

`cards-<label>.csv`: one row per card created in the window.

| column | meaning |
| --- | --- |
| card, room, status | as on the board when fetched |
| launcher | `spawned_by`, the session that launched the card |
| model | models used, space separated |
| started, ended | card creation time, and the end of its last turn |
| turns, input, cache_write, cache_read, output | as above, for this card |
| total | input + cache_write + cache_read + output |
| landed | the commit from `-LandedFile`, else `none` |

`says-<label>.csv`: one row per say, counted once. Rows whose `to` holds an `@` are dropped, because they are the
copy seen from the sender's side.

| column | meaning |
| --- | --- |
| time, room | when it was sent, and the room that received it |
| from, to | sender handle, and the receiving card |
| kind | fyi or needs when the API returns it, usually empty |
| when, delivered, channel | `immediate` or `done`, the delivery state, and `hook` or `terminal` |
| chars | length of the text |
| turn_started | a say-caused turn of the receiver began within 10 minutes after it (and 5 seconds before). Each turn is claimed by one say |
| input, cache_write, cache_read, output | that turn's tokens, empty when none started |

## Compare a run with the baseline

Four numbers, from the `ALL` row, the cards CSV and the says CSV:

- **Tokens per landed item, all**: `input + cache_write + cache_read + output` of `ALL`, divided by items landed on
  `claude/main` in the window.
- **Tokens per landed item, fresh**: `input + cache_write + output` of `ALL`, divided by the same count. Cache reads
  are left out because they track context size and turn count more than work done.
- **Coordination share**: the total of the orchestrator and minion sessions in the tokens CSV, over the `ALL` total.
- **Say-started turns**: `say_turns` over `turns` in `ALL`. Then, in the says CSV, the count of `turn_started` rows
  and the sum of their `cache_read` over `ALL` cache read.

Baseline, overnight 2026-10-15 (02:30Z to 10:45Z), from `docs/reports/token-efficiency-2026-10-15.md`:

| number | value |
| --- | --- |
| all tokens | 85.1M |
| fresh tokens | 1.72M |
| items landed | 10 (8 not counting two worked before the window) |
| all per item | 8.5M (10.6M for 8) |
| fresh per item | 172k (215k for 8) |
| coordination share | 40 percent (orchestrator 13.7M plus minion 20.4M) |
| turns, say-started | 85, 52 (61 percent) |
| says, turn-starting | 62, 49, reading 29.4M cache (35 percent of cache read) |
| items run as atrium items | 0 |

Per-item figures are lower is better. A run is only comparable when the window is a similar length and the item count
is counted the same way, so say how you counted.
