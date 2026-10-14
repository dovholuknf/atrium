# off-card-actions

Two board-only fixes.

## Decisions

- "Not running" is `cardState(t) === "off"` (js/cardrules.js), the one predicate the board already uses. No new check.
- The hover popup (`peekActions`) drops `say` and `restart` on an off card. `open` and `resume` stay. The terminals
  menu, the card menu and the phone sheet drop `restart` on the same test. A parked card used to offer restart as a
  resume, and it still has its own `resume` entry.
- No card has a restart button of its own, so the menus and the popup are every place it appeared.
- Group headers in the Terminals list (`.tgroup`, css/sharing.css): text and count 10.5px to 13.5px, chevron 11px to
  14px, drag handle 10px to 14px, padding 1px to 5px, min height 26px. All scale with `--uiscale`. The card title is
  14.5px, so a header stays under it. The `.tl-mini` rule still overrides padding for the collapsed strip.
- Tests: `offCardActions` and `termHeaderSize` in scripts/test-board-headless.js. Both failed before the change.
  The card menu (js/card-menu.js) carries the same one-line condition and is not driven by a unit.

Shots: `img/off-card-actions-{before,after}.png`, `img/term-headers-{340,270}-{before,after}.png`.

## Test plan

## @LETTER@. Off cards and group headers

### @LETTER@1. No say or restart on an exited card

1. Hover an exited card in the Terminals list. The popup shows open, and no say or restart.
2. Right click it. There is no restart. Resume is there.
3. Hover a running card. Open, say, exit and restart are all there.

### @LETTER@2. Group headers

1. Look at the group headers in the Terminals list, at a normal width and at about 270px.
2. The text, count, chevron and drag handle read clearly and the header is taller. The card titles are still larger.
