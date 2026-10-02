# Review: u-remind-me, e4b7653c..239be6ba (m1mini, 2026-10-02): OK

claude/u-remind-me, one commit, board only, a pause exception from clint ("no growler reminders, but a remind me
control"). It reuses the existing `POST /_hub/growls/{id} {do: snooze, minutes}`. Unsigned.

## What holds

- **Snooze is renamed "remind me"** on the board and on /m, with the options 15 min, 1 h and tomorrow 9am.
  `growlRemind` is split out, so the bell and the growler share one path. The minutes are clamped to the hub's 1 to
  10080, and tomorrow 9am is computed from the local clock.
- **The bell gets the control, and the growler being off does not matter.** `recordToLog` carries the growler id,
  and a repeat refreshes it. The bell reads `growlSet`, which is tracked whether or not the growler is drawn. So with
  the growler off, the default since u-growler-off, the bell is where "remind me" lives:
  - only the newest row of a growler gets the control;
  - an open growler gets "remind me" and then the options;
  - a snoozed one shows "reminding in X";
  - an ended or unknown one shows nothing.
- **The handlers follow the attribute rule** (u-new-sec-attr-js-strings): the growler id goes in an escaped
  `data-growl`, the clicks are bound with `onclick` on the element and not inline JS, and `stopPropagation` keeps
  the row's open from firing too. A growler that ended between paint and click is a no-op, then a repaint.
- **Tests.** The new `growlRemind` case covers the off-by-default bell: the open row shows the control, the ended
  row shows none, the menu shows the three options, the click posts `snooze` with 60, nothing is drawn, and tomorrow
  9am comes out between 1 and 2880 minutes. I read it and did not run it, per the board rule.

## Lows

- **The "reminding in X" check is vacuous.** It passes when the row shows nothing (`btns.join() !== ""` guards the
  regex). Feed a growler with `state: "snoozed"` and an `until`, then assert the text.
- **An open bell does not repaint on a growl event.** If the hub ends or snoozes a growler while the bell is open, its
  row keeps the old control until the next open or click. It is harmless (a click on an ended one is a no-op), but
  calling `openToastLog()` from `growlApply` when the bell is open would keep it true.

Verdict: OK e4b7653c..239be6ba, hub-ok and room-ok.

Quality: after the Sonnet switch, a tidy change that fits the growler-off world, where the bell is the surface. The
lows are a test assertion and a repaint.

## Re-read at 2b7eaa5d (2026-10-02): OK e4b7653c..2b7eaa5d

Both lows are closed.
- `growlApply` repaints an open bell (`#toastlog.open`, not on the seed). `openToastLog` already guards
  `showModal` on an open dialog.
- `growlRemind` now feeds a snoozed row with an `until` and asserts "reminding in 3 h" and no menu. When the growler
  comes back open, it asserts that the control returns without a reopen.

Two side effects of the repaint, low, for a later touch:
- `openToastLog` marks the log seen, so every growl event while the bell is open resets the "new" highlight under
  the reader, against its own comment ("does not clear under you while you read it"). Split a repaint from an open,
  so the repaint skips the seen mark.
- The list is rebuilt, so a reader's scroll position can jump. Keep `scrollTop` across the repaint.

Verdict: OK e4b7653c..2b7eaa5d, hub-ok and room-ok.
