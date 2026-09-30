# r-new-item-dependencies. Work items that wait on other work, with a gate only the board resolves

Status: stage 1 BUILT 2026-09-30 by @runtime on `claude/r-deps`, to `docs/runtime/item-dependencies-design.md`.
Hub side only: live at the next hub deploy, no room restart. The board half is @ui's (design section 5, which now
carries the API shapes). From `docs/rnd/competitor-features.md` item 8 (Orca and Gas Town have it).

Code: `internal/itemgate` (ids, targets, the landed path, loops), `internal/hubstore/deps.go` (migration
`0004_item_gate`), `internal/link/deps.go` (the checks, `/_hub/deps*`, the launch refusal, the ticker) and
`internal/link/deps_mcp.go` (`atrium_deps`). Tests beside each.

Differs from the design in two small ways:

- The reads (`GET /_hub/deps`, `GET /_hub/deps/ready`) are open like `GET /_hub/launch-caps`, so a board reached
  over an overlay can show the Blocked list. Every write is loopback only.
- `atrium_launch` has no alias field, so the item comes from the title alone (`<id>:` prefix, or a title that is only
  an id).

## Why

The orchestrator tracks prerequisites by hand in its handoff, and they get lost across context resets. From one
morning: the terminal batch waits on u-033 being live, migration 0077 had to sit after 0076, f-006's sg3 and m1mini
migration waits on those rooms coming back, r-036b is not ready. Directors have also started items whose work was
already on claude/main.

## Wanted

- An item (and the card working it) can say "waits on X", where X is another item, a card, or a named condition
  ("u-033 live on the hub", "sg3 attached").
- A waiting item stays out of ready and says what it waits on, on the card and in the list.
- **Only the board resolves the gate.** An agent cannot mark a dependency met. It clears when X is done or landed, or
  when a human clears it. A named condition is cleared by a human, or by a check atrium can run.
- A director asking for its next item never gets one whose prerequisites are open.
- The orchestrator can list everything blocked, and on what, with one call.

## Pieces that exist

- Ledger states in `internal/store/ledger.go`, no dependency edge. Schema change at the END of the slice.
- `internal/daemon/sweep.go` for when a gate is re-checked.

## Open for the design

- Items versus cards: which carries the edge, since a card outlives its process and an item outlives its card.
- Cycles: refused at write time.
- Cross-room: an item on sg4 waiting on a card on sg3.
