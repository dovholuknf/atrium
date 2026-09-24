# A typed peer message merges with what the human is typing

The case: `posture-check-in-native-76500` answered the orchestrator (`main:atrium`) with `atrium_say`. Atrium typed
it in. clint's half-written "it could be that was th" was submitted in the same user turn, on its own line after
the message.

## What the timeline says

From the room store, `main:atrium`, 2026-09-24 local:

- 08:18:53 the card goes `running`. Tool calls follow every 10 to 30 s.
- 08:20:16.8 the peer message is typed, `via: terminal`. The card is still `running`.
- 08:20:17.4 the next tool call. The permission hook would have carried the message 0.6 s later.
- 08:20:32.5 the turn ends (`needs-input`).
- 08:20:34.4 a new turn starts. That turn's prompt is the message plus clint's partial line.

## Cause

- The gate did its job. At 08:20:16 clint's line was empty and he had been off the keyboard for 2 s or more.
  `injectPeer` typed the message and pressed Enter, which is by design (`messages.go`, `handleMessage`).
- The gate checks the line only at the instant of the write. It does not know that the runner was mid-turn.
- Claude Code does not submit a prompt that arrives mid-turn. It holds it as a queued message until the turn ends.
- clint then typed into the input box. At 08:20:32 the turn ended and Claude Code sent the queued message and his
  draft as one prompt, joined by a newline. This is inferred from the order of events and the screenshot. Atrium
  wrote no bytes after the Enter.
- So the race is not a byte-level interleave. `pasteMu` already prevents that. The race is a gap of up to one turn
  between "typed" and "submitted", and inside that gap the runner owns the merge.
- The message was typed, not queued, because the card allows peer typing, atrium supervises its terminal, and the
  line was clean. Nothing checks whether the runner is busy.

## Options

- **A. Queue, do not type, while the card is `running` and its hooks have been seen.** The permission hook
  delivers it at the next tool call, or the Stop hook at the turn end. The pending injector types it on screen
  only once the card is idle.
  - Cost: small. One check beside each `dialogOpen` refusal: `tellByTyping`, the two `typeThroughGate`
    callers in `messages.go`, and `pendingInjector.attempt`. A shared helper keeps the four from drifting.
  - Hazard: a mid-turn message arrives as hook context rather than as visible terminal text. The timeline still
    records it. A card with no hooks seen keeps today's behavior, so codex and bare shells are unchanged.
  - Closes this exact case: the message would have landed at 08:20:17.4 through the hook.
- **B. Hold typed delivery for N seconds after any keystroke.** This is today's gate with a longer window.
  - Cost: a constant change.
  - Hazard: it does not fix the case. The human started typing after the write, so no window before the write
    helps. A long window also slows agent-to-agent traffic, which is the main case for typing.
- **C. Deliver by the queue whenever a human is attached.**
  - Cost: small. `howBusy` already knows `peerWatching`.
  - Hazard: "attached" is not "typing". A board tab that stays open turns off typed delivery all day. clint
    reads his sessions on screen, and that is why `pendingInjector` exists.
- **D. Lock the human's input until the runner takes the prompt, then replay the keystrokes.**
  - Cost: large. Atrium must detect when Claude Code consumes a queued prompt, and it has no signal for that.
    `UserPromptSubmit` fires only at the next turn, which can be minutes away.
  - Hazard: the keyboard goes dead for a whole turn with no explanation. A replay into a changed prompt can land
    in a dialog.
- **E. Type the message but do not press Enter while the card is `running`.**
  - Hazard: the human's draft and the peer text share one input box, which is the same merge in plain view.
    `injectPeer` removed "left unsent in the prompt" on purpose.

## Recommendation

- Option A. It uses the path that already exists for the busy case: the permission and Stop hooks drain the
  queue, and `takeMessages` clears the injector's held copy. It also sends the message sooner than typing does,
  because a tool call comes before the turn ends.
- Scope it to peer text first (`from != ""`). The operator's own channel (notes, actions, board messages) hits
  the same race, but a note typed mid-turn is what a human at the keyboard would do anyway. Decide that case
  separately.
- Test: a card with `ToolHookSeenAt` set and `Status` `running` makes `deliverPeer` return queued, and the
  injector does not type until the card goes `needs-input`.
- Not done on this branch. It changes the delivery policy that `peers.go` documents at length, so it needs a
  decision first.

## Side notes

- The comment on `tellByTyping` and `peerRoom` still describes "typed and NOT submitted" for `peerWatching`.
  `injectPeer` no longer does that. The comment is stale.
- The typed event carries no `how` field, so it came through `handleMessage` (`messages.go`, the
  `typeThroughGate` call), not `tellByTyping`. Both paths end in `injectPeer`, so the cause is the same.
