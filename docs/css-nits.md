# CSS nits

Small visual defects, each naming the skin it was seen in. Kept apart from `docs/backlog.md` because none of
these is a decision: they are things that are wrong and have not been fixed yet.

**Say which skin.** A colour that is wrong in one is frequently right in another, and a nit with no skin on it
cannot be reproduced. Twenty skins is twenty chances for a value that was tuned against navy to be invisible.

## Open

| # | Where | Skin | What is wrong |
| --- | --- | --- | --- |
| 4 | A project group's name, and a stack group's name, at rest | `paper`, `daylight`, `linen`, `frost` | see below |
| 5 | The restart banner, with nothing attached | light skins | see below |
| 6 | The group expander in the stack | every skin | see below |

**4.** The name is `hsl(var(--ghue) 70% 72%)`, a fixed lightness, on a block washed with the same hue. On the
four light skins there is a hue at which the name and the wash have the SAME luminance: a contrast ratio of
1.00, which is not a low ratio, it is a heading that is not there until you point at it. Hovering it works,
because hovering is the part that got fixed.

This is nit 1 again, one line above where nit 1 was. The fix moved the group name's HOVER off a fixed
lightness and left the resting colour on one.

It is not fixed here because the fix is a choice rather than a value, and the choice is not obvious. The name
has to stay identifiably its project's hue, that is its whole job, and the blends that clear a useful floor on
`linen` are close enough to `--head` that the hover has nowhere left to go. Somebody has to decide what a
project group's name is allowed to look like on a light board. Measured candidates, worst ratio across all
twenty one skins: blending 75% of `hsl(--ghue 70% 60%)` with `--head` gives 1.70, 65% gives 2.12, and 55%
gives 2.70, which is still under the floor the rest of the palette clears.

`scripts/check-contrast.js` holds this one PINNED at 1.00 rather than failing on it: see below.

**5.** `.termwait` takes its background from `--term-bg` and its text from `--term-fg`, falling back to
`--shell-0` and `--head`. The comment above it explains why: a terminal theme's two colours are designed to be
legible together, so taking BOTH from the same theme is right for every theme including ones nobody has
written. That reasoning holds and the fallbacks break it.

With nothing attached the pane has no terminal theme, and the two variables do not necessarily go missing
together: whichever one is still set from a previous attach pairs with the other one's board fallback. The
screenshot is that pair on a light skin, pale text on a near-white pill, saying `atrium is restarting. waiting
for dotfiles to come back` in a colour you have to already know is there.

The fix is to stop mixing systems in the fallback rather than to pick better colours. Either both come from
the terminal theme or neither does, which means one fallback that sets both, applied when there is no theme,
rather than two independent `var(--x, --y)` pairs that can resolve from different sides.

Note that this is the SECOND time this banner has been wrong for this reason, and the first fix is quoted in
the comment. A rule about pairs is not enforced by writing the pair down once.

**6.** The expander on a stack group heading, the `▾` before `today 7`, is a few pixels of glyph. Operator:
"the 'expando' icon in the titles is too small". It is the control for the one thing the heading does, it is
the only affordance on that row, and it is smaller than the count beside it.

Not a contrast defect, so `check-contrast.js` will never see it. Same category as nit 2: a control sized by
the text it happens to sit in rather than by what it is for.

## The check that catches these

**`scripts/check-contrast.js`** exists, and `scripts/ci.sh` runs it beside the skins check. It parses `:root`
and every `:root[data-skin=...]` block, holds a table of pairs that have to stay legible, composites each
`rgba` layer onto the surface under it, and scores the WCAG ratio in all twenty one palettes. 1176 pairs.

**It reads the hue-derived colours out of the rules,** by selector, rather than keeping its own copy. A table
that restates the stylesheet keeps checking the OLD colour after somebody edits the stylesheet, which was
tested by putting nit 1 back into the file and watching a table of copies stay green. A selector it names and
cannot find is an error and not a skipped check: the rule was renamed, so the pair describes a board that does
not exist.

**Three things it asserts, not one.**

1. A floor per pair. The floors are set BELOW what the palette manages today and above what is unreadable, and
   the worst current value is written next to each one. This is not a WCAG AA certificate: `--dim` and
   `--dimmest` are under AA on purpose, and a floor of 4.5 on those would fail every skin and be switched off
   within a week.
