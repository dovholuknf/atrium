# u-phone-card-search

Finding a card on the phone meant opening one first, because the "cards" picker lived only in the card view.

## What changed

- `m/index.html`: `#m-home-pick` ("cards") sits beside "view" in the landing page's title row, which now wraps so the
  page never widens at 360px (`m/m.css`).
- `m/js/card.js`: one picker, two buttons. From the landing page the menu hangs from the page under the button
  (`.onhome` in `m/cardurl.css`) and a pick calls `open`. In a card it stays in the sheet as before.
- The search box is always there, focused on open, and searches every card by title, alias, wire name, room and tags
  whichever chips are on. The card view gets it too.
- Ctrl+K toggles the picker, "/" opens it when no field has focus, Escape closes it.

## Proof

Headless unit `mHomePicker` at 360x780 and 412x892: tap "cards" on the landing page, type part of an alias, tap the
row, the card opens. It also checks the page never scrolls sideways with the menu open, and Ctrl+K, "/" and Escape.
Pictures: `docs/changes/img/u-phone-card-search/`.

## Test plan

## @LETTER@. Phone landing page card picker

### @LETTER@1. Find a card by alias

1. Run `HEADLESS_UNITS=mHomePicker,mSwitcher,mNoSideways node scripts/test-board-headless.js`.
2. Expect each to print ok.
