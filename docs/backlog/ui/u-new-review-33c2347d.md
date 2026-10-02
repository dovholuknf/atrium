# Review: u-switch-latency fix 1 (hover prewarm) 33c2347d

Range `0f35c7cb..33c2347d`, 4 commits, 6 files. There are no Go changes.

- Three commits hold the measurements and the scripts: `5865011f`, `af7cab1b` and `a8aae0b0`.
- `33c2347d` makes the change, in `js/terminal-list.js` and `js/settings-spine.js` (`attachTask`), with the
  `switchPrewarm` headless section.

Verdict: **OK** for hub and room. Two Lows can follow.

## How it was checked

- I read the JS diff, `api`/`apiFetch` in `core.js` (the 6-slot cap and the 15 s read timeout), and the room's
  `getTask`.
- I ran the prewarm functions in node, lifted from the file, with a fake clock and a fake `api` that I settle by hand.
- `HEADLESS_ONLY=switchPrewarm` passes at the tip, in a scratch worktree.
- `node --check` passes on the five JS files.

## Points

- **Staleness: 3 s, used once.** The age counts from when the fetch started, not when it answered, which is the safe
  end.
  - In node, a take at 3001 ms returns null, so the click fetches again.
  - `takePrewarmed` deletes the entry whether or not it was fresh, so a second click always fetches.
  - The card it hands over is at most 3 s older than the one a click would have fetched. `openTerm` reads the card's
    state and room from it, and 3 s is about the time a remote GET takes anyway.
- **A prewarm that fails, or is still in flight.**
  - **Failed before the click.** The reject handler deletes the entry, so the click fetches again. When nobody took
    it, the rejection is handled, with no unhandled rejection in node.
  - **In flight at the click.** The click awaits the same promise, so there is no second GET.
    - If it then fails, `attachTask`'s catch shows "could not attach". That is the same as a click whose own GET
      failed at about the same moment.
    - If the room is silent, the click waits for the rest of the prewarm's 15 s, which is no longer than a fresh
      fetch would wait.
  - The in-flight count goes down on either outcome, so a failure cannot hold a slot.
- **Touch and phone: nothing.**
  - `prewarmRowOf` requires `pointerType === "mouse"`, so touch and pen are out.
  - The touch case is in the headless section.
  - The listeners are `pointerover`/`pointerout` only, so the mouse events a phone emulates after a tap do not reach
    them.
- **No socket, and a pure read.**
  - The prewarm is `api()`, which is `fetch`, and the headless section counts zero `WebSocket` constructions.
  - The room's `getTask` is `st.Get` plus `withSeen`, a read with no side effect.
  - On a hub, a cold bare id costs one fan-out to each room. The hub caches the answer for two minutes, and the 100 ms
    dwell plus the cap of 2 bound it.
- **Slots.** The prewarm goes through the board's 6-slot cap. In the worst case, 2 stuck prewarms hold 2 slots for 15
  s, which leaves 4 for everything else. The cap of 2 is the right size.
- **Other cases.**
  - A `cold` row and the open card are skipped.
  - `takePrewarmed` is defined in `terminal-list.js`, which `index.html` loads after `settings-spine.js`. It is
    called only at click time, so the order is fine. `index.html` is the only page that loads either file.
- **Public repo.** The scripts and the item name only loopback addresses and room names. There are no private paths,
  tokens or nonces.

## Lows

- **L1: an old rejection deletes a newer prewarm.** Shown in node:
  1. A prewarm for `b` is in flight.
  2. A click takes it.
  3. The pointer rests on `b` again, which starts a second prewarm.
  4. The first one rejects, and its handler deletes the second entry.

  The cost is one missed prewarm, because the click fetches again. The counter stays right. Delete only when
  `prewarmed.get(id)?.p === p`.
- **L2: the headless section calls `takePrewarmed` directly, not through a click.** Add one click through
  `attachTask` after a rest, and assert a single GET for that id. Also add a rest followed by a click more than 3 s
  later, and assert two GETs. Then the TTL and the hand-over in `attachTask` are covered where they are used.

Atrium-Verdict: hub-ok 0f35c7cb..33c2347d
Atrium-Verdict: room-ok 0f35c7cb..33c2347d
Quality: a well-measured fix. It goes after the 75-325 ms that was actually measured, opens no socket, and leaves
phones alone.