2. **A hover may not read as less prominent than the colour it replaces.** This is the rule nit 1 taught, in
   the only form a script can hold it. A fixed direction in lightness is wrong on half the skins, so what is
   checked is not the direction but the loss: whichever way the skin points, the pointer has to make the thing
   MORE legible against the surface it is on. A floor cannot find this. The hover that fails it in the self
   test scores 4.1, comfortably over the floor, against a resting colour that scores 6.8.
3. **A pinned pair,** for a defect that is written down and not fixed: it is held to the ratio it measures
   today instead of to its floor. A pin is not a suppression. A suppression goes green whatever happens next,
   and a pin fails in both directions: make the defect worse and it fails, FIX the defect and it also fails
   and tells you to delete the pin. So the list above cannot end up describing a board that no longer has
   the nit.

**It tests itself, on every run.** Seven colours broken on purpose, in memory, each asserting that the check
catches it and names the right pair, because a check that cannot fail is worse than no check: it is also a
claim that the colours were looked at. This one passed on its first run against a palette that had two real
defects in it, which is exactly what a broken checker looks like.

**The hazard, which is why most of the file is compositing:** hovers and lifts are `rgba` over a backdrop, so
the pair has to be composited against the surface it actually sits on before the ratio means anything. A
checker that compares the raw `--lift` against the text colour passes everything and proves nothing. Deleting
the compositing from the script turns 338 pairs red, so the compositing is load-bearing rather than decorative.

**What it does not catch.** "Ugly", and a colour that is legible and wrong, such as a hover that reads as a
warning. Those still need an eye. It also cannot follow a rule that stops using a palette variable: the pairs
that name `var(--x)` trust that the rule still says `var(--x)`, and that is the reason a pair reads from the
rule wherever the rule holds a colour of its own.

## Fixed

| # | Where | Skin | What it was, and what it is now |
| --- | --- | --- | --- |
| 1 | Hovering a group heading, stack and board | `paper`, and every light skin | see below |
| 2 | Glyph buttons in a terminal's bar | all | see below |
| 3 | The pinned group's heading | `linen`, `paper`, `daylight`, `frost` | see below |

**1.** The hover raised the name's LIGHTNESS to 85%, which reads as more prominent on a dark board and nearly
invisible on a light one. It now blends toward `--head`, so it goes darker on a light skin and lighter on a
dark one: the same intention, expressed in a way that survives both, and needing no new per-skin variable.

**2.** `.term-bar button` is tuned for `ctrl-c` and `exit`, which is roughly twice the padding a single
character wants. `.term-bar button.icon` squares them off. The `copy` button the original nit named is gone
from the bar, and the same defect had moved to the folder, the cog and the up arrow.

**3.** Found by `scripts/check-contrast.js` on the day it was written, and not by anybody looking. The pinned
group's heading was the raw `--warn`, a mid amber, on the loudest wash on the board: a ratio of 2.73 on
`linen`, and 2.95 to 3.08 on `paper`, `daylight` and `frost`, which is over the floor and not by much. It is
now `color-mix(in srgb, var(--warn) 60%, var(--head))`, about 4.9 on `linen`, which darkens it on a light skin
and lightens it on a dark one and still reads as amber. Nit 1's rule, applied to a colour that is not a hover.

**And the check was wrong about it first.** It reported 1.96 on `linen` and about 2.15 on the other three,
because it swept the pinned block through all twelve group hues. The pinned group is not a project and does
not get a hashed hue: `pinnedGroupHTML` writes `--ghue:41` on it and always has. So the first numbers were
taken at a hue that group cannot have, and described a heading nobody could ever see. The defect at hue 41 is
real and smaller, on one skin instead of four.

Worth keeping, because it is the failure mode of this kind of check: a pair that sweeps a value the MARKUP
fixes will invent defects, and a check that cries wolf is a check somebody switches off. `PIN_HUE` in the
script is that correction, and the pair now scores the hue the board draws.

## What the fixes taught

**A hover that changes lightness in a fixed direction is wrong on half the skins.** Anything that means "more
prominent" has to move toward or away from the skin's own text colour rather than toward white. Nits 1 and 3
are the same mistake made twice, and nit 4 is the same mistake sitting one line above the fix for nit 1, which
is the argument for the check rather than for a careful eye: the eye had already been over those exact lines.

Nits 1 and 3 are both what `scripts/check-contrast.js` finds without anybody hovering anything. Nit 2 is not
a contrast failure and the check would never have found it.
