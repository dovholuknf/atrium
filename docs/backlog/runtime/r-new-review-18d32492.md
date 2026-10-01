# Review of r-director-ceiling f7c2539b at 18d32492 (@runtime: `atrium:context-ceiling`, cycled mid-turn)

Reviewed by @review, 2026-10-01, from `git show f7c2539b` and the branch tip 18d32492 (claude/main merged). Room side.
@runtime asked for the hardest read on three things: no work-protecting gate loosened beyond mid-turn, running and
idle quiet; no clear before the capture proves the handoff; and the settings floor.

## What holds

- **Only three gates move, and only for a ceiling card.** In `autoReady`, a card wearing the tag may be `running`
  as well as `needs-input`, may be mid-turn or `cardRunning`, and skips the idle quiet while busy. Every other gate
  still refuses it, unchanged: no terminal, parked, a pending permission, a dialog, subagents, background work,
  held messages, the same-directory and minimum-gap checks after it, and `no-auto-new-context`, which is checked
  before the tag.
- **Nothing is typed into a turn, and nothing is cleared before the handoff is proved.** Starting a run on a busy
  card does not interrupt it. `ncType` waits for `!midTurn && !cardRunning` and a settled quiet before typing the
  capture. While it waits it types only `newContextStop`, at 1 minute and at half the step's limit, through the
  operator's gate. `/clear` follows only a successful `ncCapture`: the capture turn started, ended and settled, then
  `handoffWritten` found this card's file at 200 bytes or more, written during the capture or carrying this run's
  token.
- **Retries are bounded.** There are at most two attempts per crossing and 30 minutes between cycles, then the
  give-up notice. A director that will not end its turn is asked at most four times per crossing.
- **The threshold and the floor.** `autoThreshold` takes the ceiling when only the tag reaches the card, and the
  lower of the ceiling and the global line when both do, then 70 percent of the window. The ceiling is checked
  against `context_threshold_k` when typed (`checkContextCeilingK`, including a threshold typed in the same request),
  and clamped up to it when read (`EffectiveContextCeilingK`), so raising the threshold later cannot leave the
  ceiling below it. The bounds are 50 to 2000, and it is exported with the other settings.
- The wake says the context went at the ceiling, and the launcher's notice says "passed its context ceiling".

## Tests

In a detached worktree at 18d32492, with `ATRIUM_LOCATION` and `ATRIUM_DEBUG_INPUTLAG` cleared:

- `go vet ./internal/daemon/ ./internal/api/ ./internal/store/`: clean
- `go test -count=1 -run 'Auto|Ceiling|NewContext|Handoff|Capture' ./internal/daemon/`: ok (41s)
- `go test -count=1 -run 'Ceiling|Context|Settings' ./internal/api/ ./internal/store/`: ok

Nothing was run against a live room, by @runtime or by me.

## Findings

### Low

1. **A ceiling card skips the "someone is watching" gate.** `autoHuman` now returns false for any card with the
   tag, so a ceiling card takes the agent branch, which has no `run.watching()` and no human quiet. On a director
   clint is attached to and typing into, a cycle can begin. Its nudge ("a new context is waiting. Finish the step
   you are on, commit, and end your turn.") lands in the conversation he is having, and the conversation is then
   cleared. The operator's keyboard gate stops a collision mid-keystroke, but not the cycle itself. Keeping
   `run.watching()` as a hold for a ceiling card (wait while a human is attached and typed recently) would leave the
   unattended case, the one the feature is for, untouched.
2. **Pre-existing, but now reached more often: no directory, no proof.** `handoffWritten` returns nil when
   `handoffDir` is "" (no worktree, or one this daemon cannot stat), and the clear then rests on the turn ending
   alone. Ceiling cycles run on busy directors, the cards with the most to lose. Refusing to clear a ceiling card
   whose file cannot be checked would close this for the new path without changing the old one.

Quality: after the Sonnet switch. The gate change is minimal and exactly as described. The design reasoning (the
idle quiet "is a fact about a card between turns") is right, and the floor is enforced on both write and read. The
watching gate is the second-order case that was missed.

ROOM DEPLOY OK 18d32492. The lows are worth closing before many directors wear the tag.
