# Automatic new context at a context threshold (r-029)

Status: designed, awaiting @rnd review. Nothing here is built. Owned by @runtime. clint approved the idea: when a
card's context passes a threshold, atrium runs the new-context cycle on it, rather than waiting for a person or the
launcher to press the button.

## What exists

Read from the code on claude/main (hub-main 4815d47), not from memory.

- `internal/daemon/newcontext.go`. `StartNewContext` claims the card (`nctx.begin`), and `runNewContext` runs four
  steps: capture (`ncCapture`), `/clear`, wait for the new SessionStart, wake. Every step types through `ncType`,
  which waits for the card to be between turns (`midTurn`, `cardRunning`, `dialogOpen`, `sinceBusy`) and for the
  runner's typed-line gate (`injectPeerIf`, `peerGateOpen`: an empty line, quiet for `peerGateIdle`). While a run is
  going, `holdingMessages` holds every message for the card until the wake is typed. A failed step leaves a `failed`
  chip with its reason and types nothing further, and the chip goes only when a SessionStart names a different
  conversation, the action is run again, or it is dismissed. The card's handoff file is
  `HANDOFF.<alias or 13 char id>.md` (`HandoffName`, item 91).
- `internal/daemon/contextsize.go`. `watchContext` runs on the reaper tick over cards in running, needs-input or
  needs-permission. It reads the size with `d.ctx.read(t)`, which reads the transcript of `sessionOf(t)`, the session
  the runner last STARTED, and falls back to the stored resume id only before any SessionStart is heard. That is the
  figure to use, because a `/clear` leaves the resume id on the old transcript until the next Stop (item 62). The
  threshold is the setting `context_threshold_k` (default 150, range 10 to 2000, `api.ContextThreshold`). The only
  action at the threshold today is one launcher notice (`NoticeContext`, sa87), claimed in the store per session and
  re-armed when the card is seen back under the line. A card with no launcher is only marked on the board.
- Statusline telemetry (`telemetry.go`, `docs/runtime/statusline-telemetry.md`) carries `context_used`,
  `context_window` and `model` per session, is never stored, and is believed for 30 minutes. It is the only source of
  the window size.
- Idle parking (`idlepark.go`, `idletick.go`, r-007). Subject cards are agent cards, cards tagged
  `atrium:park-idle`, or the orchestrator. It calls `ncCapture` alone (no clear) after 50 minutes idle, with its own
  `nctx.begin` claim, then parks after `idle_park_after`.
- Keep-alive (`keepalive.go`) refreshes an idle card's cache by forking its session. It skips parked cards and needs
  needs-input.
- Cull (`cull.go`, `mergedcull.go`) exits `atrium:subagent` workers whose branch merged.

## Design in one paragraph

A new step, `watchAutoContext`, sits inside `watchContext` so it reuses the size that loop already read. A card that
is a SUBJECT (opted in), is over its auto threshold, is ARMED, and passes the QUIET gates is handed to the existing
`StartNewContext` machinery with an `auto` origin. The cycle itself is unchanged except for the capture guard
(section 3), which fixes a real failure and applies to the manual cycle and idle parking too. Failure is bounded: two
automatic attempts, then it stops, tells someone, and waits for a human. It is off until clint turns it on.

## 1. Which cards

Default: OFF for every card. Nothing changes until the setting is turned on.

Two controls, mirroring idle parking (`ParkIdleTag`) and keep-alive:

- Setting `auto_new_context` with values `off` (default), `tagged` and `agents`.
  - `tagged`: only cards wearing the tag `atrium:auto-new-context`.
  - `agents`: every card with `origin:agent` (directors and workers) plus tagged cards.
  - A card's own tag `atrium:no-auto-new-context` always wins and excludes it.
- Human cards (no `origin:agent`) are subject ONLY when tagged, under either value.

Why this shape. clint's standing answers on keep-alive were opt-in, and only for cards a human uses. Keep-alive is
cost only, so being wrong costs cents. This action discards a live conversation, so it is stricter than keep-alive on
one point and looser on another:

