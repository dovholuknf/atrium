# The pulls page redesign

Status: design. Backlog item u-pulls-overhaul, HIGH. Supersedes `docs/rnd/pulls-overhaul.md`, whose data analysis, stage
list, primary-action table and phases still stand and are not repeated here. This document changes the look: the page
gets a hierarchy, an attention strip, groups by what you must do, and a card per PR with a colour bar that says the
verdict before you read a word.

clint, 2026-10-08 and again today: the pull request page has no sexy. Today's page is `img/pulls-redesign/before.png`:
flat grey rows divided by a hairline, two wide inputs on top, five equal buttons per row, no verdict.

Mockups, static HTML drawn with the board's own colour tokens and shot with headless playwright (`build.js` rebuilds
them): `img/pulls-redesign/loaded-desktop.png`, `loaded-phone.png`, `empty-*.png`, `loading-*.png`, `menu-*.png`.

## The page, top to bottom

1. **Header.** Title, an Open / All segmented control with counts (replaces the state select), a repo select, and on the
   right the one green button `+ Paste a PR` and a `...` menu (Find unclaimed worktrees). The link and why inputs are
   gone: paste opens a popover.
2. **Attention strip.** Four counters: need you (orange), ready to post (teal), reviewing (blue), failed (red). Each is
   a filter: click one and the list shows only that group, click again to clear. This is the answer to "what do I do
   next" before reading any row.
3. **Groups**, in this order, each with a small caps label and a count: Needs you, Ready to post, In progress, Failed,
   Closed recently (only under "All" or when the page is empty). Inside a group, newest first. An empty group is not
   drawn.
4. **Cards**, one per PR, described below.

## The card

```
 ▌ openziti/tlsuv #378  Rework the tls handshake  ekoby                          $2.40 . 1h
 ▌ [BLOCKED] 2 blockers  6 med  7 low  tests 38 ran, 2 failed
 ▌ o worktree -- o deps -- o panel -- o review                        [ Post review ] [...]
```

- **Colour bar** on the left edge, 4px: teal approve, amber changes, red blocked, blue reviewing, orange waiting on you,
  dim red failed, grey queued or closed. It is the whole row's state at a glance, and the only strong colour on the card
  besides the one primary button.
- **Line one:** repo in mono and dim, number in bold white, title in white semi-bold (the heading of the card), author
  dim. Cost and age sit top right in the dim meta text.
- **Line two:** the verdict pill, then the finding chips: blockers red, med amber, low plain, tests green or red.
  A pill is uppercase and tinted, never a solid fill, so only the primary button is solid.
- **Line three:** the stage strip as four dots joined by lines: filled teal done, blue with a halo running, red failed,
  hollow not reached. A one-line muted note sits above it for a waiting question (orange) or a failed stage (red).
- **Right column:** meta on top, the ONE primary button bottom right, then `...`. The primary's colour follows the state:
  teal Post review, orange Answer, blue Open card, grey Retry and Start review.
- Hover raises the border from dim to stroke. A closed card is dimmed to 55 percent with no buttons.

The primary action per state, the `...` menu contents (`menu-desktop.png`), and the cut list are as in
`pulls-overhaul.md`. Menu labels stay sentences.

## Empty, loading and loaded

- **Empty** (`empty-desktop.png`): a dashed panel, a check mark, "Nothing under review", one sentence on what pasting
  does, and the same green Paste button. Closed recently, if any, sits under it so the page is never blank.
- **Loading** (`loading-desktop.png`): three skeleton cards the height of a real one, so nothing jumps when rows land.
  The header counts show an ellipsis.
- **Loaded:** `loaded-desktop.png`. A row that is not yet reviewed has no verdict pill and shows the state pill
  (QUEUED, REVIEWING) in its place, so the second line is never empty.

## Phone (390 wide)

`loaded-phone.png`. The card becomes one column: repo, then title and author, pill and chips wrapping, the strip, and a
bottom row with meta on the left and the primary plus `...` on the right. Buttons grow to a 36 to 40px touch target. The
attention strip wraps to two lines. The colour bar stays. The menu opens as a sheet anchored to the card.

## Tokens, no new colours

Everything uses existing tokens: `--teal`, `--warn`, `--danger`, `--blue`, `--wait` (the one orange, kept for waiting on
you, which is what it means everywhere else on the board), `--chip`, `--stroke-dim`. Tints use the `-rgb` channel
triples so skins follow. Add only `--pull-bar` (the bar colour per state) so a skin can move it. No inline hex.

## Phases

Phases 1 to 3 of `pulls-overhaul.md` stand (data, row layout, primary action). The redesign replaces its phases 4 and 5:

4. **Page frame (board).** Header with the segmented control, paste popover, `...` header menu, empty and loading states.
   Done when: no `#pulls-url` or `#pulls-why` on the tab, the popover opens a card, empty and loading shots match.
5. **Groups and attention strip (board).** Group by need, the strip as filter. Done when: a headless test clicks each
   counter and asserts the shown groups.
6. **Skin and phone (board).** Cards, colour bar, pills, strip dots, phone layout, per `loaded-*.png`. Done when:
   shots at 1280 and 390 are committed beside the mockups and `bootClean` passes under every skin.

## Open for clint

1. Do the counters filter, or only count? Recommended: filter, one click, since it costs nothing and answers "show me
   only what needs me".
2. Group the list by need, or keep one newest-first list? Recommended: group. Newest-first buries a waiting card under
   finished ones, which is the failure this page exists to fix.
3. Should the verdict pill show the board's interim guess before the panel writes one? Recommended: yes, drawn with a
   dashed outline so it reads as a guess (as `pulls-overhaul.md` question 1).
4. Is the left colour bar too loud under the light skins? Recommended: ship it, and check phase 6's shots in the light
   skin. The bar is 4px and uses the same tokens as the pills.
5. Does `...` on the phone open a sheet or a popover? Recommended: a bottom sheet, because a popover on a 390px screen
   lands under the thumb.
6. Show cost on every card? Recommended: yes, top right and dim. It is cheap to ignore and clint reads it often.
7. Closed rows: under "All" only. Recommended: yes, and the empty state shows the last few so the page is never blank.
