# The usage tab: cache reads out of the way, and what else it should tell clint

clint, 2026-09-29, through @orchestrator: hide cache reads on the usage tab, because they swamp every graph. Then:
what else should the tab do? **Constraint from clint: keep the current look.** He likes it. Nothing here changes it
dramatically. Anything larger is optional or added alongside (a new section, a toggle, a view), and never replaces
what is there.

The tab is `internal/api/web/js/usage-charts.js` over `GET /v1/usage` (`internal/store/usagebuckets.go`) and the
`usage` event. Today, top to bottom: a range bar (1h, 6h, 24h, 7d) with a room picker and a card filter chip, a
line per room that could not be read, a legend, **burn rate** (stacked bars per kind), **by card** (the top 12 as
small sparklines, the rest as others), **tokens by kind** (one split bar and a legend), and **tokens by cause** (a
table).

Nothing here is built. Where the store or the daemon changes, it is @runtime's, and the board is @ui's.

**Decided by clint, 2026-09-29: all of it.** Ranks 1 to 8 and the flameout projection, with "sex appeal" and the
current look kept (section 4.1). The flameout does NOT notify: it shows on the tab only. The room DOES keep a few
hours of limit readings, stored and never computed by a model, so it costs no tokens (section 3.3.2). The cache-reads
toggle stays off by default as proposed.

## 1. The counting rule, shared with the rooms dashboard

Over 24h on this board: cache read **1451M**, uncached in **24k**, out **6.2M**, cache write **26M**. Cache reads are
98% of the tokens and a tenth of the price or less, so every chart is one block of the cache-read colour.

**Counted tokens are in + out + cache write (5m and 1h). Cache reads are reported apart.** This is the rule u-010
(the rooms dashboard) takes for its rate and series in my review of it, and r-010's room-stats event carries it. The
two surfaces count the same way, so "41k/min" on a room tile and the peak on the burn chart are the same kind of
number. The rule lives in one place on the board, a `ucCounted(sums)` beside `ucTokens`, and both surfaces call it.

## 2. Cache reads: a toggle, off by default

Yes, a toggle, off by default. Off means:

- **burn rate** stacks four kinds (uncached in, out, cache write 5m, cache write 1h). The peak and the axis are
  counted tokens per minute.
- **by card** sorts the top 12 by counted tokens, and each sparkline and total is counted tokens.
- **tokens by kind** keeps its split bar over the four counted kinds. Beside it, in the legend's own style, one line:
  `cache reads 1451M · not in these charts · 98% of input was served from the cache`. So the number is never gone,
  it is just not drawn at a scale that flattens everything else.
- **tokens by cause** shows counted tokens, with cache reads as a dim column of their own.
- the hover readout on a burn bar still lists all five kinds, cache read included, because a readout is a place
  where a big number does no harm.

On means exactly today's tab. The toggle sits in the range bar, at the right, as one small button in the range
buttons' style: `cache reads` with its legend colour swatch, pressed or not. The card details' own 24h chart
(`paintCardUsageChart`) follows the same setting.

Where the setting lives: a daemon setting, `usage_cache_reads` (default off), like the skin and density, because the
board asks the daemon rather than remembering (`internal/api/web/CLAUDE.md`). Per board, not per tab.

## 3. Everything else, ranked

Each is a new section or a fix, and each is optional on its own. Nothing below changes a section that exists except
where it says "fix".

| Rank | What | Kind | Size | Owner |
|---|---|---|---|---|
| 1 | Cache reads toggle, off by default (section 2) | toggle | small | @ui |
| 2 | Fix: every cause row says "0 calls" | fix | small | @runtime, @ui |
| 3 | Fix: sg3 and sg4-wsl "too old to keep usage", and what the line says | fix | small, and a room update | @ui, @merge |
| 4 | **Limits**: the 5h and weekly windows | new section | medium | @ui |
| 5 | **By department and by director** | a "group by" on an existing section | medium | @runtime, @ui |
| 6 | **Tokens per outcome**: per accepted item | new section | medium | @runtime, @ui |
| 7 | Cache hit rate and keep-alive paying for itself | one line | small | @ui |
| 8 | Backfill a room's history from its transcripts | one command | medium | @runtime |

### 3.1 Fix: "0 calls" on every cause row (rank 2)

`usageCount` prints `N prompts · M calls` from `rows` and `replies`, but the bucket sums the tab reads carry no
`replies`: `UsageSums` in `internal/store/usagebuckets.go` has `rows` and the token kinds only, and `ucSums` in the
board mirrors it. So every cause row in the tab says "0 calls", while the card details, which read
`store/usage.go`'s per-card totals where `Replies` is summed, are right. The fix is `replies` in `UsageSums`, summed
in the bucket SQL, added in `add`, and in `ucSums`. The `usage` event already carries a row's fields, so live rows
count too. A test pins a bucket's replies against the rows it came from.

