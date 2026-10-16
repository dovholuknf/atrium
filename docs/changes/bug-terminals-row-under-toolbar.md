# Terminals list: a sliver of a row above PINNED

## What the row is

The link-opening card (`js/link-opening.js`). A pasted link is drawn as a `.card.linkopen` at the top of the terminals
list, inserted with `afterbegin` straight into `.termscroll`. A failed open stays as that card with a warn (orange)
border, and its title is the link, so the cut text is the tail of a pull request url or name.

## Why it is clipped

`.termscroll` is a flex column with `min-height: 0` and a bounded height, and `.card` is `overflow: hidden`. A flex item
with `overflow: hidden` has an automatic minimum height of 0 and `flex-shrink: 1`, so when the list holds more than
fits (29 rows) the column squeezes this one direct child down to its borders: an orange top and bottom rule with the
text cut off. The other rows sit inside group wrappers that do not shrink. The toolbar is not covering it, the row is
only a few pixels tall.

## The fix

`.termscroll > .card { flex: none; }` so a card that is a direct child of the scroll box keeps its own height and the
box scrolls instead.

## Test plan

## @LETTER@. A pending link card keeps its height

1. Paste a link that fails to open while the Terminals tab lists many rows, grouped.

**Expected:** the card above PINNED shows its title, the reason and the buttons in full, and the list scrolls under it.
