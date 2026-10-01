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

## Re-read of 60636d2e + 375421e0 at 375421e0 (@runtime, lows 1 and 2, with the orchestrator's amended rule)

`git diff 60636d2e~1 375421e0`, read. In a detached worktree at 375421e0: `go vet` on daemon, api and store was clean.
`go test -count=1 -run 'Auto|Ceiling|NewContext|Handoff|Capture' ./internal/daemon/` was ok (41s), as were the api and
store tests. @runtime reports that `TestIdleParkWorkersFirst` flakes on claude/main itself (7 of 30), which is
unrelated.

**Low 2 is closed.** A ceiling run on a card with no readable directory is refused at the top of `runNewContext`,
before anything is typed or the launcher is told. `handoffWritten` also refuses one, as a backstop. The chip says
why.

**Low 1 is closed as the amended rule states it, with one gap.** `ceilingHeld` holds without limit while a key
landed in the last 2 minutes. It holds while a terminal is attached or the /m card page fetched replies in the last
2 minutes, for up to 30 minutes past the crossing. It is checked at the start (`watchAutoContext`, `autoReady`),
before the launcher is told (`autoPrepare` through `autoStillOK`), and before each nudge (`ncNudge`).

- **The replies stamp.** Only the /m card page fetches `/replies` (`card.js` on open and refresh, and on load
  older). No hub tool and no board view does. A phone holds a director only while that director's own card page is
  open and refetching, and a hidden tab stops refetching, so the stamp ages out in 2 minutes. That is acceptable.
- **The crossing reset.** `crossedAt` resets only when the card is under its line, not when a run starts, so a run
  dropped for a watcher keeps the wait it has served. A daemon restart starts the wait over, since it is in memory.
  That is fine.

### Low

3. **The capture prompt and `/clear` are not held by recent typing.** Typing is checked at the start, in
   `autoPrepare` and before each nudge. The capture prompt (`ncCapture`, then `ncType`) and `/clear` are typed
   through `typeLabelledGuarded`, which waits only for the operator gate's short quiet (`peerQuiet`, seconds). A
   person who submits a message, reads the answer for more than `peerQuiet`, and is still inside the 2-minute
   typing hold can have the capture typed in front of them, then their conversation cleared once the capture turn
   ends. Adding `!(ceilingRun && run.typedWithin(ceilingTypedQuiet))` to `ncType`'s `quiet()` for ceiling runs
   would make "typing holds without limit" true through to the clear. The step then waits, or fails at `captureEnd`
   with nothing typed, which is the safe failure.

### Nit

4. `autoContexts.read` is never pruned. `forgetExcept` drops `by` but not `read`, so it keeps one entry per card id
   for the daemon's life. Prune it in the same loop.

Quality: after the Sonnet switch. The rule is implemented where it is stated, the reset reasoning is right, and the
stamp was justified by the fact that /m holds no connection. The gap is the same shape as before: the typed steps
past the start were not given the hold.

ROOM DEPLOY OK 60636d2e~1..375421e0. Low 3 should follow before directors are tagged broadly.

## Re-read of 6bcaf1bb + cc767d49 at cc767d49 (@runtime, low 3 and nit 4)

`git diff 2d220614 cc767d49`, read. In a detached worktree at cc767d49, `go test -count=1 -run
'Auto|Ceiling|NewContext|Handoff|Capture' ./internal/daemon/` failed once, then passed three times out of three
with `-v`. The failing test was not named in the tail I kept, and its last log line was "store is closed". That is
the teardown shape of the flakes @runtime reports on claude/main. api and store passed. @runtime reports the full
daemon package green in one run.

- **Low 3 is closed.** `ncType`'s `quiet()` refuses a ceiling run while `run.typedWithin(ceilingTypedQuiet)`. It is
  evaluated at the moment of writing, so it covers the capture prompt, `/clear` and the wake. Plain runs are
  unchanged. TestCeilingCycleTypingStepHoldsForARecentTypist covers a typist one minute ago, past `peerQuiet` and
  inside the two minutes.
- **Nit 4 is closed.** `forgetExcept` prunes `read` with `by`.

Nit: `/clear` is typed with `ncTiming.typeWait`, which is two minutes, the same as the typing hold. A person still
typing as the capture turn ends makes the cycle fail after a good capture, rather than wait, so the card keeps its
context and the next crossing tries again. That fails safe.

Quality: after the Sonnet switch. Exactly the fix, with a test on the boundary. No drop seen.

ROOM DEPLOY OK 6bcaf1bb~1..cc767d49
