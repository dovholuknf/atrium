# u-new-context-bar-on-rows. A context bar under each terminal row

Status: row colours done 2026-10-04 (see Design note), per-runner limit layer not built. Owned by @ui. Filed by the orchestrator 2026-10-01, from clint.

Screenshot of the rows it goes on: `D:\git\github\dovholuknf\atrium\.atrium\incoming\20261001-093851-pasted.png`
(the terminals list, untagged group).

## What clint asked

"I want the token usage and the limit expressed as a small line under each card, going from green to yellow to
red, and if it hits the limit I want some sort of visual indicator."

clint, after: "token usage is against the context, for example `ctx 157099/1000000 (15%) getting full`. Those values
need to be atrium settings, per runner and per agent in that runner, with board defaults so I don't have to update
a bajillion."

## The limit is a setting, in three layers

- Board default (exists today as the gear's context threshold, `context_threshold_k_now`).
- Per runner (harness row): new. Overrides the board default for every card that runner starts.
- Per card: exists as `context_size.threshold_k` and the `atrium:context-ceiling` tag. Overrides the runner.
- The context window (1M in the example) comes from the model, not a setting. The bar shows used against the
  limit, and the tooltip shows both: "157k used, limit 150k, window 1M".
- Settings UI: board default in settings, per runner on the runners page, per card in the card's settings, each
  saying which layer the value came from. This half is @runtime's (store + API) and @ui's (the three editors).

## Wanted

- A thin bar along the bottom edge of each terminals-list row: the card's context now against its limit. The limit
  is the threshold the details popover already uses (`peekThresholdK` in `js/peek.js`: the card's
  `context_size.threshold_k`, else the gear's setting, else 150k), so a card tagged `atrium:context-ceiling` shows
  against its ceiling.
- Colour by fraction of the limit: green, then yellow, then red. Recommend yellow from 60%, red from 85%.
- At or past the limit: a distinct mark that reads at a glance across the list. Recommend the bar full and
  pulsing red once (not looping: an endless animation on many rows costs CPU, as the restart cover showed today),
  plus a small "limit" chip on the row. A card being cycled by new-context shows that instead.
- Tooltip on the bar: "136k of 150k context".
- No new polling. Use what the card row already carries. If the row carries only the warn flag (see `peek.js`
  header: the list carries only the warn flag), the daemon has to put the context figure on the row first. Say so
  in the design, that half is @runtime's.
- Same bar on the board's cards and on /m rows, if it fits. Say which in the design.

## How it should look

clint, 14:26: the bar should look like the context meter in the details popover, just the line, no text.
Screenshot: `D:\git\github\dovholuknf\atrium\.atrium\incoming\20261001-142616-pasted.png` (169k CONTEXT, the bar,
"past the line", "warns at 150k").

- Reuse that meter's look: a thin track, the filled part, and a tick mark where the limit sits, with the fill
  running past the tick when the card is over.
- Drop the figure, the CONTEXT label, "past the line" and "warns at 150k" on the row. Those stay in the popover and
  the bar's tooltip.
- Share the drawing with the popover's meter rather than writing a second one, so the two cannot drift.

## Design note (2026-10-04)

Most of the bar had already landed (u-ctx-bar, then the land-the-plane work, see u-new-ctx-bar-findings.md): the thin
line on terminals rows and board cards, drawn by the popover's own `ctxMeter`, the tick at the limit, one pulse past
it, the tooltip "201k of 200k (land the plane), window 1M". The row already carries `context_size.tokens`, so the
daemon needed no change and nothing polls. What this pass added is the colour ramp by fraction of the limit, in
`ctxLine` (js/board.js):

- Under 60% of the limit: the theme's good colour (teal, as the popover meter has it). From 60%: yellow. From 85%: red.
  At or past the limit: red, full, one pulse (never a loop). A card cycling its context does not pulse.
- The limit is the land-the-plane line (`landThePlaneK`: the per-browser setting, never below the card's own
  threshold, so a card with `atrium:context-ceiling` shows against its ceiling when that is higher). The amber mark
  (`ctxwarn`) still follows the daemon's warn flag at the 150k line and is unchanged.
- No separate "limit" chip. The row's LAND badge was taken off on purpose after the review of ctx-badge (contrast on
  dark skins), and the full red pulsing line plus the amber mark say it. Say so if the chip is wanted back.
- Board cards use the same `ctxLine`. The stack list only has the amber mark. /m rows were not touched.

### Not built: the per-runner limit layer

Board default, then runner, then card. The runner layer is store, API and three editors, too big for this pass.
Plan: @runtime adds `context_limit_k` to the harness row (store and the runners API, validated like the card's
`threshold_k`) and resolves it in the daemon into `context_size.threshold_k` with a `source` field ("card", "runner",
"board"), so the row needs no new read and the popover and `peekThresholdK` already show it. @ui then adds the runner
field on the runners page, the card field in card settings, and the source label in each. The land-the-plane line is
a browser setting today, so moving it into the daemon store (`land_k` beside `warn`) belongs in the same step.
