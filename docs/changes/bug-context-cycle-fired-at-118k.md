# bug-context-cycle-fired-at-118k: finding and proposed fix

## Finding (not yet proven)

The cycle fired on the sg4-control orchestrator at 118080 tokens against a 400000 status line. The code gives a limit
of 200k for that card unless one of four inputs was smaller than assumed. The cycle start event
(`EventNotified`, `by=context-cycle`, with `tokens` and `limit`) is the evidence, and I could not read it: the card's
event list from the hub returns only the most recent events, with no payloads, and the 13:29 EDT start has rolled off.
The card's worktree is `D:/git/.../atrium` on a Windows room, so its settings files are not readable from here either.

What the code rules in and out:

- The token count (118080) matches the status line, so the transcript read is not the cause.
- The hub's `context_limits` and the card's `context_limit_k` override can each only produce a limit at or below
  118k if someone set one that low. Both show on the card details as `own_k` and `source`.
- The runner ceiling is `(autoCompactWindow/1000 - 33) * 10/11`. A ceiling under 118k needs a window under about
  165k, from `~/.claude/settings.json`, or from the worktree's `.claude/settings.json` or `.claude/settings.local.json`,
  which `claudeconf.SettingsPaths` reads AFTER the global one so they win. The brief's global value of 400000 is
  not enough to rule this out: a project file in that worktree beats it.

Most likely the project settings file, or a card or hub override. Ask the orchestrator's owner to run the card details
for `own_k`, `source`, `ceiling_k`, `wanted_k` and `wanted_from` (the board already shows them in `ContextSize`).

## Proposed fix

1. Make the next occurrence self-explaining: the start event and the log line carry `limit_from`, `own_k`, `ceiling_k`
   and `wanted_k`/`wanted_from` from `api.ContextLimitOf`, not just `limit`.
2. Default limit and the model window: leave the built-in `claude` limit at 200k. The status line's 400k is the
   user's `autoCompactWindow`, not the model's 1M window, and a limit that follows the model would make a 1M card
   wait until 900k+, which the design deliberately avoids. No change proposed unless told otherwise.

## Test plan

## @LETTER@. Context cycle start records why

### @LETTER@1. Start event names the layers

1. Start a context cycle on a card with a card override and a runner ceiling.
2. Read the card's `context-cycle` start event.
3. It carries `limit_from`, `own_k`, `ceiling_k`, and `wanted_k` when the ceiling cut the limit.
