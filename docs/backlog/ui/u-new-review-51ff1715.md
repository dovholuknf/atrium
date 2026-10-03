# Review: u-pin-shows 51ff1715

Range `a9307bc7..51ff1715`, one commit: `js/terminal-list.js`, the headless test, the item doc and `docs/changes`.
clint asked for it: a pinned row is never removed by the agents or subagents hide pill, or by cold.

Verdict: **OK** for room and hub.

## How it was checked

- I read the diff. `node --check` passes on terminal-list.js. Per the standing note, I read the board units and did
  not run them.

## Points

- **One guard, at the one place a row is hidden.** `sessionHiddenBy` returns false for `t.pinned`. The pill counts
  (`inactiveAgent`, `inactiveSub`) skip pinned rows, so what a pill offers is what it removes.
  - The `!t.offline` filter is untouched, so a pinned card on an offline room still goes, as before.
  - A pinned row whose runner has gone is still drawn cold.
- **It reverses a rule that was there on purpose.** The old comment said "turning a toggle on is the operator asking
  for the inactive ones to go, pinned or not". The new rule ("a pin is always here") is clint's call, and the
  comments, the bucket count and the old hide-strip assert are all updated to match. Nothing still says the old
  rule.
- **The bucket count.** Once nothing in the bucket can be hidden, `total` equals the rows drawn and the count reads
  plain. The `5/15` form stays for a caller that passes fewer rows.
- **The test.** `pinShows` fails on 72 states with the exemption removed, per @ui.

## Note

- **The caveat stands.** The reported row drew in every state, so the original cause is not proven. This closes the
  pill paths, which are the only hide paths in `renderTermList`. If clint sees it again, the next place to look is
  the offline filter and the pinned list's own fetch (`pinnedNow`).

Atrium-Verdict: room-ok a9307bc7..51ff1715
Atrium-Verdict: hub-ok a9307bc7..51ff1715
Quality: a small change at the right point, with the pill counts kept in step and an assert that covers the whole
state grid.