### 3.2 Fix: rooms "too old to keep usage" (rank 3)

The tab says it when `/v1/usage` answers 404, which means the room's build predates the endpoint (`39a8da1`,
2026-09-28). sg3 and sg4-wsl are on older builds. Two parts:

- **The line says what to do.** `sg3 build 2f715b5 predates usage (needs 39a8da1 or later). Update the room to see
  its usage here.` The build comes from `/_hub/rooms`, which the tab already has through the hub. When the rooms tab
  gains an update action (u-010's `not the hub's build` chip hints at it), the line links to it.
- **Updating the room is the fix, and it is @merge's deploy, not a tab change.** After the update, the room records
  every turn from then on. Turns before it were never recorded, which rank 8 addresses.

### 3.3 Limits: the 5h and weekly windows (rank 4)

A new section under **tokens by cause**, and a thin band on the burn chart.

```
limits  highest seen, last hour                                                         
 5h      ████████████████░░░░░░░░  64%   resets 21:40 (in 2h 36m)   from sa-orch · 3m ago
 week    ███████░░░░░░░░░░░░░░░░░  31%   resets Mon 09:00           from u-005 · 8m ago
```

- The numbers are the `five_hour` and `weekly` telemetry cards already carry (`internal/daemon/telemetry.go`), from
  Claude Code's statusline. The same wording rule as u-010: atrium does not know which account a card runs under,
  so each bar is **highest seen** in the last hour, labelled with the card that reported it and how long ago. With
  none in the last hour, the bar is a dash. Never summed.
- **On the burn chart**, when the range is 6h or longer: a faint vertical band from the current 5h window's start
  (`resets_at` minus 5 hours) to now, in `--teal-bg`, with the label `5h window` at its top. So "how much of this
  window has gone already, and on what" is one glance at the chart that exists. Off when no card reported a 5h reset.
- If two cards report different reset times for the 5h window, they are on different accounts. The section then
  shows one row per distinct reset time, each labelled with its cards, rather than pretending it is one account.

#### 3.3.1 Projected burn and flameout: when does each limit run out

clint asked for this by name, and nothing in atrium or dotfiles computes it. It sits on each limit row as a second
line, so the section answers "how much is left" and "when do I hit the wall" in one place.

```
limits  highest seen, last hour
 5h      ████████████████░░░░░░░░  64%   resets 21:40 (in 2h 36m)   from sa-orch · 3m ago
         at this pace: 100% at 20:55, 45m before the reset         ▲ flameout before reset
 week    ███████░░░░░░░░░░░░░░░░░  31%   resets Mon 09:00           from u-005 · 8m ago
         at this pace: 100% Sat 14:10, 1d 19h before the reset
```

**How it is worked out.** Atrium never sees the limit's size. It sees a percentage (the statusline's) and its own
counted tokens. So the projection pairs them:

1. **The pace of the percentage itself.** For each limit, keep the (time, pct) readings the cards reported. The
   board already holds each card's latest telemetry, and the readings over the last hour are enough. Fit the rate
   in percentage points per hour as a least-squares line over the readings in the current window only, meaning
   since the 5h window's start, or over the last 24h for the weekly. That rate needs no guess about the limit's size.
2. **Projected 100%:** `now + (100 - pct) / rate`. Shown as a time, and as how long before or after the reset.
   After the reset the percentage falls and the window starts over, so a flameout after the reset is shown as `not
   this window`, never as a date past it.
3. **When the fit has too little to go on** (fewer than 3 readings in the window, or less than 15 minutes between
   the first and the last), it falls back to scaling by counted tokens. From the readings take how many counted
   tokens one percentage point took (the counted tokens between the first and last reading, divided by the points
   gained), and divide by the counted burn over the last 30 minutes. The line says `(estimated from token burn)`,
   so the two methods are never confused.
4. **Nothing is projected when the pace is flat or falling,** or there is no reading in the last hour. The line
   says `no pace to project` rather than a date.

**The warning.** `▲ flameout before reset` in `--warn` when the 5h projection lands before the reset, and in
`--danger` when it lands within 30 minutes. It shows on the tab only and never notifies (clint, question 6).

**How honest this is, stated on the row's hint and here:**

- **The percentage is "highest seen", not the account's own figure.** Readings come from whichever cards report
  them, as often as their statuslines run. An idle board reports nothing and the projection stops, and that is shown.
  Two cards on the same account agree, so their readings pool. Cards on different accounts carry different reset
  times (section 3.3), and each gets its own row and its own projection, never a blend.
- **Atrium's counted tokens are not everything that counts against the limit.** Anything the account spends outside
  atrium (claude.ai in a browser, a session on another machine that is not a room) moves the percentage and not
  atrium's counted tokens. So method 1, the percentage's own pace, is the primary one: it includes spend atrium cannot
  see. Method 3 can only undercount, and it says so.
- **How the vendor weights tokens into the percentage is not public.** Output, cache writes and cache reads may
  count differently, and the counted-token rule (section 1) is atrium's choice, not the vendor's. Method 1 needs no
  weighting. Method 3 assumes the mix stays as it was over the readings, and the hint says that.
- **A linear pace is a straight line through a bursty curve.** One big turn moves it. So the projection is shown to
  the nearest 5 minutes, never the second, and when the fit's residuals are large it says `rough`.

The honest summary on the hint: *"When you would hit 100% if you keep spending as you have since the window
started, from the percentages your cards reported. It sees spend outside atrium, it does not see the future, and it
is only as fresh as the last card that reported."*

Rank: it goes with rank 4 (limits), in the same stage, since the readings it needs are the ones that section
already shows.

#### 3.3.2 The room keeps the readings (clint: yes, at no token cost)

So a reload, a second browser or the phone draws the same projection, the room stores the readings it is sent.
Nothing is asked of a model: a reading is a number the statusline already reports, written as it arrives.

- A table `limit_reading` (at the END of the migration slice, `CREATE TABLE IF NOT EXISTS`): `task_id`, `kind`
  (`five_hour` or `weekly`), `pct`, `resets_at`, `at`. A row is written in the telemetry path when a card's reported
  pct or reset differs from the last row for that card and kind, so an unchanged statusline writes nothing.
- Kept for 8 hours for `five_hour` and 7 days for `weekly`, pruned on the sweep timer (`sweep.go`), never on the
  telemetry path.
- `GET /v1/usage/limits?since=` returns the rows, for the board's first paint. New readings arrive on the existing
  telemetry updates, so there is no new event and no polling.
- The fit (section 3.3.1) runs on the board over these rows. The room computes nothing. @runtime for the table and
  endpoint, @ui for the fit.

### 3.4 By department and by director (rank 5)

A **group by** select at the right of the **by card** heading: `card` (today, the default), `department`, or
`director`.

- **department** groups cards by their `dept:<name>` tag, with cards that have none as `no department`.
- **director** groups each worker under the card that launched it, with the director's own spend in its group, and
  cards launched by clint as `clint`.

The same small-sparkline layout, one tile per group, sorted by counted tokens. Clicking a group filters the tab to it,
as clicking a card does now.

**The tags must be kept at the time of the turn.** A worker is culled and its card pruned, and its tags and launcher
go with it, so a rollup built from today's card list would file last week's spend under `no department`. So
`session_usage` gains `dept` and `launcher` columns, written from the card when the row is written (a migration at
the END of the slice, tolerant of already being there), and the bucket read groups by them when asked
(`GET /v1/usage?group=dept|launcher`). Rows from before the migration show as `before grouping`, never as a
department they may not have had. @runtime for the store and endpoint, @ui for the select.

### 3.5 Tokens per outcome (rank 6)

A new section at the bottom, **per accepted item**: what the work that landed cost.

```
per accepted item, last 7 days                         counted     cards   cache reads
 u-005  review tab design                                 2.1M        3        88M
 r-004  worktree-gone reaper fix                          4.8M        2       210M
 t-002  banner copies in a fresh scrollback               1.2M        1        40M
 median 2.1M per item · 11 items accepted
```

- An item is a work-ledger row (`internal/store/ledger.go`, `work_item`) that reached `accepted` in the range. Its
  cost is the counted tokens of every card linked to it in `work_log`, including their subagents and keep-alive.
- A card linked to two items is split evenly between them, and the row says so. An item with no linked card is
  left out and counted in a footer, `2 accepted items have no card on record`, so the median does not flatter.
- Grouped by department when the section above is grouped by department.

This depends on the ledger linking cards to items, which it does for items reported through `atrium_report`. How
complete that linking is, is open question 4.

### 3.6 Cache hit rate and keep-alive paying for itself (rank 7)

Two phrases on the cache-reads line from section 2, no new section: `98% of input served from the cache` (cache read
over cache read plus uncached in plus cache write), and, when the range holds keep-alive refreshes,
`keep-alive spent 3.2M counted and kept 41 cards warm`. The break-even figure the keep-alive already computes goes in
that line's hint.

### 3.7 Backfill from transcripts (rank 8)

A room updated today has no usage before today. The transcripts on disk do: every Claude Code JSONL line carries its
message's usage. `atrium usage backfill [--since 30d]` on a room reads the transcripts of its cards' known resume
ids, writes `session_usage` rows marked `cause: backfill` for turns not already recorded, and stops at
`UsageMaxBack`. A backfilled row has no cause beyond that, and the cause table shows `backfilled` as its own line. It
is a one-time command, never a timer. @runtime.

## 4. What the tab looks like with all of it

The sections that exist stay where they are and as they look. The toggle is one button, and the new parts are
below or beside:

```
 token burn                                                   1,284 turns
 [1h] [6h] [24h] [7d]   all rooms ▾                              [■ cache reads]
 sg3 build 2f715b5 predates usage (needs 39a8da1 or later). Update the room to see its usage here.

 ■ uncached in  ■ out  ■ cache write 5m  ■ cache write 1h
 burn rate  tokens per minute, stacked by kind          ┊ 5h window ┊
 ▁▁▂▃▅▂▁▁▂▅▇▆▃▂▁▁▁▂▃▅▆▅▃▂▁▂▃▅▆▇▇▅▃▂▁▁▂▃▃▂▁▁▂▃▅▆▅▃▂▂▃▅▆▇▆▅▃▂
 by card  top 12 by tokens, the rest as others. Click one to filter             group by: card ▾
 [tile] [tile] [tile] ...
 tokens by kind
 ██████████████████████████████████████████████████
 uncached in 24k · out 6.2M · cache write 5m 3.1M · cache write 1h 23M
 cache reads 1451M · not in these charts · 98% of input served from the cache
 tokens by cause
 you            312 prompts · 4,120 calls     21M      cache reads 1.1G
 keep-alive     88 refreshes                  2.1M     cache reads 190M
 limits  highest seen, last hour
 5h   ████████████░░░░░░  64%  resets 21:40
 per accepted item, last 7 days
 ...
```

### 4.1 Sex appeal, without a new look

clint asked for the whole of it "with sex appeal", and for the current look to stay. That means polish inside the
existing language (`internal/api/web/css/tokens.css` and the rules in `internal/api/web/CLAUDE.md`), never a new
style:

- **Every new section uses the tab's existing parts:** the `uch` heading with its `ucnote`, the `uclegend` swatches,
  the `ucsplit` bar, the `ucminis` tiles. A limit bar is a `ucsplit` with one filled segment. A group tile is a
  `ucmini`. Nothing new looks different from what is there.
- **Numbers move, charts do not jump.** A number that changes on a live `usage` event fades from its old value over
  300ms (the rooms dashboard's rule). A new burn bar grows up from the axis over 200ms. Both respect
  `prefers-reduced-motion`.
- **The flameout line earns its colour.** It is `--dim` text normally. It takes `--warn` only when the projection
  lands before the reset, and `--danger` inside 30 minutes, so colour on the tab always means something.
- **The 5h band** on the burn chart is `--teal-bg` at low alpha behind the bars, with a hairline edge at the window's
  start. It reads as "now" without competing with the data.
- **Hover is the detail.** Every new number has a `data-tip` with its source and definition (the honesty notes of
  section 3.3.1 live there), so the tab stays uncluttered and nothing is unexplained.
- **Skins:** colours come only from existing variables, so all the skins wear it. `scripts/check-skins.sh` has
  nothing new to check.

## 5. Staged plan

1. Ranks 1 to 3: the toggle, the calls fix, the too-old line. One @ui worker plus @runtime's `replies` fix, and a
   room update for sg3 and sg4-wsl through @merge. Small, and all of it is what clint asked for directly.
2. Rank 4, limits. Board only, from telemetry the cards already carry.
3. Ranks 5 and 7: the migration for `dept` and `launcher`, the grouped read, the select, the cache line.
4. Rank 6, per accepted item, once question 4 is answered.
5. Rank 8, backfill, only if clint wants history from before each room's update.

Each stage ships alone, and clint can stop after any of them.

## 6. Clint's answers, 2026-09-29

1. **Cache reads:** off by default, as proposed, and remembered per board as a daemon setting.
2. **Which extras:** all of them, ranks 1 to 8 and the flameout projection, in the staged order of section 5.
3. **Limits:** "highest seen in the last hour, labelled by card" stands, with the honesty notes on the hint.
4. **Tokens per outcome:** yes, with the footer counting accepted items that have no card on record.
5. **Backfill:** yes, as a one-time command.
6. **Flameout notification:** no. It shows on the tab only.
7. **The projection's memory:** yes. The room keeps the readings in the store (section 3.3.2), with no model and no
   tokens involved.
