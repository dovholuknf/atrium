# Review: u term-sort-started cddeb2b2 (third terminal-list sort, started)

Range `a215945d..cddeb2b2`, one commit, branch claude/term-sort-started. It is a cherry-pick of 0684c447 (sent on
the old-history base b36e0390), and `git range-diff` shows the two patches identical. Spec: option D of
docs/rnd/what-am-i-working-on-design.md (claude/rnd 58262f39). Option B there was rejected, D is clint's own ask.

**Verdict: HUB OK and ROOM OK `a215945d..cddeb2b2`.** No findings above a nit.

## What was checked

- `termOrder` sorts `created_at` descending with `localeCompare`, then the id ascending. That is a sound order for
  real data because the store writes every timestamp as `store.TimeFormat` (`2006-01-02T15:04:05.000Z`), fixed
  width in UTC, so text order is time order. A card with no `created_at` sorts last. It runs before grouping, so it
  holds inside every group mode, as the other two sorts do.
- The stored key: `atrium.termSort` is `"started"`, `"1"` or `"0"`. Anything else reads as the activity default,
  as before (`!== "0"`). Nothing else reads the key: the board's two files are the only users, and no Go code or
  settings sync knows it, so the new value cannot be refused or dropped anywhere.
- The buttons: name is lit only when neither of the other two is, and `setTermSort` now leaves `started` when
  either of the old two is pressed, even when `sortByActivity` already matches. `toggleTermSort` keeps its name
  and swaps between name and activity, which is its only meaning today.
- CSS: the sort row takes the three-cell grid the group row already uses, in both the gear (`index.html`) and the
  tray. termBox's overrun checks at 150px still run against the three-cell row, and the worker reports them green.
- test-sort-order.js runs the tie fixtures under `started` for stability in both input orders, and skips the
  oldest-first expectation for it only.
- The headless section termSortStarted covers order, tiebreak, two renders, grouping, the stored value, the gear's
  lit button, the summary, a reload, and a nonsense value. It is wired into HEADLESS_ONLY and the whole run. Board
  checks are @ui's: read, not run.

## Nit

1. In scripts/test-board-headless.js the new section is inserted under the existing comment "The gear's terminal
   list section: the list's controls, and the cache summary ...", so that comment now sits on top of the new
   section's own comment, away from gearTermListSection. Move the new section above that comment.

Quality: after the Sonnet switch, no drop seen. The change is small, complete across both control rows, the
summary, the stored value and both test scripts, and the rebase was done by cherry-pick with the same patch.
