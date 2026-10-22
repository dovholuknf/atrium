## Test plan

## @LETTER@. Clear context by tool

### @LETTER@1. A session clears itself

1. In a claude card that atrium owns the terminal of, call atrium_clear_context with a handoff.

**Expected:** the call answers at once with the handoff path. Nothing is typed mid-turn. When the turn ends atrium types
/clear, then a prompt to re-read BRIEF.md and the handoff at its absolute path. No limit prompt is typed.

### @LETTER@2. Refusals

1. Call it with an empty handoff, from a card whose runner is not claude, from a card with no atrium terminal, and
   while a new-context cycle is already under way.

**Expected:** each is refused with the reason and nothing is written or typed.
