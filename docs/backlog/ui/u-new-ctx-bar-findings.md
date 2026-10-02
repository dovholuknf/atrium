# u-new-ctx-bar-land-the-plane: findings and what was built

Companion to the item `u-new-ctx-bar-land-the-plane` (on claude/ui-director). Kept in its own file so the two do not collide on landing.

## Why the "limit" chip did not show on the orchestrator row

NOT CONFIRMED. The orchestrator card is not on the room this worker could read, so its real `context_size` JSON was not seen.
The orchestrator can send it (`curl localhost:7781/v1/tasks`, that card's `context_size`, `new_context`, `telemetry`) to confirm.

What the code and the live cards say:

- The old chip is `ctxWarnMark`: an icon only, no text, and it is drawn only when `context_size.warn` (tokens at or past the
  gear's `context_threshold_k`, default 150k, daemon-decided) and the card is not cycling (`t.new_context` hides it,
  js/terminal-list.js). Live cards at 221k to 523k all carried `warn:true, threshold_k:150`, so the data is there.
  So the likely causes are `new_context` set on that card, or a small icon nobody saw.
- The thin bar was scaled to `threshold_k * 1.5` (150k to 225k), so 201k drew at 201/225 = 89%: a nearly full bar that was still only a thin pink line.
  The card's real limit was 300k (`autocompact.limit_k` 300, runner window 330k), so the bar was on neither number.
- 200k exists only in the status line script (`~/.claude/statusline-command.sh`, hard-coded there). The board had no 200k.

## Where the land-the-plane figure is read from

A per-browser pref, `atrium.landThePlaneK` (js/peek.js, `landThePlaneK`), default 200, in one place (`LAND_K_DEFAULT`).
Not the status line (a shell script, unreadable from the board) and not the daemon's `context_threshold_k` (that is the
WARN line at 150k, a different line). The gear field sits beside `context_threshold_k` and says it is this browser's
setting and matches the status line's LAND THE PLANE. Same pattern as `atrium.growler` and `atrium.reposView`:
every localStorage access in try/catch, and a `storage` listener so another window follows. Junk reads as 200; the
line is never below the warn line (`threshold_k`). This is a second 200k in the board, and it is said here and in the gear hint.

Possible LATER runtime step, not for now: move it into the daemon store (`land_k` beside `warn` in `ContextSize`, an API
field `land_the_plane_k` like `context_threshold_k`) so every browser and the launcher notice agree. That is @runtime's
store/API and was ruled out while the pause holds.

## What was built (board only, no Go)

- Past the land line: solid red `LAND 201k` badge (`.chip.ctxland`, replaces the amber icon), red row border, row flood red.
  In the narrow terminals list the number is in the tooltip and the badge says LAND, so it cannot squeeze the name out;
  on a phone row it says `LAND 201k`.
- Between warn and land: amber flood and the existing amber icon. Under warn: a faint neutral flood, so the list is not a rainbow.
- The row is the gauge (direction B): `.peek-bar.ctxline` is a flood behind the row, full at the land line plus 10% (the
  runner's compaction window), tick at the line, a 5px floor along the bottom that carries the tooltip.
  One pulse as it crosses the line, no looping animation.
- Tooltip: `201k of 200k (land the plane), window 1M` (below the line: `170k of 200k, window 1M`).
- The popover meter is the same drawing (`ctxMeter`), 10px, ramp to red past the line, with the same tooltip.

Headless: sections ctxLine (rewritten), landThePlane (new), contextSize (expects the badge on a 212k card), bootClean.
