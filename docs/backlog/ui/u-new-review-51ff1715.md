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

## Reverted: 310d75be, then u-pin-tests b343ac5d

The orchestrator reverted u-pin-shows on claude/main (310d75be) because cold pinned cards flooded the list: a pin
kept every dead pinned row on screen. That is the cost the old comment named. So the OK above was right about the
code but wrong on the rule, and I should have weighed the old rule's reason against clint's ask. The original report
(a pinned row missing) is still open.

u-pin-tests b343ac5d (range `310d75be..b343ac5d`) is test-script and docs only. It removes `pinShows` and its `only`
entry, restores the hide-strip asserts, and deletes the u-pin-shows item and changes doc.

Verdict: **OK** for room and hub.
- Against `a9307bc7`, the last tree before 51ff1715, no line that mentions a pin differs in the headless script.
  `terminal-list.js` is identical. `node --check` passes.
- It merges cleanly onto landing after the revert.

Atrium-Verdict: room-ok 310d75be..b343ac5d
Atrium-Verdict: hub-ok 310d75be..b343ac5d
