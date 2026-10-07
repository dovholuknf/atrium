# r-new-lean-context-cycle: a cheaper, less stale new-context cycle

Status: parked (clint, 2026-09-30). Not assigned. Designed by @rnd: `docs/archive/lean-context-cycle-design.md`.

## Why

The orchestrator's 16:14 cycle on 2026-09-30 worked, but it cost more than it needed to:

- The handoff was 470 lines. The fresh session needed about 40. Each cycle prepends a section and keeps the old
  ones, so every wake reads the whole history again (about 12k tokens).
- Line 1 named a card id the session no longer had. Identity written into the file goes stale.
- The session lost tool calls rediscovering things the old context knew: a Bash hook that refuses `;`, the card JSON
  field names, the line deploy-batch writes when it is done.
- The card had written its handoff before it asked for the cycle. Atrium still typed the capture prompt, and the
  session spent one full-context turn adding the token line.

All four changes below are in `internal/daemon/newcontext.go`. The automatic threshold cycle (`autocontext.go`) and
idle parking share `ncCapture` and the prompts, so they change too, on purpose.

## A. Skip the capture turn when the handoff is already fresh

- `atrium new-context <who> --handoff-ready`, sent as `{"handoff_ready": true}` on the existing POST.
- The daemon accepts it only when the card's own file (`HandoffName`) exists, is at least `handoffFloor` bytes, and
  was modified after the start of the card's last turn. Then the cycle goes straight to `/clear`.
- Any check failing falls back to the full capture. The clear is never made over an unwritten handoff.
- Needs a timestamp for the last turn start. The activity tracker counts turns (`turnsBegun`) and keeps no time.
- Saves the most expensive turn of the cycle, at full context, and about a minute.

## B. Identity in the wake prompt, not in the file

`newContextWake` says who the card is: `Read HANDOFF.x.md and continue from it. You are card <id>, alias <alias>,
<handle>@<room>.` The daemon knows all of it, so it cannot be stale.

## C. The capture prompt asks for a rewrite, not an append

Add to `newContextCapture`: rewrite the file as the current state only, under about 150 lines, and move anything
that no longer applies to `HANDOFF.<name>.archive.md`, which the wake does not read. Also list every tool or hook
refusal hit, and the exact commands and endpoints that worked.

## D. Warn on a large handoff (optional)

`handoffWritten` notes on the chip when the file is over about 24 KB. A warning, never a refusal: refusing blocks
the clear, which costs more than a long file.

## Order

B and C first (prompt text only), then A. D whenever.
