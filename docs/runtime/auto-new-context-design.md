# Automatic new context at a context threshold (r-029)

Status: designed. @rnd accepted it with four changes (2026-09-29), all folded in below. Nothing here is built except
the capture guard, which is split out as r-030 and lands first. Owned by @runtime. clint approved the idea: when a
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
automatic attempts, then it stops, tells someone, and waits for a human. It is off by default, and switched on in steps (decided question 2).

## 1. Which cards

Default: OFF for every card (decided question 2). The first live step is `tagged`, with one director tagged, once r-030 has landed and a manual cycle has passed with the token.

Two controls, mirroring idle parking (`ParkIdleTag`) and keep-alive:

- Setting `auto_new_context` with values `off`, `tagged` and `agents`, default `off`.
  - `tagged`: only cards wearing the tag `atrium:auto-new-context`.
  - `agents`: every card with `origin:agent` that is NOT an `atrium:subagent` worker (so directors and the
    orchestrator), plus tagged cards of any kind.
  - A card's own tag `atrium:no-auto-new-context` always wins and excludes it.
- Human cards (no `origin:agent`) are subject ONLY when tagged, under either value.

Why this shape. clint's standing answers on keep-alive were opt-in, and only for cards a human uses. Keep-alive is
cost only, so being wrong costs cents. This action discards a live conversation, so it is stricter than keep-alive on
one point and looser on another:

- Stricter: a card a person is working in is never cycled unless someone tagged that card. A person who is mid
  thought in a card does not lose it because of a number.
- Wider in scope: directors are the cards that benefit. They are resident for days, hold the largest contexts and
  already resume from a handoff after a park, so `agents` covers them.

Workers (`atrium:subagent`) are subject only when tagged. A worker is short lived, has a launcher that already gets
the sa87 notice, and a cycle in the middle of a brief loses more than it saves. Settled as question 1.

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

- Default 300, range 50 to 2000, checked like `checkContextThresholdK`. The setting must be at least
  `context_threshold_k`, and the settings check refuses a lower number and says why. That check constrains the
  SETTING only. It is not what guarantees the launcher is told.
- When a fresh statusline figure gives a window (`Telemetry.Window`, not older than `telemetryStaleAfter`), the
  effective threshold is `min(auto_new_context_k, 70 percent of window)`. A 200k window is cycled at 140k and a 1M
  window at 300k. When there is no window the setting alone applies. This is per model window without a per runner
  table: the harness reports the window it really has, and 70 percent leaves room for the capture turn, which costs
  context and must not be the turn that compacts.
- The effective threshold can therefore sit BELOW the sa87 notice (140k against 150k on a 200k window). That is
  intended. For a subject card, the `NoticeAutoContext` sent when the cycle BEGINS is the telling (section 6), so the
  launcher always hears before the context is cleared, whichever number is lower. The effective threshold is not
  clamped to the notice.
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

### The guard: the card proves freshness, or the file was written during capture

Accept a handoff when EITHER of these holds, checked in this order.

1. The token. The capture prompt carries a per run token, `atrium-capture: <13 char run id>`, and tells the card to
   put that line in the file. The file passes when the exact line appears anywhere in its FIRST 4 KB. The read is
   bounded to 4 KB and never reads the whole file. A card that rewrites its file may not keep the line first, so the
   position is not fixed. No clock is involved, so a coarse file system, clock skew, or a card that wrote early and
   then touched nothing cannot fail it, and an old file cannot pass it, since the token is new every run. A card that
   already wrote a good handoff only has to add one line, and the prompt says so: "If you have already written it and
   nothing has changed since, just add the line and stop." This is how @ui's case passes.
2. Written during capture. The existing rule stands: `mtime >= started - 2s`, meaning the file was written during the
   capture turn itself.

Always required as well, whichever rule passed: the file exists, is a regular file, and is at least 200 bytes. A token
alone is not a handoff.

Rejected on review: accepting any write inside the last completed turn. It would pass a director that wrote its
handoff at the start of a 20 minute turn, kept working, then answered "already written" at capture. Those notes are
stale, and the `/clear` throws away the difference, which is the exact failure the guard exists to stop. A model that
ignores the token now fails loudly, and a person presses again, which is the right failure. It also means activity is
not widened: no turn timestamps are added.

The wake side is unchanged. `handoffExists` still only checks that the card's own file is there.

Where it lives. `handoffWritten` gains the run's token and is the single place the rules are applied. `ncCapture` is
shared by the manual cycle, the idle park and the automatic cycle, so all three get the fix. It is a bug on the
shipped path, so it lands first and on its own, as r-030, before the rest of this design.

