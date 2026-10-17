# bug-mgrowlquestion-fails-alone

Headless unit `mGrowlQuestion` passed before cb6ef738 and failed alone after it: "the buttons are scrolled out of the
strip: the body leaves no room for them."

## Cause

The code was wrong, not the unit. cb6ef738 made every `.gm-full` `flex: 0 0 auto` so a stack of nine growlers would not
squeeze under their own content. That is right for a stack, but a LONE question is the whole strip, and its body
(`flex: 1 1 auto`, capped at 30dvh) relied on the growler shrinking so the strip stayed inside its 50dvh cap. Fixed
height pushed the buttons below the fold and the strip scrolled.

The commit was not caught because `internal/api/web/m/m.css` belonged to the `m-home` bucket only. The `m-growl`
bucket (mGrowl, mGrowlQuestion, mGrowlStack, mBell) listed `growl.js` and `bell.js`, so a change to the growler's CSS
never selected its units, and the time budget of a broad diff then cut the rest.

## Fix

- `internal/api/web/m/m.css`: a lone `.gm-full` (no sibling `.gm-full` before or after it) goes back to
  `flex: 0 1 auto; min-height: 0`. Stacks keep their height and scroll.
- `scripts/test-buckets.json`: `m.css` is a source of the `m-growl` bucket, so `test-touched` picks all four of its
  units for a change to it.

## Proof

`HEADLESS_UNITS=mGrowlQuestion node scripts/test-board-headless.js` passes three times alone, and `mGrowlStack`,
`mGrowl` and `mBell` still pass. `echo internal/api/web/m/m.css | node scripts/test-touched-plan.js` lists all four.

## Not fixed here, causes noted

- `clearKeepsPage` fails only under 16 shards: it reads xterm `getLine`/`translateToString` before the terminal has
  written, so `loadCell` throws. The cure is to wait for the write before reading.
- `worn` fails only when sharded. The cause was not chased.

## Test plan

## @LETTER@. A lone phone growler keeps its buttons in view

### @LETTER@1. One question

1. Run `HEADLESS_UNITS=mGrowlQuestion,mGrowlStack node scripts/test-board-headless.js`.
2. It prints `mGrowlQuestion ok` and `mGrowlStack ok`.
3. On `/m/` with one long question open, the reply box and buttons are visible without scrolling the strip.
