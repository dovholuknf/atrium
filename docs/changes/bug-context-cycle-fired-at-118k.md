# bug-context-cycle-fired-at-118k: finding and proposed fix

## Finding

The cycle fired at 118080 tokens because the card's own limit is 100k, not because anything was misread. Live data
from the orchestrator card (`sg4-control~01a0f2da`), via its owner: `overrides.context_limit_k="100"`, so
`context_size` has `source: card`, `own_k: 100`, `threshold_k: 100`, `ceiling_k: 333`. The worktree has no
`autoCompactWindow` and the user settings say 400000, so the ceiling (333k) was not involved. The 400k on the status
line is the user's compaction window, not the limit. Nothing is a regression, and the settings-scope audit of
`claudeconf.SettingsPaths` is dropped. The board already said "warns and lands at 100k from card" in the peek, but
nothing flagged it, and the start event recorded only `limit`.

## Fix

1. The start event and its log line carry `limit_from`, `own_k`, `ceiling_k`, `wanted_k` and `wanted_from`.
2. Guard: a limit more than 25 percent under the built-in default (under 150k) puts `low`, "limit 100k from card",
   on the card's `context_size` from the first context read, and the peek shows it. The cycle is not blocked.
3. The built-in `claude` limit stays at 200k. It does not follow the model's window: the status line's 400k is
   `autoCompactWindow`, not the 1M window, and a limit tied to 1M would delay cycling far too long.

## Test plan

## @LETTER@. Context cycle start records why

### @LETTER@1. Start event names the layers

1. Start a context cycle on a card with a card override and a runner ceiling.
2. Read the card's `context-cycle` start event.
3. It carries `limit_from`, `own_k`, `ceiling_k`, and `wanted_k` when the ceiling cut the limit.

### @LETTER@2. A low limit shows before it fires

1. Give a card a context limit of 100k.
2. Open its peek once it has a context read.
3. A line reads "limit 100k from card". A limit of 200k shows no such line.
