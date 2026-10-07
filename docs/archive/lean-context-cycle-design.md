# Lean context cycle (r-new-lean-context-cycle)

Status: superseded by `docs/runtime/context-cycle-design.md`. Kept as the record of the lean cycle proposal.

Origin: designed by @rnd 2026-09-30, for @runtime. Nothing here is built. Backlog item
`docs/backlog/runtime/r-new-lean-context-cycle.md`.

## The answer

Build all four parts, in the item's order, with four changes:

1. **A (skip the capture turn)** keys its freshness on the turn the request arrived in, which the activity tracker
   already records (`turnAt`, read by `turnSince`). No new timestamp is needed. The request is honored only while the
   named card is mid-turn, and the first wait after it (for `/clear`) uses `captureEnd`, not `typeWait`, because the
   card that asked is still in the turn that asked.
2. **B (identity in the wake)** also names the launcher, since that was the other stale line in the 16:14 handoff.
3. **C (rewrite, not append)** is prompt text only, and the archive file is already ignored by `.gitignore`
   (`HANDOFF.*.md`).
4. **D (warn on a large handoff)** cannot live on the chip, because the chip is removed when the cycle succeeds. It
   goes on the card's timeline, and the next capture prompt for that card names the size.

Each part is independent. B and C are one small change and remove most of the cost. A saves one full-context turn
per self-requested cycle. D stops the file growing back.

## What is there today

- `internal/daemon/newcontext.go` runs the cycle: capture (`ncCapture`), `/clear`, wait for the new SessionStart,
  wake. The idle park (`idletick.go` `startIdleHandoff`) runs `ncCapture` alone. The automatic threshold cycle
  (`autocontext.go`) runs the whole sequence. All three share `newContextCapture`, and the operator's and the
  automatic cycle share `newContextWake`.
- `handoffWritten` passes a file that is at least `handoffFloor` (200) bytes and either was modified during the
  capture turn or carries this run's `atrium-capture: <token>` line in its first 4 KB.
- `atrium new-context <who>` (`internal/cli/cardverbs.go`) POSTs an empty body to `/v1/tasks/{id}/new-context` on the
  board port. The handler calls `StartNewContext`, which takes no options.
- The activity tracker keeps `turnAt`, the start of each card's current turn, and exposes it through `turnSince`
  while the card is mid-turn. The item says there is no such timestamp. There is. It was added for the long-turn
  escalation.
- The capture prompt says "write everything a fresh session needs". It says nothing about replacing what is already
  in the file, so a card that cycles every few hours appends a section each time.

## A. Skip the capture turn when the handoff is already written

### The request

`atrium new-context <who> --handoff-ready` sends `{"handoff_ready": true}`. `StartNewContext` gains an options
argument, and the board button keeps sending nothing. The automatic cycle and the idle park never set it: nobody
asked them for a cycle, so nobody can vouch for the file.

### The check, at request time

Accept `handoff_ready` only when ALL of these hold when the POST arrives:

1. The card is mid-turn, and `turnSince` gives the turn's start, `asked`. A card that is not mid-turn is not the
   caller, so this is somebody else vouching for it, and that is refused. A card between turns can simply run the
   normal capture, which is short when the file is already written.
2. The card's own file (`HandoffName`) exists in its directory, is a regular file, and is at least `handoffFloor`
   bytes.
3. Its modified time is at or after `asked` minus the same 2 s slack `handoffWritten` allows. It was written during
   the turn that is asking.

If any check fails, the cycle starts as a normal one and the answer says why:
`{"started": true, "handoff_ready": false, "why": "..."}`. It is never refused outright, since the caller wanted a
new context either way, and the full capture is always safe.

### The check, again before the clear

The capture step is replaced by a wait for the asking turn to end, with the same quiet rule `ncCapture` uses
(`midTurn`, `onSubagents`, `cardRunning`, `turnSettle`). This wait uses `captureEnd` (15 min) and nudges at
`nudgeAfter` exactly as the capture does. Using `typeWait` (2 min), which is what the `/clear` step has now, would
fail every card that does a few more steps after asking.

When the turn is over, the file checks (2 and 3) run again. The file must still be there, still big enough, and not
older than `asked`. A card that deleted or truncated its handoff after asking falls back to the full capture, which
starts from the normal capture step on the same run. It does not fail.

### What it does not protect against