- Stricter: a card a person is working in is never cycled unless someone tagged that card. A person who is mid
  thought in a card does not lose it because of a number.
- Wider in scope: directors are the cards that benefit. They are resident for days, hold the largest contexts and
  already resume from a handoff after a park, so `agents` covers them.

Workers (`atrium:subagent`) are subject under `agents`, but the design recommends running at `tagged` for them. A
worker is short lived, has a launcher that already gets the sa87 notice, and a cycle in the middle of a brief loses
more than it saves. Flagged as question 1 for clint.

Never subject, whatever the settings: a fixture, a throwaway, a card with a lent session (`guests`), a non-Claude
runner (`isClaude`, since `/clear` and the transcript reader are Claude's), and a card whose terminal atrium does not
own (`errNoTerminal`). These are the exclusions `idleParkSubject` makes plus the two the cycle itself needs.

The orchestrator card is subject like any other tagged card. It needs no extra rule. `orchestratorMayPark` exists to
stop it being parked with work running, and a cycle keeps the card alive.

## 2. The threshold and what is measured

Measured: `d.ctx.read(t)` for the card, that is the last main reply's input plus cache writes plus cache reads from the
transcript of `d.ctx.sessionOf(t)`. Never the stored resume id, and never the statusline figure alone. The statusline
is used for one thing, the window size.

Threshold, a new setting `auto_new_context_k` in thousands of tokens:

- Default 300, range 50 to 2000, checked like `checkContextThresholdK`. It MUST be at least `context_threshold_k`, so
  the launcher is always told before atrium acts. The settings check refuses a lower number and says why.
- When a fresh statusline figure gives a window (`Telemetry.Window`, not older than `telemetryStaleAfter`), the
  effective threshold is `min(auto_new_context_k, 70 percent of window)`. A 200k window is cycled at 140k and a 1M
  window at 300k. When there is no window the setting alone applies. This is per model window without a per runner
  table: the harness reports the window it really has, and 70 percent leaves room for the capture turn, which costs
  context and must not be the turn that compacts.
- A per card override is out of scope. The tag pair is the per card switch.

The figure can be one turn behind, because a transcript reply is read after the turn ends. That is acceptable. The
cycle only fires between turns, so the number it fired on is the number the capture starts from.

## 3. The capture guard

### What went wrong on @ui

`ncCapture` records `started := time.Now()` before typing the capture prompt. After the capture turn ends,
`handoffWritten` stats the file and refuses it when `ModTime` is before `started - 2s`, saying "was not updated by the
capture turn, so it is from an earlier session". @ui had written its handoff on its own about a minute before capture
started. The capture prompt arrived, the card saw its file was already current, replied that it was written and did
not touch it, so the mtime was 60 seconds old and the cycle failed on a handoff that was in fact fresh. The check
conflates "written since capture began" with "written this session and not stale". The same check runs for the idle
park's capture, so a director that wrote a handoff shortly before its idle handoff would fail the same way.

The check is needed. Without it, a `HANDOFF.<name>.md` left by an earlier cycle satisfies the capture, the clear runs,
and the wake reads the previous cycle's notes. That is worse than a failed cycle.

### The guard: prove freshness by the card, fall back to the timeline

Accept a handoff when ANY of these holds, checked in this order.

1. The nonce. The capture prompt carries a per run token, `atrium-capture: <13 char run id>`, and tells the card to
   make it the first line of the file. The file passes when its first 512 bytes contain the token. No clock is
   involved, so a coarse file system, clock skew, or a card that wrote early and then touched nothing cannot fail it,
   and an old file cannot pass it, since the token is new every run. A card that already wrote a good handoff only has
   to add one line, and the prompt says so: "If you have already written it and nothing has changed since, just add
   the line and stop."
2. The early write. Some cards will still not carry the token (a model that ignores it, a prompt from an older
   daemon). The file passes when `mtime >= turnBegan`, where `turnBegan` is the start of the LAST turn that had
   completed when capture began, and that turn ended no more than `captureEarlyWindow` (10 minutes) before capture
   began. In words: it was written inside the most recent piece of work and nothing has happened since. A stale file
   from an earlier cycle predates the current session's start, which predates that turn, so it cannot pass. This is
   @ui's case: written in the last completed turn, a minute before capture.
3. Otherwise the existing rule stands: `mtime >= started - 2s`, meaning the file was written during the capture turn.

Always required as well, whichever rule passed: the file exists, is a regular file, and is at least 200 bytes. An
empty file the card created to satisfy the check is not a handoff.

Data needed. Rule 2 needs the start and end of the card's last completed turn. `activity` already tracks
`turnsBegun`, `midTurn` and `sinceBusy`. It needs two remembered timestamps per card, `lastTurnBegan` and
`lastTurnEnded`, in memory like the rest of activity. If @rnd would rather not widen activity, rule 2 degrades to
`mtime >= currentSessionStartedAt` (the `wakes.sessionStarted` time) and within `captureEarlyWindow`, which is weaker,
because work between the write and the capture goes unseen. Question 1 for @rnd.

The wake side is unchanged. `handoffExists` still only checks that the card's own file is there.

Where it lives. `handoffWritten` gains the run id and the capture start facts and is the single place the rules are
applied. `ncCapture` is shared by the manual cycle, the idle park and the automatic cycle, so all three get the fix.
It is a bug on the shipped path, so it should land first and on its own, before the rest of this design. Question 2
for @rnd.

The failure message names which rule failed and the times, so the next report of this is diagnosable: "HANDOFF.ui.md
in <dir> has no capture token, was last written 14:02:11, before the last turn began at 14:03:40".

## 4. When it fires

The trigger is the reaper tick, so the decision is made at most once per tick per card. A card is started only when
EVERY line below holds. All are read at the tick, and `ncType` re-checks the ones that can change before each
keystroke.

State gates (checked in `watchAutoContext`):

- Subject (section 1), a Claude card, atrium owns its terminal, and it is not a fixture, throwaway or guest.
- Status is exactly `needs-input`. The tick lists running, needs-input and needs-permission, and only needs-input
  passes. That rules out mid-turn (running) and a permission dialog (needs-permission) in one test. Parked, shelved,
  done, dead and archived cards are not in the list at all, and `isParked` is checked on the fetched card as well.
- No pending permission for the card (`PendingForTask`), the same check keep-alive makes.
- Idle for `auto_new_context_idle_s` (default 45 seconds, `d.act.sinceBusy`). It reuses the r-022 `cardRunning` and
  `onSubagents` checks, so a card whose subagents are running, or whose turn ended on background work, is not idle.
- No message or wake queued for the card (`pending`), so what someone just sent is delivered first and the cycle waits
  for a later tick. Nothing is lost: the message runs its turn, and the card is checked again afterwards.
- `nctx.holding` is false, no idle handoff is capturing (`d.idle.get(id).capturing`), and `sameDirBusy` is empty.
  "Never twice at once" is the `nctx.begin` claim, which is atomic and already refuses a second run, so a race between
  the tick and a button press is safe by construction. A failed chip is not a claim, so the arm state below is what
  stops a loop on a failed card.
- Not inside a restart grace: for `autoStartGrace` (5 minutes) after the daemon starts, nothing fires. A restarted
  daemon meets every card cold, with activity blank and telemetry unheard, and should not act on a guess.

Line gate (checked at the moment of typing):

- The typed-line gate, through `ncType` and `injectPeerIf`, exactly as now. A person typing in the card closes it.
- `ncType` today waits up to `captureEnd` (15 minutes) for the gate. The automatic cycle uses a shorter
  `autoTypeWait` (2 minutes) for the FIRST typing, the capture prompt. If the gate never opens, nothing has been typed
  and nothing is lost: the attempt is dropped without a failure chip, counted as a deferral, and tried again on a
  later tick. A person typing in the card is by definition using it, so the cycle should stand aside for them and not
  queue behind them. Once the capture prompt HAS been typed, the run is committed and waits as the manual cycle does.

Hysteresis, so it does not loop. Each card has an in memory arm state.

- ARMED: may fire. The start state.
- FIRED: set when the cycle begins. Cleared, back to ARMED, only when BOTH hold: the card's read context is below half
  of its effective threshold, and the card is in a different conversation from the one the cycle began in (the same
  proof the failed chip uses, `sessionStarted`). A card that comes back after the clear at 20k is armed again and
  fires again when it grows to the line. A card that comes back still large (a handoff that reloads to over half the
  line, or a `/clear` that did nothing) stays FIRED and does not cycle again, and a notice says so: "auto new
  context ran and the card is still at 210k, its handoff is probably too large".
- A minimum gap of `autoMinGap` (30 minutes) between two automatic cycles on one card, in any case, as a backstop.
- The arm state is NOT stored. It describes a process running now, the same argument as `newcontext.go` and activity.
  After a daemon restart a still large card meets the restart grace and is judged again. That can give one extra cycle
  across a restart, which is what a person pressing the button would get anyway.

## 5. When the card cannot write a handoff, or capture times out

Nothing is cleared without a verified handoff. That rule is the cycle's and stays.

What each failure does:

- Before the capture prompt was typed (gate never opened, terminal gone). The card is untouched. Silent drop, retry
  on a later tick, no chip.
- Capture prompt did nothing, or the capture turn did not end in 15 minutes. Context intact. Failed chip with the
  reason, one retry after `autoRetryAfter` (30 minutes), then give up.
- Capture turn ended but the guard refused the file. Context intact. Same as above. The retry is worth it, since the
  second prompt names the missing token again.
- `/clear` typed but no new session. State unknown. Failed chip and NO automatic retry, since only a SessionStart
  proves the state (existing rule). A person decides.
- Cleared, wake not typed. The context is gone. Failed chip, and the wake alone is retried once, because it is the
  only step that can put the card back. If that fails too, notify and stop.

"Give up" means the arm state becomes GAVE_UP, the failed chip stays, and the reason stays in the card's history as an
event. GAVE_UP clears when a person presses new context (which replaces the failed chip), dismisses the chip, or a new
conversation starts. The card is then judged again from ARMED. That is two automatic attempts per crossing at most.

What is said:

- Chip: the existing `failed` chip, with `auto: true` so the board prefixes it, "auto new context failed", then the
  reason, ending "not retrying, press New context to try again".
- A launcher notice (`NoticeAutoContext`, claimed in the store per session so a restart does not repeat it) for agent
  cards: "<name> is at 312k and atrium could not cycle its context: <reason>. Not retrying."
- Human cards get the board chip and no notice.

The handoff a failed capture DID produce is left in place. It is useful, and the next attempt overwrites it.

A card that is over the line and never quiet (a director always mid turn) is not a failure. It never satisfies the
gates, and nothing is recorded, as with idle parking. The launcher notice at 150k is already the signal for it.

## 6. How it is recorded and how a person sees it

Events. No new event kind, because the event table's `kind` is a CHECK list and this needs no migration. It uses the
two kinds the cycle already writes:

- `notified` with `{"by": "auto-new-context", "started": true, "tokens": 312000, "threshold": 300000, "file":
  "HANDOFF.x.md"}` when a run begins. This is the record that atrium, not a person, did it.
- `notified` with `{"by": "auto-new-context", "failed": "<step>: <reason>", "attempt": 1}` on failure, the same shape
  the manual failure already uses (`by: new-context, failed`), so one reader handles both.
- `notified` with `{"by": "auto-new-context", "done": true, "before": 312000, "after": 18000}` once the wake is typed
  and the next context read comes back. `after` is filled by `watchContext`, not by the cycle, and written at most
  once per run.
- The existing `prompted` events, `from: "new-context"`, for the capture, `/clear` and wake. Unchanged. Their text is
  already labelled `atriumLabel("new context:")`, so the card and any reader can see they are atrium's.

Attribution. `by: auto-new-context` is atrium. It is never attributed to a person or the launcher. The manual button
keeps its own attribution, so the two are told apart in one query.

How a person sees it:

- The card's existing three step chip, with a prefix: "auto new context, capturing state to HANDOFF.x.md".
  `newContextView` gains `auto: true`, `tokens` and `threshold`.
- The context readout on a subject card says "auto new context at 300k" in its tooltip.
- The card's history has the start and result rows above. A board toast for human cards, since the card just lost
  its context under them.
- A launcher gets `NoticeAutoContext` when a run begins: "<name> reached 312k and atrium is cycling its context
  (handoff HANDOFF.x.md). It will wake and read it, no action needed." For a director's workers this is how the
  director learns a worker went quiet for a couple of minutes on purpose.

The sa87 notice at `context_threshold_k` keeps working. On a subject card its wording changes to "atrium will cycle its
context at 300k", so a launcher does not act on the earlier notice and race the automatic one.

## 7. Interactions

Messages held for the card. Reused as is: from `begin` to the wake, `holdingMessages` holds the card's peer injector
and every delivery path, and a sender is told `newContextHoldNote`. The one addition is the pre-check: a card that
already has a queued message or wake does not begin, so the automatic cycle never adds hold time to a message already
on its way. When the run ends any way at all, `releaseHeld` kicks delivery. A held message is delivered AFTER the wake,
to a card that has read its handoff, which is the right order.

Permission chain. Needs-permission cards and any card with a pending permission never start. If a permission dialog
opens after the capture prompt (the capture turn asks to run something), `ncType`'s `dialogOpen` check holds the next
step and the capture times out with the existing message. An automatic run never answers a dialog and never widens a
permission. The capture prompt asks for a file write in the card's own cwd, which its rules already allow for a
handoff.

Idle parking. The cycle's prompts are `prompted` events and would reset the idle clock. Rules so they do not fight:

- The automatic cycle never starts on a card whose idle handoff is capturing, and `startIdleHandoff` already backs off
  when `nctx.begin` fails, so whichever starts first wins and the other waits a tick.
- The cycle's own prompts (`from: new-context`) are excluded from the idle clock by the same filter idle parking uses
  for its own capture. A cycle is atrium's work, not the card's, so a card that was idle for an hour and crosses the
  line at the same moment is cycled and then parked on the old clock. Recommended, and question 3 for clint asks if
  he would rather a cycle counted as activity.
- A parked card is not in the tick's list, so it is never cycled. On unpark its context is read again like any card's.

Keep-alive. It forks the card's session, which does not touch the card, so a refresh in flight is harmless. After the
clear the cached prefix is gone by design, so the ledger for the old session is finished. `decide` gains one skip,
"new context running" while `nctx.holding`, and it follows the card to the new session when `sessionOf` moves. Its own
economics point the same way: a 300k context is expensive to keep warm, so cycling it is the cheaper end.

Auto-cull. `mergedcull` and `atrium_cull` exit a worker. A cycle in progress must not be culled under it, so cull adds
one refusal, "a new context is running on it, try again", using `nctx.holding`. The sweep is periodic, so the next
pass gets it. If a cull's exit wins the race anyway, `ncWait` sees `d.sup.get(taskID) == nil` ("the terminal closed"),
the run fails, and there is nothing to clear. The card is gone regardless.

A director's held reports. If a director is cycled, the reports it was waiting on queue behind the hold and arrive
after the wake. A test below covers that none is lost.

## 8. Schema changes

None. The design uses settings (the key value table `SetSetting` writes), tags, in memory arm state, events of
existing kinds, and the existing store notice claims (`ClaimNotice`, `ForgetNotices`). No migration is added, so
nothing is appended to the slice in `internal/store/schema.go`.

Considered and set aside: a `task.auto_new_context` column. It would make the per card switch a real field, but the
tag pair already does that for idle parking, and a column costs a migration for a flag. If @rnd prefers it, it is a new
migration named `0076_task_auto_new_context` at the END of the slice: `ALTER TABLE task ADD COLUMN auto_new_context
TEXT NOT NULL DEFAULT ''`, with values empty, `on` and `off`.

New setting keys, added to the store's settings, the API's settings view and the export list (`export.go`):

- `auto_new_context` (`off`, `tagged`, `agents`, default `off`).
- `auto_new_context_k` (default 300, 50 to 2000, not below `context_threshold_k`).
- `auto_new_context_idle_s` (default 45, 10 to 3600).

Constants, not settings: `autoStartGrace` 5m, `autoMinGap` 30m, `autoRetryAfter` 30m, `autoTypeWait` 2m,
`captureEarlyWindow` 10m and the 200 byte floor. They are variables, so a test runs them in milliseconds, as
`ncTiming` is.

## 9. Test plan

All in `internal/daemon`, table driven where it fits, using the existing `ncTiming` override, the fake runner and the
fake transcript (`contextSizes.transcript`) helpers the cycle's tests use.

The capture guard (`newcontext_guard_test.go`):

- `TestHandoffWrittenAcceptsNonce`: a file written two hours ago whose first line carries this run's token passes.
- `TestHandoffWrittenRefusesOldToken`: a file with a token from a previous run and an old mtime fails.
- `TestHandoffWrittenAcceptsWriteInLastTurn`: mtime 60 seconds before capture began, inside the last completed turn,
  which ended within the window. This is @ui's case and passes.
- `TestHandoffWrittenRefusesFileFromEarlierSession`: mtime before the current session's start, or before the last
  completed turn's start, fails and the message names the times.
- `TestHandoffWrittenRefusesWriteBeforeLaterWork`: written, then a later turn began and ended, then capture. Fails on
  rule 2 because work happened after, and needs the token.
- `TestHandoffWrittenRefusesEmptyAndTiny`: 0 and 199 bytes fail under every rule.
- `TestHandoffWrittenSlackOnCoarseFilesystem`: rule 3 keeps its 2 second slack.
- `TestIdleParkCaptureUsesGuard`: the idle handoff accepts a fresh early write as well.
- `TestCapturePromptCarriesToken`: the prompt names the token, and two runs get different tokens.

Trigger and gates (`autocontext_test.go`):

- `TestAutoContextOffByDefault`: a huge card with the setting unset starts nothing.
- `TestAutoContextSubjectMatrix`: table of (off, tagged, agents) by (human card, tagged human, agent worker, director,
  `no-auto` tag, fixture, throwaway, guest, non-Claude runner). Each row asserts fire or not.
- `TestAutoContextThresholdFromSessionNotResumeID`: after a `/clear`, with the resume id on the old huge transcript
  and `sessionOf` on the new small one, it does not fire. Item 62 again, on this path.
- `TestAutoContextThresholdUsesWindow`: a fresh telemetry window of 200k fires at 140k, a stale one falls back to the
  setting, and no telemetry falls back too.
- `TestAutoContextRefusesThresholdBelowNotice`: the settings check refuses a value under `context_threshold_k`.
- `TestAutoContextNeverMidTurn`, `...NeedsPermission`, `...DialogOpen`, `...SubagentsRunning`, `...MessageQueued`:
  each holds the start, and it starts on the tick after the condition clears.
- `TestAutoContextNeverParkedShelvedDone`: statuses outside the tick's list, and a parked card. None start.
- `TestAutoContextTypedLineGate`: a card with unsent characters on its line is not typed into, the attempt is dropped
  after `autoTypeWait` with no chip and no event, and it retries on a later tick.
- `TestAutoContextNeverTwice`: two ticks and a manual press racing produce exactly one `begin` and one set of prompts.
- `TestAutoContextRestartGrace`: nothing fires in the first `autoStartGrace`.

Hysteresis (`autocontext_hysteresis_test.go`):

- `TestAutoContextDoesNotLoopWhenStillLarge`: after a cycle the card reads above half the line, stays FIRED, does not
  start again, and a notice says the handoff is probably too large.
- `TestAutoContextRearmsAfterShrinkAndNewSession`: below half and a new conversation re-arms it, and it fires again at
  the line.
- `TestAutoContextMinGap`: two crossings inside `autoMinGap` fire once.

Failure (`autocontext_failure_test.go`):

- `TestAutoContextCaptureTimeoutRetriesOnceThenGivesUp`: the first failure leaves the chip and one retry after
  `autoRetryAfter`, the second gives up, GAVE_UP holds across ticks, and a manual press or a dismiss clears it.
- `TestAutoContextNeverClearsWithoutHandoff`: with the guard failing, no `/clear` is ever typed.
- `TestAutoContextClearUnprovenNotRetried`: `/clear` typed, no SessionStart, no automatic retry.
- `TestAutoContextWakeFailureRetriesWakeOnly`: cleared and the wake fails, only the wake is retried, and once.
- `TestAutoContextFailureNotifiesLauncherOnce`: one `NoticeAutoContext` for an agent card, none for a human card, and
  none again after a restart (the claim is stored).

Recording and interactions (`autocontext_record_test.go`):

- `TestAutoContextEventsAttribution`: `notified` with `by: auto-new-context` for start and result, `prompted` with
  `from: new-context`, never a person's name.
- `TestAutoContextChipMarkedAuto`: `newContextView` carries `auto`, `tokens` and `threshold`.
- `TestAutoContextHoldsMessagesThenDeliversAfterWake`: a say sent during the run is held, told `queued`, and typed
  after the wake. A director's worker report is not lost.
- `TestAutoContextIdleParkDoesNotFight`: with both due, exactly one begins, the other waits, and the idle clock is not
  moved by the cycle's prompts.
- `TestAutoContextKeepaliveSkipsWhileHolding`.
- `TestAutoContextCullRefusedWhileHolding`, and `TestAutoContextCullExitFailsRunCleanly` for the exit that wins the
  race.
- `TestSa87NoticeWordingOnSubjectCard`: the earlier notice says atrium will cycle at N.

## Open Questions

For clint:

1. Scope of `agents`. Should workers (`atrium:subagent`) be subject at all, or only directors and tagged cards? The
   design recommends directors and tagged cards, with workers left to the launcher's own notice.
2. The default. Off until you turn it on, with `tagged` as the first step. Agree, or should directors be on by
   default, the way agent cards are for idle parking?
3. Should a cycle count as activity for the idle clock? The design says no, it is atrium's work, so a card idle for an
   hour that is cycled is still parked on the old clock.
4. The number. 300k or 70 percent of the window, whichever is lower. Is that where you want it, given the notice is at
   150k and the cache cost of a big context is the reason for the whole item?
5. A tagged card you use yourself that goes quiet for 45 seconds. Is 45 seconds of quiet the right bar for "nobody is
   here", or should a tagged human card need far longer silence, or a per card switch on the board and not a tag?

For @rnd:

1. The guard's rule 2 needs `lastTurnBegan` and `lastTurnEnded` in `activity`. Accept that, or take the weaker session
   start fallback? Recommend accepting: it is the rule that decides whether a fresh early handoff is trusted.
2. Should section 3 land as its own item before any of the rest, since it is a live bug on the manual and idle park
   paths? The design says yes.
3. Is the nonce an acceptable change to the capture prompt, given the prompt is one line and the model gets all of it?
   The alternative is no token and rule 2 alone.
4. Retry policy: two automatic attempts, a 30 minute gap, and no retry of an unproven `/clear`. Right, or should the
   wake retry be a manual action only?
5. No migration and a tag pair for the per card switch, against a `task.auto_new_context` column. Which do you want?
6. The exact times from @ui's failed cycle (handoff mtime, capture start, the chip text) were not asked for. The
   account in section 3 comes from reading `handoffWritten` and the report of a handoff a minute early. If the real gap
   was over `captureEarlyWindow`, or the file was touched after a later turn, rule 2 would not have saved it and rule 1
   would. Worth confirming before the window is fixed at 10 minutes.
7. Whether the launcher notice for a running automatic cycle belongs to the sa87 notice family or its own claim key.
   The design uses its own, `NoticeAutoContext`.
