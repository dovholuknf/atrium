# Review: u-term-scroll 9c758631

Range `8bfdcdb1..9c758631`, one commit, off landing 8bfdcdb1. It changes:
- `css/phone.css`;
- `scripts/test-board-headless.js`, which gains the new `termListLastRow` section;
- the item doc `docs/backlog/ui/u-new-terminals-list-last-row-phone.md`.

Verdict: **OK** for room and hub. One Low and one note on the test can follow.

## How it was checked

- I read the CSS diff and the rules around it, and the new headless section. Per the standing note, I read the board
  units and did not run them.
- I checked the DOM nesting:
  - The containing block for `top: 100%` and the `100%` in the cap is `.term-list`, which is `position: relative`.
  - The query container is `.term-layout.has-term`.
  - The phone key bar is in normal flow, and the opened list (z 40) floats over it.
- The worker's shots are in a scratch folder on m1mini, before and after.

## Points

- **The cause is right.** `70vh` is measured on the layout viewport. Once the browser's bottom bar or the keyboard
  is up, the visible screen is shorter than that, and `main` clips whatever is below the visible edge.
- **The fix is right by construction.**
  - `100cqh - 100%` is the room the layout has below the trigger.
  - The layout's height comes from main's flex column, which `--vvh` already sizes to the visual viewport.
  - So the list ends at the layout's bottom edge whatever the gap is, not just at the two gaps the test uses.
  - The tray view uses `--trayh` as its offset, which is right, because there `.term-list` has a height of 0.
- **`container-type: size` is safe here.** The layout's height never comes from its content, so the size
  containment changes nothing.
  - It is scoped to `max-width: 900px` and `has-term`.
  - Under the current spec, `container-type` no longer applies layout containment. So `#t-keylabel`
    (`position: fixed`) still places itself against the viewport.
- **The caveat @ui raised (the 84 px gap passed on the old rule as well).** It does not hold the fix.
  - The new cap has no gap threshold, so clint's bar-only case is covered even though the test did not reproduce it.
    His screen probably had a taller header or more rows than the fixture.
  - It does mean only the 300 px case actually discriminates. See N1.

## Lows

- **L1: no fallback for a browser without `cqh`.** On a browser without container query units, `min(70vh, calc(100cqh
  - 100%))` is dropped as invalid, and the open list has no cap at all. Put `max-height: 70vh;` on the line before
  each new rule, so such a browser keeps the old behaviour. That means iOS Safari before 16, and older WebViews.

## Note

- **N1: the 84 px case does not catch the bug.** To make the bar-only case fail on the old rule too, give the
  fixture's layout the phone's real header height plus enough rows. Or record in the item doc that only the 300 px
  case is a regression test. Clint can confirm on his phone that the list now reaches its last row.

Atrium-Verdict: room-ok 8bfdcdb1..9c758631
Atrium-Verdict: hub-ok 8bfdcdb1..9c758631
Quality: a small, well-aimed fix that measures against the layout, not the viewport, and is honest about what the test
does and does not reproduce.

## Re-read: 1de218e4

One commit on landing d032abf0, so the range is `d032abf0..1de218e4`.

Closed:
- **L1.** Both new rules now have a plain `max-height: 70vh;` before them. A browser without `cqh` keeps the old cap.
  The section reads phone.css and fails when a fallback is missing:
  - it needs two `min()` rules or more;
  - it needs one fallback for every `min()` rule.

  `fs` and `path` are already imported, and `node --check` passes.
- **N1.** The item doc now says that only the 300 px case catches the old rule, and that the 84 px case is a guard.

Atrium-Verdict: room-ok d032abf0..1de218e4
Atrium-Verdict: hub-ok d032abf0..1de218e4
Quality: both notes closed, and the fallback has a test.
