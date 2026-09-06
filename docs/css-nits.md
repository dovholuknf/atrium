# CSS nits

Small visual defects, each naming the skin it was seen in. Kept apart from `docs/backlog.md` because none of
these is a decision: they are things that are wrong and have not been fixed yet.

**Say which skin.** A colour that is wrong in one is frequently right in another, and a nit with no skin on it
cannot be reproduced. Twenty skins is twenty chances for a value that was tuned against navy to be invisible.

## Open

Nothing.

## The check that would catch these

Most of this class of defect is a contrast failure and can be found without looking, which is worth doing
because nobody is going to open twenty skins and hover every element by hand.

**`scripts/check-contrast.js`,** run from `check-board.sh` like the other two:

1. Parse `:root` and every `:root[data-skin=...]` block out of `index.html`, resolving `rgb(var(--x-rgb))`
   back to a colour. The stylesheet is already regular enough for this: the skins check parses it today.
2. Hold a table of pairs that have to stay legible: body text on the page, a chip's text on its own
   background, a hovered row against the surface under it, the accent against the card, the warn colour
   against the warn tint.
3. Compute the WCAG contrast ratio for each pair, in each of the twenty skins, and fail below a floor.

**What it would and would not catch.** It catches "invisible" and "far too light", which is this whole list so
far. It does not catch "ugly", and it does not catch a colour that is legible and wrong, such as a hover that
reads as a warning. Those still need an eye.

**The hazard to avoid:** hovers and lifts are `rgba` over an unknown backdrop, so the pair has to be composited
against the surface it actually sits on before the ratio means anything. A checker that compares the raw
`--lift` value against the text colour will pass everything and prove nothing.

## Fixed

| # | Where | Skin | What it was, and what it is now |
| --- | --- | --- | --- |
| 1 | Hovering a group heading, stack and board | `paper`, and every light skin | see below |
| 2 | Glyph buttons in a terminal's bar | all | see below |

**1.** The hover raised the name's LIGHTNESS to 85%, which reads as more prominent on a dark board and nearly
invisible on a light one. It now blends toward `--head`, so it goes darker on a light skin and lighter on a
dark one: the same intention, expressed in a way that survives both, and needing no new per-skin variable.

**2.** `.term-bar button` is tuned for `ctrl-c` and `exit`, which is roughly twice the padding a single
character wants. `.term-bar button.icon` squares them off. The `copy` button the original nit named is gone
from the bar, and the same defect had moved to the folder, the cog and the up arrow.

## The check that would catch these

Nit 1 is exactly what `scripts/check-contrast.js` would have found without anybody hovering anything, and it
is still worth building for the same reason: twenty skins is twenty chances for a value tuned against navy to
be invisible. The design for it is below and is unchanged.

Worth noting what the fix taught, because it generalises: **a hover that changes lightness in a fixed
direction is wrong on half the skins.** Anything that means "more prominent" has to move toward or away from
the skin's own text colour rather than toward white.
