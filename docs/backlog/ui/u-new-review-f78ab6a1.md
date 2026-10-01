# Review of e7555101..f78ab6a1 (@ui u-lows: pop-out growler, card-URL lows, two suite flakes)

Reviewed by @review, 2026-09-30, from `git diff claude/main...m1mini/claude/u-lows` (47e8622e...f78ab6a1, six
commits: e7555101, 6fc420dc, 77fb386d, 9a2117c4, eafb0514, f78ab6a1), read only. Hub side. Board and headless checks
are @ui's, and I ran none.

## What holds

- **The pop-out rings only for its own card.** `growlSay` still returns first for any card that is not
  `growlMine` in a pop-out, and for a popped-out card on the board. Only the focus test changes, and only in a
  pop-out. `focusIsElsewhere` answers "another atrium window has focus", so a user in another application gets the
  same behavior as before. The `growlOnce` key is unchanged, so one reminder still rings once.
- **The window name follows the card.** `bootTerminalOnly` sets `window.name` from the id that `cardUrlOpen`
  resolved, with `bareId` from notify.js, which loads first. The name holds no card text.
- **The trailing slash.** `pathOf` and the path that the notification carries both drop one trailing `/`. The root
  path `/` becomes `""`, and `""` is falsy at the only comparison, so the board is never taken for a solo window.
  `isSolo` already stripped the slash, so the two sides now agree.
- The flake fixes change test waits only (`scripts/test-board-headless.js`). No assertion is loosened.

## Findings

None.

HUB DEPLOY OK f78ab6a1
