# r-turn-cost-thresholds: a turn cause over its daily limit shows on the Terminals tab

r-turn-cost-by-cause reports what each kind of atrium text cost. This adds the judgment, in a separate place, and
leaves the usage tab's turns section as it was: it still reports and decides nothing.

## What changed

- Setting `turn_cause_limits` (internal/store/turnlimits.go), daemon-wide. JSON like
  `{"turns":100,"tokens":5000000,"kinds":{"say":{"turns":40}}}`. `turns` and `tokens` apply to every kind, `kinds`
  overrides one kind and keeps the general value for a field it leaves out, 0 is no limit on that axis, empty is the
  default and `off` is no limit at all. A value that does not parse is refused with a 400. It is read and written
  through GET and POST /v1/settings like the others, with `turn_cause_limit_default` alongside. There is no box for it
  on the settings page yet.
- GET /v1/usage/turn-limits (internal/api/turnlimits.go) sums today's UTC day from the delivery rows and answers one
  finding per kind past a limit: kind, turns, tokens, the limits, which was passed and the backlog item that would
  remove it. The operator's own typing is never counted, and tokens are counted as the usage tab counts them, cache
  reads apart. A small static map, `turnCauseItems`, names the item. Its first entry is the `notice` kind
  (atrium's own words with no kind of their own, which is where the expected-end notices land) to
  r-expected-end-notices. A kind with no item says how to raise the limit.
- The Terminals tab (js/turn-limits.js, `#term-limits`) draws one line per finding and a dismiss button. A dismissal
  is kept in localStorage for the UTC day it was made. Every value is written with textContent.
- A mid-turn usage flush (a report, an exit, a runner going) used to cost the delivery with only the replies it had
  read, and the Stop that ended the turn read only what came after. It now costs what it read and remembers the
  delivery, and the next read adds the rest of the turn to that same delivery, reply class included
  (internal/daemon/turncost.go). A flush with no Stop after it, a launched worker with no Stop hook, stays costed as
  before. A delivery that arrives after the flush starts its own turn and takes the later replies itself.

## Why these defaults

From GET /v1/usage/turns on the live room, over its three days of delivery rows, the busiest cause other than the
operator ran 6 turns and 180k tokens in a day, and the operator itself ran 21 turns and 1.8M tokens. 100 turns or 5M
tokens a day per kind is more than ten times the busiest atrium cause and still catches a loop (a nudge or a notice
every few minutes all day is a few hundred turns). Three days is thin, so the number is a floor to be tightened once
the endpoint has a longer history, and it is a setting for that reason.

## Test plan

## @LETTER@. A turn cause over its limit shows on the Terminals tab

### @LETTER@1. Over the limit

1. POST /v1/settings with `{"turn_cause_limits":"{\"turns\":1,\"tokens\":0}"}`.
2. Cause two turns from one kind of atrium text, for example two `atrium_say` to a card, and let each turn end.
3. Open the Terminals tab.

**Expected:** one line for that kind says its turns and tokens today and the limit of 1 turn, and says to set
turn_cause_limits, or names the item when one is known.

### @LETTER@2. Dismiss, and the next day

1. Click dismiss on the line and reload the board.

**Expected:** the line stays gone for the rest of the UTC day and returns the next day if the kind is still over.

### @LETTER@3. Off, and under the limit

1. POST `{"turn_cause_limits":"off"}`, or an empty string for the default.

**Expected:** with off, or with the default and a normal day, the Terminals tab shows no line.

### @LETTER@4. A flush mid-turn costs the whole turn

1. Have a launched worker report while it is still working, then let it finish the turn.

**Expected:** on the usage tab's turns section that turn is one turn on one delivery with the tokens of the whole
turn, not the part read at the report.

Before and after: docs/changes/img/r-turn-cost-thresholds-before.png (under the limit) and
docs/changes/img/r-turn-cost-thresholds-after.png (three findings).