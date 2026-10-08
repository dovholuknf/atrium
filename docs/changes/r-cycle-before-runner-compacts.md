## Why a ceiling, and why refuse

On 10-07 the orchestrator card compacted twice near 220k. Its own override `context_limit_k: 900` was above claude's
compaction point (settings.json `autoCompactWindow: 253000`, less the 33k reserve), so atrium waited for 900k and
claude compacted first, losing the handoff. Nothing in code wrote the override and the board did not show it.

- `claudeconf.AutoCompact` reads `autoCompactWindow` and `autoCompactEnabled` from claude's settings files (home, then
  the card's project, narrowest wins). That is the one place atrium can see the runner's real compaction point, and it
  is what `daemon.runnerCompactAtK` uses for a claude runner. Unreadable or absent means no ceiling, never a guess.
- `api.RunnerCeilingK` is that point times 10/11, the inverse of the 10 percent backstop `autocompactK` already puts
  above the limit. 220k gives 200k.
- `api.ContextLimitOf` takes the smaller of the layer's limit and the ceiling. `ContextLimitFor`, the cycle trigger,
  the `--autocompact` value and the board all go through it, so they cannot disagree.
- A card's own limit above the ceiling is REFUSED at PATCH, not capped. A typed number then never silently differs
  from the one in force. The hub's limit and a settings.json edited after the card was set cannot be refused, so
  they are capped at read time and the row says `source: "runner"` with `wanted_k` and `wanted_from`.
- The hub's 200k limit and every live setting are untouched.

## Test plan

## @LETTER@. A card cannot be given a limit its runner would compact first

### @LETTER@1. Refused above the ceiling

1. On a machine whose `~/.claude/settings.json` has `autoCompactWindow: 253000`, open a claude card's details.
2. Type 900 in the context limit box.

**Expected:** the save is refused and the message names the 200k ceiling and the 220k compaction point.

### @LETTER@2. Capped and shown

1. Set the hub's claude limit to 900 in settings.
2. Hover the card.

**Expected:** the card cycles at 200k and the limit reads "200k, capped by its runner from 900k (hub)".

### @LETTER@3. Own limit shown and cleared

1. Give a card its own limit of 150.
2. Open its details, then press clear.

**Expected:** the box shows 150 with a clear button, and after clear the line reads "200k from hub".
