# Review: u-growler-off stage 1, 06c45ed2..0d88af99 (m1mini, 2026-10-02): OK

claude/u-growler-off, 4 commits, board only, a pause exception clint approved. Unsigned.

## What holds

- **One gate, applied at every sink.** `growlOn()` reads `atrium.growler` from localStorage on every call, and
  false is the default, also when storage throws.
  - `growlDrawn()` returns nothing when off. So the stack, the favicon dot and the title flash all go through it,
    and `growlHas` goes false.
  - The undo bar (`growlOfferUndo`), the phone strip's undo and the growler's own tone (`growlSay`) check it
    separately.
  - The set is still tracked and `growlLog` still writes every raise, snooze, reminder and end, so the bell keeps the
    alert and its count. `alerting.notify` still runs, so desktop notifications follow the notify setting as stated.
- **Permissions fall back to the ordinary path, by construction.** With `growlHas` false, the keyed toast wrapper no
  longer swallows the permission toast (growl.js toast wrapper), and the permission nag stops skipping
  (notify.js:870-871). The ordinary announce always called `notify()` for a new permission (notify.js:1183). So a
  permission keeps its toast, tone, nag, perms badge and bell line. Nothing new is suppressed.
- **Switching** clears leftover undo bars, redraws, and repaints the checkbox. A `storage` listener carries it to
  every other window of the browser, so there is no message between them.
- **Tests.** The new `growlOff` case is thorough: off by default, with the checkbox agreeing. A question and a blocked
  card draw nothing and ring no growler tone, set no favicon dot, leave a bell line and send one desktop notification.
  With notify off, none. A reminder is logged and silent. A dismissal leaves no undo bar. A permission is logged,
  draws no growler, and `growlHas` lets the nag through. On restores the growler at once, and a second window
  follows both ways. Every existing growler case seeds the setting on (`growlBoard`'s init script), so they still
  test the on behaviour. I read the cases and did not run them, per the board rule.

## Low

- **The permission fallback is shown by construction, not by a test.** `growlOff`'s permission is a growler stub
  with no pending permission row, so the ordinary toast and tone never fire in the case. Add one pending permission
  through the perms feed with the growler off, and assert the keyed toast and the permission tone. That proves the
  item's "permission keeps the ordinary toast, tone, badge and bell".
- Default off is a visible change for every browser on the next board deploy. Questions then reach only the bell
  and the desktop notification. That is what clint asked for, so the changelog line should say it plainly for the
  deploy note.

Verdict: OK 06c45ed2..0d88af99, hub-ok and room-ok.

Quality: after the Sonnet switch, a small, well-placed change. One predicate at the sinks, the fallback inherited
rather than rebuilt, and a test that walks every state. The low is a missing assertion, not a missing behaviour.
