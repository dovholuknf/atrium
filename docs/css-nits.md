# CSS nits

Small visual defects, each naming the skin it was seen in. Kept apart from `docs/backlog.md` because none of
these is a decision: they are things that are wrong and have not been fixed yet.

**Say which skin.** A colour that is wrong in one is frequently right in another, and a nit with no skin on it
cannot be reproduced. Twenty skins is twenty chances for a value that was tuned against navy to be invisible.

## Open

| # | Where | Skin | What is wrong |
| --- | --- | --- | --- |
| 1 | Stack page, grouped by `when`, hovering `today` | `paper` | far too light. Reads as yellow on a pale background, so the hover barely registers |
| 2 | Copy icon in a popped-out terminal's bar | all | `.term-bar button` gives it `padding: 6px 13px`, which is right for a word and far too wide for a glyph. It wants icon padding, the way `.chip.icon` does |

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

Nothing yet.
