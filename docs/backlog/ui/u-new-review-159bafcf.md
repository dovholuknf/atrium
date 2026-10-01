# Review of fix-joined 159bafcf (@ui, a joined live session hidden by the terminals list)

Range `727ebe27..159bafcf`, one commit on claude/fix-joined. Board only: `internal/api/web/js/terminal-list.js`, the
`joinedLive` headless section, and the term-nesting name list. Order: D:\tmp\ui-order-cover-filter.txt item 2.

## What is right

- `termCold` is still the one predicate behind the grey, the tooltip, the click and the hide (`sessionHiddenBy` and
  `inactiveAgent` both read it), so the order's "one predicate" rule holds.
- A supervised card is never cold. A card with status `done`, `dead` or `shelved`, and not supervised, is cold.
- A joined row has no `attachTask`, has a tooltip that says why, and gets a `joined` chip. Every string is a literal,
  and the id is the one `termRow` already put in the attribute.
- `termJoined` was added to the term-nesting name list, so that test still extracts the function it calls.

## Findings

1. **MEDIUM, from reading the code: a parked card reads as joined.** Parking is a flag, not a status
   (`internal/daemon/park.go:20`: "`parked_at` is set, the status it had is kept, its resume id is kept and no process
   runs"). A parked card is `supervised: false` and keeps `running`, `needs-input` or `needs-permission`. The strip
   lists it while it is pinned (`terminal-list.js:1633`), which is the usual case for parked fixtures. Before this
   commit, it was cold: grey, and a click called `resumePinned`. Now `termCold` is false and `termJoined` is true. The
   row wears `joined`, the tooltip says "joined from your own terminal", a click does nothing, and hide agents keeps
   it. The keepalive policy parks idle cards, so this hits the cards the operator returns to most. The board already
   has `parked_at` (`board.js:955`, `card-menu.js:838`). Fix: a parked card is cold
   (`if (t && t.parked_at) return true` after the supervised check), so `termJoined` excludes it too. Add a parked
   `needs-input` card with `parked_at` set to `joinedLive`: it must be cold, it must hide with hide agents on, and its
   row must call `resumePinned`.
2. **Low.** `termJoined` trusts the status alone. A joined session whose terminal closed without a SessionEnd keeps a
   live status until the reaper sees its pid gone. That is the daemon's liveness, so it is acceptable. Consider also
   requiring `t.pid > 0`, which `sharing.js:450` and the terminate entry already treat as "a process is there". Then a
   card with a live status and no process at all, such as a card left over after a daemon restart, falls to cold.
3. **Nit.** `joinedLiveSection` was inserted between the gear terminal list's comment and `gearTermListSection`, so
   that comment now sits above the wrong function (`scripts/test-board-headless.js:15379`).

The known gap @ui named, that an unpinned joined card is not in the strip at all, is outside this fix and still
open.

Not run: the headless suite. Board checks are @ui's.

Quality: after the Sonnet switch, the same pattern as before: the case the order named is right, and the other card
that is not supervised and has a live status, the parked one, was missed.

**HOLD 727ebe27..159bafcf** on finding 1.

## Re-read: d93076a2

Range `727ebe27..d93076a2`, 159bafcf plus d93076a2.

- **Finding 1 is closed.** `termCold` returns true for `parked_at` after the supervised check. A parked card is cold,
  `termJoined` is false for it, its row calls `resumePinned`, and hide agents takes it out. A supervised card that
  still carries a stale `parked_at` stays live, because the supervised check comes first. `joinedLive` now asserts all
  of this on a parked `needs-input` card, and it fails on 159bafcf.
- **Low 2 is withdrawn.** `terminal-list.js:698` says the pid is a reconnect hint that is never cleared, so `pid > 0`
  would not separate live from gone. @ui is right to skip it.
- **The nit is fixed.** The gear terminal list's comment is back above its own function.

No new findings. Not run: the headless suite, which is @ui's.

Quality: after the Sonnet switch, the fix is exact and the test covers the case I named. Skipping the low with a
reason from the code was right.

**HUB DEPLOY OK and ROOM DEPLOY OK 727ebe27..d93076a2.**

## Follow-up: 39302b51, the strip lists an unpinned joined card

Range `d93076a2..39302b51`, one commit. The strip filter is now `supervised || pinned || termJoined(t)`, and
`!t.offline` still comes first.

- `termJoined` is the existing predicate, so a parked or exited card that is not pinned stays out, and a live joined
  card is listed with its chip and no click. `joinedLive` drives `renderTermList` over unpinned cards with hide
  agents off and on. It fails on the old filter.
- Any unlaunched claude session whose SessionStart hook reaches atrium gets a card (`session.go:185`, `Register`),
  so "joined" here means every live session atrium does not supervise, not only `atrium join`. That is the order's
  intent, since each such session can ask a question. The volume is small: a read-only GET of the live room board
  (7781) found one such card.
- A joined session that ends without a SessionEnd leaves through the reaper: dead when its pid is gone, or after 15
  quiet minutes with no pid (`reaper.go`). Its row then turns cold, and the filter drops it because it is not pinned.
- Other readers of the row: dragging and filing read `lastTasks` by id and work for any row. The badge counts
  `supervised` alone. `reconcileAttached` keys on `supervised`.

No findings. Not run: the headless suite, which is @ui's.

Quality: after the Sonnet switch, this closes the gap @ui named itself, and @ui checked the callers.

**HUB DEPLOY OK and ROOM DEPLOY OK d93076a2..39302b51.**