Each refusal has its own named, one sentence message, since the chip shows it: the file is missing, the file has no
token and is older than the capture ("HANDOFF.ui.md in <dir> was last written 14:02:11, before capture began at
14:03:40, and has no line atrium-capture: <token>. Add that line."), or the file is under 200 bytes.

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
- A HUMAN card (no `origin:agent`, so subject only because it is tagged) needs more. It needs NO BROWSER ATTACHED to its
  terminal (the supervisor's attach viewers for the card, the same set the attach websocket registers into, read at
  the tick and again before the capture prompt is typed). It also needs a much longer quiet, `autoHumanQuiet` (10
  minutes). A person reading output for a minute is not typing, and 45 seconds would clear the context under them.
  Nobody watching and ten quiet minutes is the bar for "nobody is here". Recommended, and clint's question 5.
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

### The ceiling tag (r-director-ceiling)

A card tagged `atrium:context-ceiling` is held to `context_ceiling_k` (default 150, never below `context_threshold_k`)
whatever `auto_new_context` says, `off` included. It is a second trigger beside the global line, so a card with both
is cycled at the lower. `atrium:no-auto-new-context` still excludes it, and it gets the agent idle rule, not the human
one.

It is allowed to start MID-TURN, which no other card is. A director sits in turns that last hours, driving workers and
watchers, so a gate that waits for an idle prompt never opens on one, and a director paid $1.19 a relay at 416k
against $0.35 at 101k. The cycle already knows what to do with a running card: the capture step types the one
labelled line asking it to finish its step, commit and end its turn, at a minute and again at half the limit, and
types nothing else until the turn is over. So the only gates dropped are mid-turn and running (and the idle quiet,
which is about a card between turns). Pending permission, an open dialog, subagents, background work, held messages,
the same-directory check and the minimum gap all stay. The orchestrator tags directors. Workers never tag themselves.

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

For a subject card, the `NoticeAutoContext` at begin IS the telling. When the effective threshold falls below
`context_threshold_k` (a small window, section 2), the sa87 notice has not fired yet, and this one is the first and only
notice the launcher gets. It is sent before the capture prompt is typed, so the launcher always hears before anything
is cleared.

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
`autoHumanQuiet` 10m, the 4 KB token read and the 200 byte floor. They are variables, so a test runs them in milliseconds, as
`ncTiming` is.

## 9. Test plan

All in `internal/daemon`, table driven where it fits, using the existing `ncTiming` override, the fake runner and the
fake transcript (`contextSizes.transcript`) helpers the cycle's tests use.

The capture guard (built as r-030, `newcontext_guard_test.go`):

- `TestHandoffWrittenAcceptsToken`: a file written two hours ago that carries this run's token passes. This is @ui's
  case.
- `TestHandoffWrittenFindsTokenPastLineOne`: the token on line 40, inside the first 4 KB, passes.
- `TestHandoffWrittenIgnoresTokenPast4KB`: the token after the first 4 KB is not found, and an old file fails.
- `TestHandoffWrittenRefusesOldToken`: a file with a token from a previous run and an old mtime fails.
- `TestHandoffWrittenRefusesEarlyWriteWithoutToken`: mtime 60 seconds before capture began and no token fails, with
  the named message that says to add the line.
- `TestHandoffWrittenRefusesEmptyAndTiny`: 0 and 199 bytes fail under both rules, token or not.
- `TestHandoffWrittenSlackOnCoarseFilesystem`: rule 2 keeps its 2 second slack.
- `TestIdleParkCaptureUsesGuard`: the idle handoff accepts a tokened early write as well.
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
- `TestAutoContextBelowNoticeTellsLauncherFirst`: a 200k window fires at 140k, before the sa87 notice, and the
  launcher's `NoticeAutoContext` is sent before the capture prompt is typed.
- `TestAutoContextHumanCardNeedsNoViewerAndLongQuiet`: a tagged human card with a browser attached never starts. With
  none attached it waits for `autoHumanQuiet`, not 45 seconds. A viewer that attaches between the tick and the capture
  prompt stops the run with nothing typed.
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

clint's questions, DECIDED by @rnd for clint, 2026-09-30. clint was asleep, and his standing order was that
directors settle questions with @rnd. clint may overturn any of them, and each is one setting or one constant.

1. Scope of `agents`: directors and tagged cards only. Workers (`atrium:subagent`) are NOT subject under `agents`,
   only when tagged. A worker is short lived, its launcher already gets the sa87 notice, and a cycle in the middle of
   a brief loses more than it saves. Section 1's `agents` value means exactly this.
2. The default: `off`. The first step on the live board is `tagged`, with one director tagged, once the capture guard
   (r-030) has landed and a manual cycle has passed with the token. Turning on `agents` after that is the
   orchestrator's call, made on the live setting, not a code default.
3. A cycle does NOT count as activity for the idle clock. It is atrium's work, not the card's.
4. The number: the lower of 300k and 70 percent of the window, as designed. The begin notice (`NoticeAutoContext`) is
   the launcher being told (section 2), so 140k on a 200k window is fine.
5. A tagged human card is cycled only with no browser attached to its terminal AND 10 minutes of quiet
   (`autoHumanQuiet`). Agent cards keep the 45 second quiet.

Build order: the capture guard (r-030) as its own item first, then the rest. @rnd cleared building it without another
review of the design.

@rnd's questions, answered in review (2026-09-29) and folded in:

1. Turn timestamps in activity: not needed. The early write rule was dropped, so activity is not widened.
2. Land the guard first as its own item: yes. It is r-030 (the token, the written during capture rule, the 200 byte
   floor, the named failure messages).
3. The token in the capture prompt: fine.
4. Retry policy: agreed. The wake retry stays automatic, because typing a wake twice is harmless, and an unproven
   `/clear` is never retried.
5. The per card switch: the tag pair, no migration.
6. @ui's exact times: moot now that no timing rule decides freshness.
7. The launcher notice: its own claim key, `NoticeAutoContext`.