Work done after the handoff was written and before the turn ended is not in the file. The flag means "my handoff is
final and I am about to stop". The CLI prints that back: `asked <who> for a new context, handoff ready: end your turn
now`. The capture prompt's "commit or stash" instruction does not reach a card that skipped it, so the documentation
for the flag says to commit first.

Until security stage 2 (`docs/rnd/security-design.md`) there is no way to know the caller is the card itself. The
mid-turn rule makes a stranger's use of the flag unlikely to do harm: the stranger can only skip the capture for a
card that is already working and wrote its file in this turn. When `ATRIUM_CARD_TOKEN` lands, `handoff_ready` is
honored only with the card's own token. That is a follow-up for the stage 2 build, not a blocker here.

### On the chip and in the history

Step 1 reads `handoff ready, waiting for the turn to end` in place of `capturing state to <file>`. The `prompted`
event the capture would have written is replaced by an event `{"by": "new-context", "handoff_ready": true, "file":
..., "bytes": ...}`, so the history shows why no capture prompt was typed.

## B. Identity in the wake prompt

`newContextWake(file)` becomes `newContextWake(file, who)`, where `who` is built from the card at wake time:

`Read HANDOFF.rnd.md and continue from it. You are card 01k6..., alias rnd, rnd@claude-sg4, launched by
orchestrator-sg4-control@sg4-control.`

- The id, alias and handle are the same values `atrium peers` prints. Read them from the store at wake time, not at
  begin, since the wake is where they have to be right.
- The launcher is included when the card has one, from `launcherOf` (`a2a.go`), named by its handle. It is the line
  a card most often needs and most often gets wrong from memory.
- A missing alias or launcher leaves its clause out. It never prints an empty value.
- The wake-only retry (`autocontext.go`) calls the same function and gets the same text.

The chip label stays `waking it to read <file>`.

## C. The capture prompt asks for a rewrite

Append to `newContextCapture`, still one line:

> Rewrite the file as the current state only, under about 150 lines, and move anything that no longer applies to
> HANDOFF.<name>.archive.md, which is not read back. Include every tool or hook refusal you hit and the command that
> worked instead, and the exact commands and endpoints you used.

- The archive name is derived from `HandoffName` (`HANDOFF.rnd.md` becomes `HANDOFF.rnd.archive.md`), so two cards in
  one directory keep separate archives. `.gitignore` already covers it.
- The line limit is advice. Nothing counts lines, and D is the only check.
- The token rule is unchanged. A rewritten file is new, so it passes on its modified time anyway.
- The idle park gets the same text. A parked card is resumed by reading the same file, so a shorter file helps it
  too.

## D. A large handoff is reported, never refused

`handoffWritten` returns the file's size along with its verdict. When a capture passes with a file over
`handoffLarge` (24 KB):

- An event on the card: `{"by": "new-context", "handoff_bytes": N, "large": true}`. The chip cannot carry it, since
  `finish` removes the chip when the wake is typed.
- The size is kept in memory per card, and the next capture prompt for that card adds: `Your last handoff was N KB.
  Bring it under 150 lines.` Lost on restart, which is fine: it is advice.

Never a refusal. Refusing leaves the context uncleared, which costs more than a long file.

This is not the automatic cycle's existing notice. `autocontext.go` tells the launcher "its handoff is probably too
large" when a card comes back from a cycle still over its token threshold. That notice measures tokens after the wake,
and it covers only the automatic cycle. D measures the file at capture, for every cycle and the idle park. Both stay.

## Stages

| stage | what | owner | size | acceptance test |
| --- | --- | --- | --- | --- |
| L1 | B and C: wake names the card, capture asks for a rewrite | @runtime | small | `newcontext_test`: the typed wake contains the id, alias, handle and launcher, with no empty clause for a card with no alias. The capture prompt names `HANDOFF.<name>.archive.md`. Existing token and floor tests still pass |
| L2 | A: `--handoff-ready` | @runtime | medium | four tests. (1) A mid-turn card with a file written this turn: no capture prompt typed, `/clear` typed after the turn ends, wake typed. (2) The file older than the turn: a capture prompt is typed and the answer says why. (3) The card not mid-turn: the capture is typed. (4) The file deleted after asking: a capture is typed on the same run, and the chip does not fail |
| L3 | D: size on the timeline and in the next capture | @runtime | small | a capture that passes on a 30 KB file records `large`, the next capture prompt names the size, and a 10 KB file records nothing |

L1 is prompt text and one signature. It can ship with the next room deploy. L2 and L3 touch `ncCapture`, which the
idle park and the automatic cycle share, so the idle and auto tests (`idletick_test`, `autocontext_*_test`) must pass
unchanged.

## Questions for later

None for clint. Defaults decided here: 150 lines, 24 KB, the launcher named in the wake, and a refused
`handoff_ready` falling back to a full capture rather than an error.
