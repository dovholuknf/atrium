# Review: r-notify-input-turn-end 0f274fc9

Range `d739825c..0f274fc9`, one commit. It touches `internal/daemon/activity.go`, `internal/link/notify.go`, a new
`internal/daemon/notification_reason_test.go`, `internal/link/notify_test.go`, `docs/test-plan.md` and the item.

It replaces the notify.go one-liner that @fabric pulled for regressing atrium-87300.

- **Room.** An `idle_prompt` Notification ends the wait with an empty reason, not `asked`.
- **Hub.** The "no turn has ended" drop now also needs `WaitingReason != "asked"`. So a card that asked before its
  first Stop raises `input`, and a fresh or idle card stays quiet.

Verdict: **OK** for room and hub. There are three Lows and a test-plan fix-up.

## How it was checked

- **Reading.** I read the diff, `turnEndedBecause`, `SetStatusBecause`, `NoteAsked`, every writer of
  `WaitingAsked` and `WaitingStarted`, the cli's `wantsAHuman` filter, and where `seen.turn_ended_at` comes from.
- **Gates.** On a scratch worktree at the tip:
  - `gofmt -l` is clean on the touched files. It lists only `daemon/fyi_test.go`, which is unchanged in the range.
  - `go vet` passes on daemon, link and store.
- **Tests.**
  - link, store and cli pass.
  - The full daemon run fails only on the known reds: the hostterm and unix-socket family (10 tests),
    `TestKeepaliveForkCarriesALeanCardsPromptToolsAndMCP` and `TestNoTestHereCanReachALiveRoom`.
- **Mutants.** All four were killed:
  - The `idle_prompt` branch is turned off, so it writes `asked` again. `TestAnIdlePromptDoesNotRecordAnAsk` fails.
  - The gate term is replaced by `true`. `asked before any turn` fails.
  - The term is inverted to `== "asked"`. `TestNotifyIdentityPerReasonAndPriority` fails.
  - `started` is let through as well. `started before any turn` fails.
- **Merge.** A trial merge onto landing a9307bc7 conflicts only in `docs/test-plan.md`, where both sides append after
  IN. Keep both, with this section after IP.

## Points

- **"A real earlier ask survives" holds, and on two guards.**
  - The ask is recorded while the card is running. `NoteAsked` (PreToolUse of the asking tool, activity.go:826)
    writes `asked` straight to the column. The `idle_prompt` that follows reaches `SetStatusBecause` with an empty
    reason, which falls back to the stored `asked`. `TestAnIdlePromptKeepsARealEarlierAsk` pins this.
  - On a card already in needs-input, a Notification does nothing at all. `turnEndedBecause` acts only from Running
    or Dead, and `SetStatusBecause` returns early when the status is unchanged. So an `idle_prompt` can never clear
    an ask that is already on the card.
- **No stale `asked` carries into a later wait.** Any move to running (prompt, or tool-start through `turnResumed`)
  clears the reason. help.go:178 writes `asked` only from blocked, and a blocked card has a `report`, so the hub
  gate never drops it anyway.
- **Where the gate can now fire.** `turn_ended_at` is written only by `NoteTurnEnded`, from the Stop path
  (messages.go:372). A launched or joined card starts as needs-input with reason `started` (launch.go:691,
  session.go:437). The new term therefore changes one case only: a card that went running and then raised a
  permission, elicitation or `agent_needs_input` Notification before any Stop. That is the f-029 case it was meant
  to fix. `started` and `""` are still dropped, so the 87300 card stays quiet.
- **Other Notification types.** `wantsAHuman` (cli/hook.go:120) passes only `idle_prompt`, `agent_needs_input`,
  `elicitation_dialog` and `permission_prompt`. All except `idle_prompt` still write `asked`. The `idle_prompt`
  check for cards with subagents out still runs first and is unchanged.
- **Mixed versions.**
  - **New hub, old room.** The old room still writes `asked` on an `idle_prompt`, but only from Running or Dead,
    meaning a card that worked and got no Stop. The new hub raises `input` for that card. It is not the 87300 card,
    which is needs-input/`started` and so unaffected by an `idle_prompt`. The worst case is one notice per idle
    no-Stop session until the room upgrades.
  - **Old hub, new room.** The old gate drops every needs-input card with no turn end, as it does today. Nothing
    regresses. The f-029 fix arrives with the hub.

## Lows

- **L1: f-029 is narrowed, not closed.** A session with no Stop hook that finishes without asking goes from Running
  to needs-input with reason `""` on its `idle_prompt`. Its `turn_ended_at` stays empty, so it never raises `input`.
  That is the price of keeping 87300 quiet, and probably the right one. Say so in the item, so the cr2 link 03
  question is answered as "asked: yes; idle with no Stop: no, by design".
- **L2: the daemon test's kinds.** `TestARealNotificationRecordsThatTheCardAsked` covers `permission_prompt`,
  `elicitation_dialog` and `""`, but not `agent_needs_input`, which is the fourth type the cli lets through. Every
  case also starts from Running, and none from Dead. Both go through the same branch, so this is cover, not a bug.
- **L3: the test-plan section.**
  - Header `@LETTER@` → **IQ**. Landing uses IO and IP (fabric's f-027 and f-028), IJ to IM are reserved, and IQ
    appears nowhere else.
  - Wrap the steps at 120 like IO and IP, and drop the extra blank line before the header.

Aside, not this change: on landing a9307bc7, the r-stdio-launch-lineage header still reads `@LETTER@`. It should be
IN.

Atrium-Verdict: room-ok d739825c..0f274fc9
Atrium-Verdict: hub-ok d739825c..0f274fc9
Quality: a small, well-aimed fix. It uses the reason the room already records rather than a new signal, and both
halves have tests that the mutants confirm.
