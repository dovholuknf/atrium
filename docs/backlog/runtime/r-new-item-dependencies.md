# r-new-item-dependencies. Work items that wait on other work, with a gate only the board resolves

Status: designed 2026-09-30 in `docs/runtime/item-dependencies-design.md` (@rnd), ready to build. clint wants it
BUILT TODAY (2026-09-30). @runtime builds stage 1 (hub only, one worker day), @ui draws it (design section 5). From `docs/rnd/competitor-features.md` item 8 (Orca and Gas Town have it).

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
